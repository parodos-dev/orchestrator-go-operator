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
	olmclientset "github.com/operator-framework/operator-lifecycle-manager/pkg/api/client/clientset/versioned"
	"github.com/parodos-dev/orchestrator-operator/internal/controller/kube"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	knative "knative.dev/operator/pkg/apis/operator/v1beta1"
	"reflect"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	KnativeAPIVersion             = "operator.knative.dev/v1beta1"
	KnativeServingKind            = "KnativeServing"
	KnativeServingNamespacedName  = "knative-serving"
	KnativeEventingKind           = "KnativeEventing"
	KnativeEventingNamespacedName = "knative-eventing"
	KnativeEventingCRDName        = "knativeeventings.operator.knative.dev"
	KnativeServingCRDName         = "knativeservings.operator.knative.dev"
	KnativeSubscriptionName       = "serverless-operator"
	KnativeSubscriptionNamespace  = "openshift-serverless"
	KnativeSubscriptionChannel    = "stable"
)

func handleKNativeOperatorInstallation(ctx context.Context, client client.Client, olmClientSet olmclientset.Clientset) error {
	knativeLogger := log.FromContext(ctx)

	if _, err := kube.CheckNamespaceExist(ctx, client, KnativeSubscriptionNamespace); err != nil {
		if apierrors.IsNotFound(err) {
			knativeLogger.Info("Creating namespace", "NS", KnativeSubscriptionNamespace)
			if err := kube.CreateNamespace(ctx, client, KnativeSubscriptionNamespace); err != nil {
				knativeLogger.Error(err, "Error occurred when creating namespace", "NS", KnativeSubscriptionNamespace)
				return nil
			}
		}
		knativeLogger.Error(err, "Error occurred when checking namespace exist", "NS", KnativeSubscriptionNamespace)
		return err
	}

	serverlessSubscription := kube.CreateSubscriptionObject(
		KnativeSubscriptionName,
		KnativeSubscriptionNamespace,
		KnativeSubscriptionChannel,
		"")

	// check if subscription exists
	subscriptionExists, existingSubscription, err := kube.CheckSubscriptionExists(ctx, olmClientSet, serverlessSubscription)
	if err != nil {
		knativeLogger.Error(err, "Error occurred when checking subscription exists", "SubscriptionName", ServerlessLogicSubscriptionName)
		return err
	}
	if !subscriptionExists {
		if err := kube.InstallOperatorViaSubscription(ctx, client, olmClientSet, kube.ServerlessOperatorGroupName, serverlessSubscription); err != nil {
			knativeLogger.Error(err, "Error occurred when installing operator", "SubscriptionName", ServerlessLogicSubscriptionName)
			return err
		}
		knativeLogger.Info("Operator successfully installed", "SubscriptionName", ServerlessLogicSubscriptionName)
	}

	if subscriptionExists {
		// Compare the current and desired state
		if !reflect.DeepEqual(existingSubscription.Spec, serverlessSubscription.Spec) {
			// Set owner reference for proper garbage collection
			//if err := controllerutil.SetControllerReference(&orchestrator, oslSubscription, r.Scheme); err != nil {
			//	return err
			//}

			// Update the existing subscription with the new Spec
			existingSubscription.Spec = serverlessSubscription.Spec
			if err := client.Update(ctx, existingSubscription); err != nil {
				return err
			}
		}
	}
	return nil
}

func handleServerlessCR(ctx context.Context, client client.Client) error {
	knativeLogger := log.FromContext(ctx)
	knativeLogger.Info("Handling Serverless Custom Resources...")

	// subscription exists; check if CRD exists for knative eventing;
	if err := kube.CheckCRDExists(ctx, client, KnativeEventingCRDName, KnativeSubscriptionNamespace); err != nil {
		if apierrors.IsNotFound(err) {
			knativeLogger.Info("CRD resource not found or ready", "SubscriptionName", KnativeSubscriptionName)
			return err
		}
		knativeLogger.Error(err, "Error occurred when retrieving CRD", "CRD", KnativeEventingCRDName)
		return err
	}
	// CRD exist; check and handle knative eventing CR
	if err := handleKnativeEventingCR(ctx, client); err != nil {
		knativeLogger.Error(err, "Error occurred when creating Knative EventingCR", "CR-Name", KnativeEventingNamespacedName)
		return err
	}

	// subscription exists; check if CRD exists knative serving;
	if err := kube.CheckCRDExists(ctx, client, KnativeServingCRDName, KnativeSubscriptionNamespace); err != nil {
		if apierrors.IsNotFound(err) {
			knativeLogger.Info("CRD resource not found or ready", "SubscriptionName", KnativeSubscriptionName)
			return nil
		}
		knativeLogger.Error(err, "Error occurred when retrieving CRD", "CRD", KnativeServingCRDName)
		return err

	}
	// CRD exist; check and handle knative eventing CR
	if err := handleKnativeServingCR(ctx, client); err != nil {
		knativeLogger.Error(err, "Error occurred when creating Knative ServingCR", "CR-Name", KnativeServingNamespacedName)
		return err
	}
	return nil
}

