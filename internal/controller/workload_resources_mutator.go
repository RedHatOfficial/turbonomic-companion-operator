/*
Copyright 2025 Marek Paterczyk

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
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	// "sigs.k8s.io/controller-runtime/pkg/runtime/inject"

	"github.com/RedHatOfficial/turbonomic-companion-operator/internal/metrics"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

const (
	managedAnnotation = "turbo.ibm.com/override"
)

// +kubebuilder:webhook:path=/mutate-v1-obj,admissionReviewVersions=v1,mutating=true,failurePolicy=Ignore,groups=apps,resources=deployments,verbs=update,versions=v1,name=workloadmutator.turbo.ibm.com,sideEffects=NoneOnDryRun
type WorkloadResourcesMutator struct {
	Client                       client.Client
	Decoder                      *admission.Decoder
	Log                          logr.Logger
	IgnoreArgoCDManagedResources bool
}

// implements admission.Handler.
var _ admission.Handler = &WorkloadResourcesMutator{}

func (a *WorkloadResourcesMutator) Handle(ctx context.Context, req admission.Request) admission.Response {
	if req.DryRun != nil && *req.DryRun {
		return admission.Allowed("Allowing the object because dryRun=true")
	}

	log := a.Log.WithValues("ObjectType", req.Kind.Kind, "ObjectName", req.Name, "ObjectNamespace", req.Namespace, "Username", req.UserInfo.Username)

	if strings.HasPrefix(req.Namespace, "kube") ||
		strings.HasPrefix(req.Namespace, "openshift") {
		log.Info("Passing through this request because it's a system namespace. Consider excluding those with namespaceSelector in the webhook configuration.")
		return admission.Allowed("")
	}

	log.V(5).Info("About to decode the request", "Request", req)

	if a.Decoder == nil {
		return admission.Errored(http.StatusInternalServerError, errors.New("Decoder not initialized, webhook was not setup correctly!"))
	}

	incomingObject := &unstructured.Unstructured{}
	err := (*a.Decoder).Decode(req, incomingObject)
	if err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}
	log.V(3).Info("Decoded", "incomingObject", incomingObject, "annotations", incomingObject.GetAnnotations())
	annotations := incomingObject.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}

	// This webhook supports only UPDATE, so the old object needs to exist
	oldObject := &unstructured.Unstructured{}
	err = (*a.Decoder).DecodeRaw(req.OldObject, oldObject)
	if err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}
	log.V(3).Info("Decoded", "oldObject", oldObject, "annotations", oldObject.GetAnnotations())

	oldObjectAnnotations := oldObject.GetAnnotations()
	if oldObjectAnnotations == nil {
		oldObjectAnnotations = map[string]string{}
	}

	if a.IgnoreArgoCDManagedResources && isArgoCDManagedResource(incomingObject.GetLabels(), incomingObject.GetAnnotations()) {
		if annotations[managedAnnotation] != "true" && oldObjectAnnotations[managedAnnotation] != "true" {
			log.V(2).Info("Passing through the incoming object since it's managed by ArgoCD and IgnoreArgoCDManagedResources=true")
			return admission.Allowed("")
		} else {
			log.V(2).Info("Processing this object, despite it being managed by ArgoCD and IgnoreArgoCDManagedResources=true, since it is explicitly annotated for override")
		}
	}

	if req.UserInfo.Username == turboSA {
		log.V(3).Info("Turbonomic is making this change")

		// saving metrics on Turbonomic resource recommendations for each container
		// WARNING: the webhook has no way of knowing if the action was executed successfully or not,
		// so it tracks Turbo attempts to change resources rather than effective/successful changes
		// TODO: tracking actions belongs to kubeturbo really, as kubeturbo has all the context
		trackResourcesByContainer([]string{req.Namespace, req.Kind.Kind, req.Name}, incomingObject, &log)

		if _, exists := annotations[managedAnnotation]; !exists {
			annotations[managedAnnotation] = "true"
			incomingObject.SetAnnotations(annotations)
			log.Info("Annotated incoming object as managed by Turbonomic")

			marshaledObj, err := json.Marshal(incomingObject)
			if err != nil {
				return admission.Errored(http.StatusInternalServerError, err)
			}
			return admission.PatchResponseFromRaw(req.Object.Raw, marshaledObj)
		}

	} else {
		log.V(3).Info("Turbonomic is NOT making this change")

		if value, exists := annotations[managedAnnotation]; exists {
			if value != "true" {
				log.V(3).Info("This resource is explicitly annotated to NOT do the override", managedAnnotation, value)
				return admission.Allowed("")
			}
		}

		if oldObjectAnnotations[managedAnnotation] == "true" || annotations[managedAnnotation] == "true" {
			log.V(3).Info("This is a Turbonomic managed object. Override resources.")

			annotations[managedAnnotation] = "true"
			incomingObject.SetAnnotations(annotations)

			log.V(3).Info("Overriding compute resources")
			err = copyResourcesByContainer(oldObject, incomingObject, &log)
			if err != nil {
				return admission.Errored(http.StatusInternalServerError, err)
			}

			marshaledObj, err := json.Marshal(incomingObject)
			if err != nil {
				return admission.Errored(http.StatusInternalServerError, err)
			}
			log.Info("Successfully overrode compute resources")
			metrics.TurboOverridesTotal.WithLabelValues(req.Namespace, req.Kind.Kind, req.Name).Inc()
			return admission.PatchResponseFromRaw(req.Object.Raw, marshaledObj)
		}
	}

	return admission.Allowed("")
}

// copyResourcesByContainer copies the "resources" field from source to target Unstructured objects.
// courtesy of ChatGPT
func copyResourcesByContainer(source, target *unstructured.Unstructured, log *logr.Logger) error {
	srcContainers, found, err := unstructured.NestedSlice(source.Object, "spec", "template", "spec", "containers")
	if err != nil || !found {
		return fmt.Errorf("failed to retrieve containers from source: %v", err)
	}

	tgtContainers, found, err := unstructured.NestedSlice(target.Object, "spec", "template", "spec", "containers")
	if err != nil || !found {
		return fmt.Errorf("failed to retrieve containers from target: %v", err)
	}

	// Create a map for quick lookup by container name
	srcResourcesMap := make(map[string]interface{})
	for _, container := range srcContainers {
		if cMap, ok := container.(map[string]interface{}); ok {
			name, _, _ := unstructured.NestedString(cMap, "name")
			if res, found, _ := unstructured.NestedMap(cMap, "resources"); found {
				srcResourcesMap[name] = res
			}
		}
	}

	// Update target containers with source resources
	for i, container := range tgtContainers {
		if cMap, ok := container.(map[string]interface{}); ok {
			name, _, _ := unstructured.NestedString(cMap, "name")
			if res, found := srcResourcesMap[name]; found {
				log.V(1).Info("Overriding container resources", "containerName", name, "resources", res)
				_ = unstructured.SetNestedMap(cMap, res.(map[string]interface{}), "resources")
				tgtContainers[i] = cMap
			}
		}
	}

	// Save back to target object
	err = unstructured.SetNestedSlice(target.Object, tgtContainers, "spec", "template", "spec", "containers")
	log.V(5).Info("Updated containers", "containers", tgtContainers)
	if err != nil {
		return err
	} else {
		return nil
	}
}

// https://argo-cd.readthedocs.io/en/stable/user-guide/annotations-and-labels/
// https://argo-cd.readthedocs.io/en/stable/user-guide/resource_tracking/
func isArgoCDManagedResource(labels map[string]string, annotations map[string]string) bool {
	if labels != nil {
		if _, exists := labels["app.kubernetes.io/instance"]; exists {
			return true
		}
		if _, exists := labels["argocd.argoproj.io/instance"]; exists {
			return true
		}
	}
	if annotations != nil {
		if _, exists := annotations["argocd.argoproj.io/tracking-id"]; exists {
			return true
		}
	}
	return false
}

// log compute resources recommended by Turbonomic
func trackResourcesByContainer(workloadDimensions []string, source *unstructured.Unstructured, log *logr.Logger) error {
	srcContainers, found, err := unstructured.NestedSlice(source.Object, "spec", "template", "spec", "containers")
	if err != nil || !found {
		// this should never happen - containers need to be there
		return fmt.Errorf("failed to retrieve containers from %s: %v", workloadDimensions, err)
	}

	// Create a map for quick lookup by container name
	srcResourcesMap := make(map[string]interface{})
	for _, container := range srcContainers {
		if cMap, ok := container.(map[string]interface{}); ok {
			name, _, _ := unstructured.NestedString(cMap, "name")
			if res, found, _ := unstructured.NestedMap(cMap, "resources"); found {
				srcResourcesMap[name] = res
			}
		}
	}

	metrics.TurboRecommendedTotal.WithLabelValues(workloadDimensions...).Inc()

	for containerName, resources := range srcResourcesMap {
		dimensions := append(workloadDimensions, containerName)

		if requests_cpu, _ := getParsedResource(resources.(map[string]interface{}), log, "requests", "cpu"); requests_cpu != nil {
			metrics.TurboRecommendedRequestCpuGauge.WithLabelValues(dimensions...).Set(float64(requests_cpu.MilliValue()))
		}

		if limits_cpu, _ := getParsedResource(resources.(map[string]interface{}), log, "limits", "cpu"); limits_cpu != nil {
			metrics.TurboRecommendedLimitCpuGauge.WithLabelValues(dimensions...).Set(float64(limits_cpu.MilliValue()))
		}

		if requests_memory, _ := getParsedResource(resources.(map[string]interface{}), log, "requests", "memory"); requests_memory != nil {
			metrics.TurboRecommendedRequestMemoryGauge.WithLabelValues(dimensions...).Set(float64(requests_memory.Value()))
		}

		if limits_memory, _ := getParsedResource(resources.(map[string]interface{}), log, "limits", "memory"); limits_memory != nil {
			metrics.TurboRecommendedLimitMemoryGauge.WithLabelValues(dimensions...).Set(float64(limits_memory.Value()))
		}

	}

	return nil
}

func getParsedResource(resources map[string]interface{}, log *logr.Logger, resourceType string, kind string) (*resource.Quantity, error) {
	value_str, found, err := unstructured.NestedString(resources, resourceType, kind)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve %s/%s: %v", resourceType, kind, err)
	}
	if found {
		value, err := resource.ParseQuantity(value_str)
		if err != nil {
			return nil, fmt.Errorf("failed to parse quantity %s/%s: %v", resourceType, kind, err)
		}
		return &value, nil
	}

	return nil, nil
}
