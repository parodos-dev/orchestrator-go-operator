/*
Copyright 2024 Red Hat, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package kube

import (
	"context"
	operatorsv1 "github.com/operator-framework/api/pkg/operators/v1"
	"github.com/operator-framework/api/pkg/operators/v1alpha1"
	operatorsv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	olmclientset "github.com/operator-framework/operator-lifecycle-manager/pkg/api/client/clientset/versioned"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"reflect"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	CatalogSourceNamespace               = "openshift-marketplace"
	CatalogSourceName                    = "redhat-operators"
	OpenshiftServerlessOperatorGroupName = "serverless-operator-group"
	ServerlessOperatorGroupName          = "serverless-operator-group"
	CreatedByLabelKey                    = "created-by"
	CreatedByLabelValue                  = "orchestrator"
)

func CheckNamespaceExist(ctx context.Context, client client.Client, namespace string) (bool, error) {
	nsLogger := log.FromContext(ctx)
	nsLogger.Info("Checking namespace exist", "Namespace", namespace)
	namespaceObj := &corev1.Namespace{}
	// check if namespace exists
	if err := client.Get(ctx, types.NamespacedName{Name: namespace}, namespaceObj); err != nil {
		return false, err
	}
	return true, nil
}

func CreateNamespace(ctx context.Context, client client.Client, namespace string) error {
	nsLogger := log.FromContext(ctx)
	nsLogger.Info("Creating namespace", "Namespace", namespace)
	// create new namespace
	newNamespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
	err := client.Create(ctx, newNamespace)
	if err != nil {
		nsLogger.Error(err, "Error occurred when creating namespace", "Namespace", namespace)
		return err
	}
	return nil
}

func InstallOperatorViaSubscription(
	ctx context.Context, client client.Client,
	olmClientSet olmclientset.Clientset,
	operatorGroupName string,
	subscription *v1alpha1.Subscription) error {

	logger := log.FromContext(ctx)
	subscriptionName := subscription.Name
	logger.Info("Starting subscription installation process", "SubscriptionName", subscriptionName)

	namespace := subscription.Namespace

	// check operator group exists
	err := getOperatorGroup(ctx, client, namespace, operatorGroupName)
	if err != nil {
		logger.Error(err, "Failed to get operator group resource", "OperatorGroup", operatorGroupName)
	}
	// install subscription
	installedSubscription, err := olmClientSet.OperatorsV1alpha1().
		Subscriptions(namespace).
		Create(ctx, subscription, metav1.CreateOptions{})

	if err != nil {
		logger.Error(err, "Error occurred while creating Subscription", "SubscriptionName", subscriptionName)
	}
	// Check the Subscription's status after installation
	installedCSV := installedSubscription.Status.InstalledCSV
	if installedCSV == "" {
		logger.Info("Subscription has no installed CSV: CSV not ready or Incorrectly installed subscription", "Subscription", subscriptionName)
		return err
	}
	// Get the ClusterServiceVersion (CSV) for the Subscription installed
	sfcsv := &operatorsv1alpha1.ClusterServiceVersion{}
	if err := client.Get(ctx, types.NamespacedName{Name: installedCSV, Namespace: namespace}, sfcsv); err != nil {
		logger.Error(err, "Error occurred when retrieving CSV", "ClusterServiceVersion", installedCSV)
		return err
	}
	// Check if the CSV's phase is "Succeeded"
	if sfcsv.Status.Phase == operatorsv1alpha1.CSVPhaseSucceeded {
		logger.Info("Successfully installed Operator Via Subscription", "SubscriptionName", installedSubscription.Name)
		return nil
	}
	logger.Info("Successfully installed Operator Via Subscription", "SubscriptionName", installedSubscription.Name)
	return nil
}

func getOperatorGroup(ctx context.Context, client client.Client,
	namespace, operatorGroupName string) error {
	logger := log.FromContext(ctx)
	// check if operator group exists
	operatorGroup := &operatorsv1.OperatorGroup{}
	err := client.Get(ctx, types.NamespacedName{Name: operatorGroupName, Namespace: namespace}, operatorGroup)
	if err == nil {
		logger.Info("Operator Group already exists", "Operator Group", operatorGroupName)
		return nil
	}
	// create operator group
	sfog := &operatorsv1.OperatorGroup{
		ObjectMeta: metav1.ObjectMeta{Name: operatorGroupName, Namespace: namespace},
	}
	if err = client.Create(ctx, sfog); err != nil {
		logger.Error(err, "Error occurred when creating OperatorGroup resource", "Namespace", namespace)
		return err
	}
	return nil
}

func CreateSubscriptionObject(subscriptionName, namespace, channel, startingCSV string) *v1alpha1.Subscription {
	logger := log.Log.WithName("subscriptionObject")
	logger.Info("Creating subscription object")

	subscriptionObject := &v1alpha1.Subscription{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: subscriptionName},
		Spec: &v1alpha1.SubscriptionSpec{
			Channel:                channel,
			InstallPlanApproval:    v1alpha1.ApprovalAutomatic,
			CatalogSource:          CatalogSourceName,
			StartingCSV:            startingCSV,
			CatalogSourceNamespace: CatalogSourceNamespace,
			Package:                subscriptionName,
		},
	}
	return subscriptionObject
}

func CheckSubscriptionExists(
	ctx context.Context, olmClientSet olmclientset.Clientset,
	existingSubscription *v1alpha1.Subscription) (bool, *v1alpha1.Subscription, error) {
	logger := log.FromContext(ctx)

	namespace := existingSubscription.Namespace
	subscriptionName := existingSubscription.Name

	subscription, err := olmClientSet.OperatorsV1alpha1().Subscriptions(namespace).Get(ctx, subscriptionName, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("Subscription resource not found.", "SubscriptionName", subscriptionName, "Namespace", namespace)
			return false, subscription, nil
		}
		logger.Error(err, "Failed to check Subscription does not exists", "SubscriptionName", subscriptionName)
		return false, subscription, err
	}
	logger.Info("Subscription exists", "SubscriptionName", subscription.Name)
	return true, subscription, nil
}

func CheckCRDExists(ctx context.Context, client client.Client, name string, namespace string) error {
	crd := &apiextensionsv1.CustomResourceDefinition{}
	err := client.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, crd)
	if err != nil {
		return err
	}
	return nil
}

func CleanUpNamespace(ctx context.Context, namespaceName string, client client.Client) error {
	logger := log.FromContext(ctx)
	// check namespace exist
	namespaceExist, _ := CheckNamespaceExist(ctx, client, namespaceName)

	if !namespaceExist {
		logger.Info("Namespace does not exist", "Namespace", namespaceName)
		return nil
	}
	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespaceName,
		},
	}
	// delete namespace
	if err := client.Delete(ctx, namespace); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	logger.Info("Successfully deleted Namespace", "Namespace", namespaceName)
	return nil
}

func CleanUpSubscriptionAndCSV(ctx context.Context, olmClientSet olmclientset.Clientset, subscription *v1alpha1.Subscription) error {
	logger := log.FromContext(ctx)

	subscriptionName := subscription.Name
	subscriptionNamespace := subscription.Namespace

	subscriptionExists, _, err := CheckSubscriptionExists(ctx, olmClientSet, subscription)
	if err != nil {
		logger.Error(err, "Error occurred when checking subscription exists", "SubscriptionName", subscriptionName)
		return err
	}
	if subscriptionExists {
		// get name of csv before deletion
		csvName := subscription.Status.InstalledCSV

		// deleting subscription resource
		err = olmClientSet.OperatorsV1alpha1().Subscriptions(subscriptionNamespace).Delete(ctx, subscriptionName, metav1.DeleteOptions{})
		if err != nil {
			logger.Error(err, "Error occurred while deleting Subscription", "SubscriptionName", subscriptionName, "Namespace", subscriptionNamespace)
			//return ctrl.Result{RequeueAfter: 5 * time.Minute}, err
			return err
		}
		logger.Info("Successfully deleted Subscription: %s", subscriptionName)

		// cleanup csv
		csv, err := olmClientSet.OperatorsV1alpha1().ClusterServiceVersions(subscriptionNamespace).Get(ctx, csvName, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				logger.Info("CSV resource not found", "CSV", csvName)
				return nil
			}
			logger.Error(err, "Error occurred when getting CSV", "CSV", csvName)
			return err
		}
		if err := olmClientSet.OperatorsV1alpha1().ClusterServiceVersions(csv.Namespace).Delete(ctx, csv.Name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
		logger.Info("Successfully deleted CSV", "CSV", csvName)
		return nil
	}
	return err
}

func AddLabel() map[string]string {
	return map[string]string{
		CreatedByLabelKey: CreatedByLabelValue,
	}
}