func handleKnativeEventingCR(ctx context.Context, client client.Client) error {
	logger := log.FromContext(ctx)
	logger.Info("Handling K-Native Eventing CR")
	// check CR exists
	knativeEventingCR := &knative.KnativeEventing{}
	err := client.Get(ctx, types.NamespacedName{Name: KnativeEventingNamespacedName, Namespace: KnativeEventingNamespacedName}, knativeEventingCR)
	if err == nil {
		// update CR TODO
		return nil
	} else {
		if apierrors.IsNotFound(err) {
			knEventing := &knative.KnativeEventing{
				TypeMeta: metav1.TypeMeta{
					APIVersion: KnativeAPIVersion,
					Kind:       KnativeEventingKind,
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      KnativeEventingNamespacedName,
					Namespace: KnativeEventingNamespacedName,
					Labels:    kube.AddLabel(),
				},
				Spec: knative.KnativeEventingSpec{},
			}
			if err = client.Create(ctx, knEventing); err != nil {
				logger.Error(err, "Error occurred when creating CR resource", "CR-Name", knEventing.Name)
			}
			logger.Info("Successfully created Knative Eventing resource", "CR-Name", knEventing.Name)
		}
	}
	return err
}

func handleKnativeServingCR(ctx context.Context, client client.Client) error {
	logger := log.FromContext(ctx)
	logger.Info("Handling K-Native Serving CR")
	// check CR exists
	knativeServingCR := &knative.KnativeServing{}
	err := client.Get(ctx, types.NamespacedName{Name: KnativeServingNamespacedName, Namespace: KnativeServingNamespacedName}, knativeServingCR)
	if err == nil {
		// update CR TODO
		return nil
	}
	if apierrors.IsNotFound(err) {
		knServing := &knative.KnativeServing{
			TypeMeta: metav1.TypeMeta{
				APIVersion: KnativeAPIVersion,
				Kind:       KnativeServingKind,
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      KnativeServingNamespacedName,
				Namespace: KnativeServingNamespacedName,
				Labels:    kube.AddLabel(),
			},
			Spec: knative.KnativeServingSpec{},
		}
		if err = client.Create(ctx, knServing); err != nil {
			logger.Error(err, "Error occurred when creating CR resource", "CR-Name", knServing.Name)
		}
		logger.Info("Successfully created Knative Serving resource", "CR-Name", knServing.Name)
	}
	return err
}

func handleKnativeCleanUp(ctx context.Context, client client.Client, olmClientSet olmclientset.Clientset) error {
	logger := log.FromContext(ctx)
	// remove all namespace
	if err := kube.CleanUpNamespace(ctx, KnativeEventingNamespacedName, client); err != nil {
		logger.Error(err, "Error occurred when deleting namespace", "NS", KnativeEventingNamespacedName)
		return err
	}
	if err := kube.CleanUpNamespace(ctx, KnativeServingNamespacedName, client); err != nil {
		logger.Error(err, "Error occurred when deleting namespace", "NS", KnativeServingNamespacedName)
		return err
	}
	// remove subscription and csv
	serverlessSubscription := kube.CreateSubscriptionObject(
		KnativeSubscriptionName,
		KnativeSubscriptionNamespace,
		KnativeSubscriptionChannel,
		"")

	if err := kube.CleanUpSubscriptionAndCSV(ctx, olmClientSet, serverlessSubscription); err != nil {
		logger.Error(err, "Error occurred when deleting Subscription and CSV", "Subscription", KnativeSubscriptionName)
		return err
	}
	// remove operarator group
	// remove all CRDs, optional (ensure all CRs and namespace have been removed first)
	return nil
}
