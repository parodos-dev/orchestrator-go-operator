package rhdh

import (
	"context"
	"fmt"
	olmclientset "github.com/operator-framework/operator-lifecycle-manager/pkg/api/client/clientset/versioned"
	orchestratorv1alpha1 "github.com/parodos-dev/orchestrator-operator/api/v1alpha1"
	kubeoperations "github.com/parodos-dev/orchestrator-operator/internal/controller/kube"
	"github.com/parodos-dev/orchestrator-operator/internal/controller/util"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	rhdhv1alpha2 "redhat-developer/red-hat-developer-hub-operator/api/v1alpha2"
	"reflect"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	BackstageOperatorGroup             = "rhdh-operator-group"
	BackstageAPIVersion                = "rhdh.redhat.com/v1alpha1"
	BackstageKind                      = "Backstage"
	BackstageCRName                    = "backstage"
	BackstageReplica             int32 = 1
	BackstageSubscriptionName          = "rhdh"
	BackstageSubscriptionChannel       = "fast-1.3"
)

var ConfigMapNameAndConfigDataKey = map[string]string{
	AppConfigRHDHName:              "app-config-rhdh.yaml",
	AppConfigRHDHAuthName:          "app-config-auth.gh.yaml",
	AppConfigRHDHCatalogName:       "app-config-catalog.yaml",
	AppConfigRHDHDynamicPluginName: "dynamic-plugins.yaml",
}

func HandleRHDHOperatorInstallation(ctx context.Context, client client.Client, olmClientSet olmclientset.Clientset, namespace string) error {
	knativeLogger := log.FromContext(ctx)

	// check if subscription exist
	rhdhSubscription := kubeoperations.CreateSubscriptionObject(
		BackstageSubscriptionName,
		namespace,
		BackstageSubscriptionChannel,
		"")

	// check if subscription exists
	subscriptionExists, existingSubscription, err := kubeoperations.CheckSubscriptionExists(ctx, olmClientSet, rhdhSubscription)
	if err != nil {
		knativeLogger.Error(err, "Error occurred when checking subscription exists", "SubscriptionName", BackstageSubscriptionName)
		return err
	}
	if !subscriptionExists {
		if err := kubeoperations.InstallOperatorViaSubscription(ctx, client, olmClientSet, BackstageOperatorGroup, rhdhSubscription); err != nil {
			knativeLogger.Error(err, "Error occurred when installing operator", "SubscriptionName", BackstageSubscriptionName)
			return err
		}
		knativeLogger.Info("Operator successfully installed", "SubscriptionName", BackstageSubscriptionName)
	}

	if subscriptionExists {
		// Compare the current and desired state
		if !reflect.DeepEqual(existingSubscription.Spec, rhdhSubscription.Spec) {
			// Set owner reference for proper garbage collection
			//if err := controllerutil.SetControllerReference(&orchestrator, oslSubscription, r.Scheme); err != nil {
			//	return err
			//}

			// Update the existing subscription with the new Spec
			existingSubscription.Spec = rhdhSubscription.Spec
			if err := client.Update(ctx, existingSubscription); err != nil {
				return err
			}
		}
	}
	return nil
}

func CreateBSSecret(secretNamespace string, ctx context.Context, client client.Client) error {
	logger := log.FromContext(ctx)
	logger.Info("Creating Backstage NPMrc Secret")

	secret := &corev1.Secret{}
	err := client.Get(ctx, types.NamespacedName{
		Namespace: secretNamespace,
		Name:      RegistrySecretName,
	}, secret)

	if err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("Secret does not exist. Creating secret", "Secret", RegistrySecretName)
			newSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      RegistrySecretName,
					Namespace: secretNamespace,
					Labels:    kubeoperations.AddLabel(),
				},
				Type: corev1.SecretTypeOpaque,
				StringData: map[string]string{
					".npmrc": fmt.Sprintf("registry=%s", NpmRegistry),
				},
			}

			if err := client.Create(ctx, newSecret); err != nil {
				logger.Error(err, "Error occurred when creating secret", "Secret", RegistrySecretName)
				return err
			}
			logger.Info("Successfully created secret", "Secret", RegistrySecretName)
			return nil
		}
		logger.Error(err, "Error occurred when checking secret exist", "Secret", RegistrySecretName)
		return err
	}
	logger.Info("Secret already exist", "Secret", RegistrySecretName)
	return nil
}

