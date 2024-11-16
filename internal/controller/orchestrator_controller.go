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

package controller

import (
	"context"
	"github.com/parodos-dev/orchestrator-operator/internal/controller/kube"
	"github.com/parodos-dev/orchestrator-operator/internal/controller/rhdh"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/controller"

	configv1 "github.com/openshift/api/config/v1"
	olmclientset "github.com/operator-framework/operator-lifecycle-manager/pkg/api/client/clientset/versioned"
	orchestratorv1alpha1 "github.com/parodos-dev/orchestrator-operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// Definition to manage Orchestrator condition status.
const (
	TypeAvailable   string = "Available"
	TypeProgressing string = "Progressing"
	TypeDegrading   string = "Degrading"
)

const (
	FinalizerCRCleanup = "rhdh.redhat.com/orchestrator-cleanup"
)

// OrchestratorReconciler reconciles an Orchestrator object
type OrchestratorReconciler struct {
	client.Client
	OLMClient olmclientset.Clientset
	Scheme    *runtime.Scheme
	Recorder  record.EventRecorder
}

//+kubebuilder:rbac:groups=rhdh.redhat.com,resources=orchestrators,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=rhdh.redhat.com,resources=orchestrators/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=rhdh.redhat.com,resources=orchestrators/finalizers,verbs=update
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=secrets;configmaps;namespaces;events,verbs=list;get;create;delete;patch;watch;update
//+kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=get;list;watch
//+kubebuilder:rbac:groups=operators.coreos.com,resources=subscriptions;operatorgroups;clusterserviceversions;catalogsources,verbs=get;list;watch;create;delete;patch
//+kubebuilder:rbac:groups=sonataflow.org,resources=sonataflows;sonataflowclusterplatforms;sonataflowplatforms,verbs=get;list;watch;create;delete;patch;update
//+kubebuilder:rbac:groups=operator.knative.dev,resources=knativeeventings;knativeservings,verbs=get;list;watch;create;delete;patch;update
//+kubebuilder:rbac:groups=rhdh.redhat.com,resources=backstages,verbs=get;list;watch;create;delete;patch;update
//+kubebuilder:rbac:groups=config.openshift.io,resources=ingresses,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// The Reconcile function compares the state specified by
// the Orchestrator object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.17.3/pkg/reconcile
func (r *OrchestratorReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Starting reconciliation")

	// Fetch the Orchestrator instance
	// The purpose is to check if the Custom Resource for the Kind Orchestrator
	// is applied on the cluster if not we return nil to stop the reconciliation
	orchestrator := &orchestratorv1alpha1.Orchestrator{}

	err := r.Get(ctx, req.NamespacedName, orchestrator) // Lookup the Orchestrator instance for this reconcile request
	if err != nil {
		if apierrors.IsNotFound(err) {
			// If the custom resource is not found then it usually means that it was deleted or not created
			// In this way, we will stop the reconciliation
			logger.Info("Orchestrator resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		logger.Error(err, "Failed to get orchestrator")
		return ctrl.Result{}, err
	}

	if !orchestrator.DeletionTimestamp.IsZero() {
		err := r.handleCleanup(ctx, orchestrator)
		if err != nil {
			return ctrl.Result{RequeueAfter: 5 * time.Minute}, err
		}
		// Remove the finalizer to complete deletion
		controllerutil.RemoveFinalizer(orchestrator, FinalizerCRCleanup)
		if err := r.Update(ctx, orchestrator); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Add finalizer if not present
	if err := r.addFinalizers(ctx, orchestrator); err != nil {
		return ctrl.Result{}, err
	}

	// Set the status to Unknown when no status is available - usually initial reconciliation.
	if orchestrator.Status.Conditions == nil || len(orchestrator.Status.Conditions) == 0 {
		if err := r.UpdateStatus(ctx, orchestrator, orchestratorv1alpha1.RunningPhase, metav1.Condition{
			Type:               TypeAvailable,
			Status:             metav1.ConditionUnknown,
			Reason:             "Reconciling",
			Message:            "Starting Reconciliation",
			LastTransitionTime: metav1.Now(),
		}); err != nil {
			return ctrl.Result{}, err
		}
		// Re-fetch orchestrator Custom Resource after updating the status
		if err := r.Get(ctx, req.NamespacedName, orchestrator); err != nil {
			logger.Error(err, "Failed to re fetch orchestrator")
			return ctrl.Result{}, err
		}
	}

	argoCDEnabled := orchestrator.Spec.ArgoCd.Enabled
	tektonEnabled := orchestrator.Spec.Tekton.Enabled
	wfNamespace := orchestrator.Spec.OrchestratorConfig.Namespace

	// handle sonataflow
	sonataFlowOperator := orchestrator.Spec.ServerlessLogicOperator
	if err = r.reconcileSonataFlow(ctx, sonataFlowOperator, orchestrator); err != nil {
		logger.Error(err, "Error occurred when installing SonataFlow resources")
		_ = r.UpdateStatus(ctx, orchestrator, orchestratorv1alpha1.FailedPhase, metav1.Condition{
			Type:    TypeDegrading,
			Status:  metav1.ConditionFalse,
			Reason:  "Failed to create SonataFlow Resources",
			Message: err.Error(),
		})
		return ctrl.Result{RequeueAfter: 1 * time.Minute}, err
	}
	_ = r.UpdateStatus(ctx, orchestrator, orchestratorv1alpha1.CompletedPhase, metav1.Condition{
		Type:    TypeProgressing,
		Status:  metav1.ConditionTrue,
		Reason:  "Created SonataFlow Resources",
		Message: "Completed SonataFlow Reconciliation",
	})

	//handle knative
	serverlessOperator := orchestrator.Spec.ServerlessOperator
	if err := r.reconcileKnative(ctx, serverlessOperator); err != nil {
		logger.Error(err, "Error occurred when installing K-Native resources")
		_ = r.UpdateStatus(ctx, orchestrator, orchestratorv1alpha1.FailedPhase, metav1.Condition{
			Type:    TypeDegrading,
			Status:  metav1.ConditionFalse,
			Reason:  "Failed to create K-Native Resources",
			Message: err.Error(),
		})
		return ctrl.Result{Requeue: true, RequeueAfter: 3 * time.Minute}, err
	}
	_ = r.UpdateStatus(ctx, orchestrator, orchestratorv1alpha1.CompletedPhase, metav1.Condition{
		Type:    TypeProgressing,
		Status:  metav1.ConditionTrue,
		Reason:  "Created K-Native Resources",
		Message: "Completed K-Native Reconciliation",
	})

	// handle backstage
	rhdhConfig := orchestrator.Spec.RHDHConfig
	if err = r.reconcileBackstage(ctx, wfNamespace, argoCDEnabled, tektonEnabled, rhdhConfig); err != nil {
		logger.Error(err, "Error occurred when installing Backstage resources")
		_ = r.UpdateStatus(ctx, orchestrator, orchestratorv1alpha1.FailedPhase, metav1.Condition{
			Type:    TypeDegrading,
			Status:  metav1.ConditionFalse,
			Reason:  "Failed to create Backstage Resources",
			Message: err.Error(),
		})
		return ctrl.Result{Requeue: true, RequeueAfter: 3 * time.Minute}, err
	}
	_ = r.UpdateStatus(ctx, orchestrator, orchestratorv1alpha1.CompletedPhase, metav1.Condition{
		Type:    TypeProgressing,
		Status:  metav1.ConditionTrue,
		Reason:  "Created Backstage Resources",
		Message: "Completed Backstage Reconciliation",
	})

	return ctrl.Result{}, nil
}

func (r *OrchestratorReconciler) reconcileSonataFlow(
	ctx context.Context,
	sonataFlowOperator orchestratorv1alpha1.ServerlessLogicOperator,
	orchestrator *orchestratorv1alpha1.Orchestrator) error {

	sfLogger := log.FromContext(ctx)
	sfLogger.Info("Starting reconciliation for SonataFlow")

	sonataflowNamespace := orchestrator.Spec.OrchestratorConfig.Namespace

	// if subscription is disabled; check if subscription exists and handle delete
	if !sonataFlowOperator.Enabled {
		// handle clean up
		if err := handleSonataFlowCleanUp(ctx, r.Client, r.OLMClient); err != nil {
			return err
		}
	}
	// Subscription is enabled;

	// check namespace exist
	if _, err := kube.CheckNamespaceExist(ctx, r.Client, sonataflowNamespace); err != nil {
		if apierrors.IsNotFound(err) {
			sfLogger.Info("Creating namespace", "NS", sonataflowNamespace)
			if err := kube.CreateNamespace(ctx, r.Client, sonataflowNamespace); err != nil {
				sfLogger.Error(err, "Error occurred when creating namespace", "NS", sonataflowNamespace)
				return err
			}
		}
		sfLogger.Error(err, "Error occurred when checking namespace exists", "NS", sonataflowNamespace)
		return err
	}

	if err := handleOSLOperatorInstallation(ctx, r.Client, r.OLMClient); err != nil {
		sfLogger.Error(err, "Error occurred when installing OSL Operator resources")
		return err
	}

	// subscription exists; check if CRD exists;
	sonataFlowClusterPlatformCRD := &apiextensionsv1.CustomResourceDefinition{}
	if err := r.Get(ctx, types.NamespacedName{Name: SonataFlowClusterPlatformCRDName, Namespace: SonataFlowNamespace}, sonataFlowClusterPlatformCRD); err != nil {
		if apierrors.IsNotFound(err) {
			// CRD does not exist
			sfLogger.Info("CRD resource not found.", "SubscriptionName", ServerlessLogicSubscriptionName, "Namespace", SonataFlowNamespace)
			return err
		}
		sfLogger.Error(err, "Error occurred when retrieving CRD", "CRD", SonataFlowClusterPlatformCRDName)
		return err
	}

	// CRD exist; check and handle sonataflowclusterplatform CR
	if err := handleServerlessLogicCR(ctx, r.Client, orchestrator); err != nil {
		return err
	}
	sfLogger.Info("Successfully created SonataFlow Resources")
	return nil
}

func (r *OrchestratorReconciler) reconcileKnative(ctx context.Context, serverlessOperator orchestratorv1alpha1.ServerlessOperator) error {
	knativeLogger := log.FromContext(ctx)
	knativeLogger.Info("Starting Reconciliation for K-Native Serverless")

	// if subscription is disabled; check if subscription exists and handle delete
	if !serverlessOperator.Enabled {
		// handle cleanup
		if err := handleKnativeCleanUp(ctx, r.Client, r.OLMClient); err != nil {
			return err
		}
	}

	// Subscription is enabled;
	if err := handleKNativeOperatorInstallation(ctx, r.Client, r.OLMClient); err != nil {
		knativeLogger.Error(err, "Error occurred when installing Knative Operator resources")
		return err
	}

	if err := handleServerlessCR(ctx, r.Client); err != nil {
		knativeLogger.Error(err, "Error occurred when handling Knative custom resources")
		return err
	}

	return nil
}

func (r *OrchestratorReconciler) reconcileBackstage(
	ctx context.Context, wfNamespace string,
	argoCDEnabled, tektonEnabled bool,
	rhdhConfig orchestratorv1alpha1.RHDHConfig) error {

	logger := log.FromContext(ctx)
	logger.Info("Starting Reconciliation for Backstage")

	subscriptionName := rhdhConfig.RHDHName
	namespace := rhdhConfig.RHDHNamespace

	// if subscription is disabled; check if subscription exists and handle delete
	if !rhdhConfig.InstallOperator {
		if err := rhdh.HandleBackstageCleanup(ctx, r.Client, r.OLMClient, namespace); err != nil {
			logger.Error(err, "Error occurred when cleaning up backstage", "SubscriptionName", subscriptionName)
			return err
		}
	}

	if _, err := kube.CheckNamespaceExist(ctx, r.Client, namespace); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Error(err, "Ensure namespace already exist", "NS", namespace)
		}
		logger.Error(err, "Error occurred when checking namespace exists", "NS", namespace)
		return err
	}

	if err := rhdh.HandleRHDHOperatorInstallation(ctx, r.Client, r.OLMClient, namespace); err != nil {
		logger.Error(err, "Error occurred when installing RHDH Operator resources")
		return err
	}

	clusterDomain, _ := r.getClusterDomain(ctx)
	// create secret
	if err := rhdh.CreateBSSecret(namespace, ctx, r.Client); err != nil {
		return err
	}
	// create backstage CR
	if err := rhdh.HandleCRCreation(rhdhConfig, argoCDEnabled, tektonEnabled, wfNamespace, clusterDomain, ctx, r.Client); err != nil {
		return err
	}
	return nil
}

// getClusterDomain retrieves the OpenShift cluster domain from the Ingress resource
func (r *OrchestratorReconciler) getClusterDomain(ctx context.Context) (string, error) {
	gcdLogger := log.FromContext(ctx)
	ingress := &configv1.Ingress{}
	err := r.Get(ctx, client.ObjectKey{Name: "cluster"}, ingress)
	if err != nil {
		gcdLogger.Error(err, "Unable to retrieve OpenShift Ingress resource")
		return "", err
	}

	clusterDomain := ingress.Spec.Domain
	if ingress.Spec.Domain == "" {
		gcdLogger.Error(err, "Cluster domain not set in Ingress resource")
		return "", err
	}
	gcdLogger.Info("Successfully retrieved cluster domain", "Domain", clusterDomain)
	return clusterDomain, nil
}

func (r *OrchestratorReconciler) addFinalizers(ctx context.Context, orchestrator *orchestratorv1alpha1.Orchestrator) error {
	if !controllerutil.ContainsFinalizer(orchestrator, FinalizerCRCleanup) {
		controllerutil.AddFinalizer(orchestrator, FinalizerCRCleanup)
		if err := r.Update(ctx, orchestrator); err != nil {
			return err
		}
	}
	return nil
}

func (r *OrchestratorReconciler) handleCleanup(ctx context.Context, orchestrator *orchestratorv1alpha1.Orchestrator) error {
	// cleanup knative
	if err := handleKnativeCleanUp(ctx, r.Client, r.OLMClient); err != nil {
		return err
	}
	// cleanup sonataflow
	if err := handleSonataFlowCleanUp(ctx, r.Client, r.OLMClient); err != nil {
		return err
	}
	// cleanup backstage
	if err := rhdh.HandleBackstageCleanup(ctx, r.Client, r.OLMClient, orchestrator.Spec.RHDHConfig.RHDHNamespace); err != nil {
		return err
	}
	return nil
}

// UpdateStatus sets the status of orchestrator.
func (r *OrchestratorReconciler) UpdateStatus(ctx context.Context, orchestrator *orchestratorv1alpha1.Orchestrator, phase orchestratorv1alpha1.OrchestratorPhase, condition metav1.Condition) error {
	logger := log.FromContext(ctx)
	orchestrator.Status.Phase = phase
	meta.SetStatusCondition(&orchestrator.Status.Conditions, condition)
	//orchestrator.Status.Conditions = []metav1.Condition{condition}

	err := r.Status().Update(ctx, orchestrator)
	if err != nil {
		logger.Error(err, "Failed to update Orchestrator status")
		return err
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *OrchestratorReconciler) SetupWithManager(mgr ctrl.Manager) error {
	config := mgr.GetConfig()

	// Create the OLM clientset using the config
	olmClient, err := olmclientset.NewForConfig(config)
	if err != nil {
		return err
	}
	r.OLMClient = *olmClient

	return ctrl.NewControllerManagedBy(mgr).
		For(&orchestratorv1alpha1.Orchestrator{}).
		Owns(&orchestratorv1alpha1.Orchestrator{}).
		WithOptions(controller.Options{MaxConcurrentReconciles: 2}).
		Complete(r)
}