func HandleCRCreation(
	rhdhConfig orchestratorv1alpha1.RHDHConfig,
	argoCDEnabled, tektonEnabled bool,
	clusterDomain, wfNamespace string,
	ctx context.Context, client client.Client) error {
	bsLogger := log.FromContext(ctx)

	bsLogger.Info("Handling Backstage resources")

	bsConfigMapList := GetConfigmapList(ctx, client, clusterDomain, wfNamespace, argoCDEnabled, tektonEnabled, rhdhConfig)

	if err := client.Get(ctx, types.NamespacedName{
		Namespace: rhdhConfig.RHDHNamespace,
		Name:      rhdhConfig.RHDHName,
	}, &rhdhv1alpha2.Backstage{}); apierrors.IsNotFound(err) {
		secret := rhdhv1alpha2.ObjectKeyRef{
			Name: BackendAuthSecretName,
		}
		backstageCR := &rhdhv1alpha2.Backstage{
			TypeMeta: metav1.TypeMeta{
				APIVersion: BackstageAPIVersion,
				Kind:       BackstageKind,
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      BackstageCRName,
				Namespace: rhdhConfig.RHDHNamespace,
				Labels:    kubeoperations.AddLabel(),
			},
			Spec: rhdhv1alpha2.BackstageSpec{
				Application: &rhdhv1alpha2.Application{
					AppConfig:                   &rhdhv1alpha2.AppConfig{ConfigMaps: bsConfigMapList},
					DynamicPluginsConfigMapName: AppConfigRHDHDynamicPluginName,
					ExtraEnvs: &rhdhv1alpha2.ExtraEnvs{
						Secrets: []rhdhv1alpha2.ObjectKeyRef{secret},
					},
					Replicas: util.MakePointer(BackstageReplica),
				},
			},
		}
		if err := client.Create(ctx, backstageCR); err != nil {
			bsLogger.Error(err, "Error occurred when creating Backstage resource")
			return err
		}
		bsLogger.Info("Successfully created Backstage resource")
	}
	return nil
}

func GetConfigmapList(ctx context.Context, client client.Client,
	clusterDomain, wfNamespace string,
	tektonEnabled, argoCDEnabled bool,
	rhdhConfig orchestratorv1alpha1.RHDHConfig) []rhdhv1alpha2.ObjectKeyRef {

	cmLogger := log.FromContext(ctx)
	cmLogger.Info("Creating ConfigMaps...")

	configmapList := make([]rhdhv1alpha2.ObjectKeyRef, 0)
	namespace := rhdhConfig.RHDHNamespace
	for cmName, configDataKey := range ConfigMapNameAndConfigDataKey {
		if err := client.Get(ctx, types.NamespacedName{
			Namespace: namespace,
			Name:      cmName,
		}, &corev1.ConfigMap{}); apierrors.IsNotFound(err) {
			configValue, err := ConfigMapTemplateFactory(cmName, clusterDomain, wfNamespace, argoCDEnabled, tektonEnabled, rhdhConfig)
			if err != nil {
				cmLogger.Error(err, "Error occurred when parsing config data for configmap", "CM", cmName)
				continue
			} else {
				if err := CreateConfigMap(cmName, configDataKey, namespace, configValue, ctx, client); err == nil {
					if cmName != AppConfigRHDHDynamicPluginName {
						configmapList = append(configmapList, rhdhv1alpha2.ObjectKeyRef{Name: cmName})
					}
				}
			}
		}
	}
	return configmapList
}

func CreateConfigMap(
	name string, configDataKey string, namespace string, configValue string,
	ctx context.Context, client client.Client) error {

	logger := log.FromContext(ctx)

	// Create the ConfigMap object
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    kubeoperations.AddLabel(),
		},
		Data: map[string]string{
			configDataKey: configValue,
		},
	}
	if err := client.Create(ctx, configMap); err != nil {
		logger.Error(err, "Error occurred when creating ConfigMap", "CM", name)
		return err
	}
	logger.Info("Successfully created ConfigMap", "CM", name)
	return nil
}

func HandleBackstageCleanup(ctx context.Context, client client.Client, olmClientSet olmclientset.Clientset, rhdhNamespace string) error {
	logger := log.FromContext(ctx)
	rhdhSubscription := kubeoperations.CreateSubscriptionObject(
		BackstageSubscriptionName,
		rhdhNamespace,
		BackstageSubscriptionChannel,
		"")

	namespaceExist, _ := kubeoperations.CheckNamespaceExist(ctx, client, rhdhNamespace)
	if namespaceExist {
		backstageCRList, err := listBackstageCRs(ctx, client, rhdhNamespace)

		if err != nil || len(backstageCRList) == 0 {
			logger.Error(err, "Failed to list backstage CRs or have no Backstage CRs created by Orchestrator Operator and cannot perform clean up process")
			return err
		}
		if len(backstageCRList) == 1 {
			// remove namespace
			if err := kubeoperations.CleanUpNamespace(ctx, rhdhNamespace, client); err != nil {
				logger.Error(err, "Error occurred when deleting namespace", "NS", "namespace")
				return err
			}
			// remove subscription and csv
			if err := kubeoperations.CleanUpSubscriptionAndCSV(ctx, olmClientSet, rhdhSubscription); err != nil {
				logger.Error(err, "Error occurred when deleting Subscription and CSV", "Subscription", rhdhSubscription.Name)
				return err
			}
			// remove all CRDs, optional (ensure all CRs and namespace have been removed first)
		}
	}
	return nil
}

func listBackstageCRs(ctx context.Context, k8client client.Client, namespace string) ([]rhdhv1alpha2.Backstage, error) {
	logger := log.FromContext(ctx)

	crList := &rhdhv1alpha2.BackstageList{}

	listOptions := []client.ListOption{
		client.InNamespace(namespace),
		client.MatchingLabels{kubeoperations.CreatedByLabelKey: kubeoperations.CreatedByLabelValue},
	}

	// List the CRs
	if err := k8client.List(ctx, crList, listOptions...); err != nil {
		logger.Error(err, "Error occurred when listing Backstage CRs")
		return nil, err
	}

	logger.Info("Successfully listed Backstage CRs", "Total", len(crList.Items))
	return crList.Items, nil
}
