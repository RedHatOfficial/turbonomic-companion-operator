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

// Resource management modes
const (
	ManagementModeAll   = "all"   // Manage both CPU and memory (same as "true")
	ManagementModeTrue  = "true"  // Manage both CPU and memory (legacy)
	ManagementModeCPU   = "cpu"   // Manage CPU only, allow others to manage memory
	ManagementModeFalse = "false" // Explicitly disable management
)

// isWorkloadManaged checks if the workload is managed by Turbonomic
// Returns true for values: "true", "all", "cpu"
// Returns false for values: "false", any other value, or when annotation is missing
func isWorkloadManaged(annotations map[string]string) bool {
	if annotations == nil {
		return false
	}
	value, exists := annotations[managedAnnotation]
	if !exists {
		return false
	}
	// Explicitly handle the "false" case and any other invalid values
	if value == ManagementModeFalse {
		return false
	}
	return true
}

// getManagementMode returns the management mode for the resource
func getManagementMode(annotations map[string]string) string {
	if annotations == nil {
		return ""
	}
	value, exists := annotations[managedAnnotation]
	if !exists {
		return ""
	}
	// Normalize "true" to "all" for consistency
	if value == ManagementModeTrue {
		return ManagementModeAll
	}
	return value
}

// shouldManageResource checks if a specific resource type should be managed
func shouldManageResource(managementMode, resourceType string) bool {
	switch managementMode {
	case ManagementModeAll:
		return true
	case ManagementModeCPU:
		return resourceType == "cpu"
	default:
		return false
	}
}

// +kubebuilder:webhook:path=/mutate-v1-obj,admissionReviewVersions=v1,mutating=true,failurePolicy=Ignore,groups=apps,resources=deployments;statefulsets,verbs=update,versions=v1,name=workloadmutator.turbo.ibm.com,sideEffects=NoneOnDryRun
type WorkloadResourcesMutator struct {
	Client  client.Client
	Decoder *admission.Decoder
	Log     logr.Logger
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
		return admission.Errored(http.StatusInternalServerError, errors.New("decoder not initialized, webhook was not setup correctly"))
	}

	incomingObject := &unstructured.Unstructured{}
	err := (*a.Decoder).Decode(req, incomingObject)
	if err != nil {
		log.Error(err, "failed to decode incoming object")
		return admission.Errored(http.StatusBadRequest, err)
	}
	log.V(4).Info("Decoded", "incomingObject", incomingObject, "annotations", incomingObject.GetAnnotations())
	annotations := incomingObject.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}

	// This webhook supports only UPDATE, so the old object needs to exist
	oldObject := &unstructured.Unstructured{}
	err = (*a.Decoder).DecodeRaw(req.OldObject, oldObject)
	if err != nil {
		log.Error(err, "failed to decode old object")
		return admission.Errored(http.StatusBadRequest, err)
	}
	log.V(4).Info("Decoded", "oldObject", oldObject, "annotations", oldObject.GetAnnotations())

	oldObjectAnnotations := oldObject.GetAnnotations()
	if oldObjectAnnotations == nil {
		oldObjectAnnotations = map[string]string{}
	}

	if req.UserInfo.Username == turboSA {
		log.V(3).Info("Turbonomic is making this change")

		// saving metrics on Turbonomic resource recommendations for each container
		// WARNING: the webhook has no way of knowing if the action was executed successfully or not,
		// so it tracks Turbo attempts to change resources rather than effective/successful changes
		// TODO: tracking actions belongs to kubeturbo really, as kubeturbo has all the context
		trackResourcesByContainer([]string{req.Namespace, req.Kind.Kind, req.Name}, incomingObject, &log)

		if _, exists := annotations[managedAnnotation]; !exists {
			// Analyze what resources Turbonomic is changing to set the appropriate mode
			changedResources := analyzeResourceChanges(oldObject, incomingObject, &log)
			newMode := determineManagementMode("", changedResources)

			annotations[managedAnnotation] = newMode
			incomingObject.SetAnnotations(annotations)
			log.Info("Annotated incoming object as managed by Turbonomic", "mode", newMode, "changedResources", changedResources)

			marshaledObj, err := json.Marshal(incomingObject)
			if err != nil {
				log.Error(err, "failed to marshal incoming object")
				return admission.Errored(http.StatusInternalServerError, err)
			}
			return admission.PatchResponseFromRaw(req.Object.Raw, marshaledObj)
		} else {
			// Update existing annotation based on what Turbonomic is changing
			currentMode := getManagementMode(annotations)
			changedResources := analyzeResourceChanges(oldObject, incomingObject, &log)
			newMode := determineManagementMode(currentMode, changedResources)

			if newMode != currentMode {
				annotations[managedAnnotation] = newMode
				incomingObject.SetAnnotations(annotations)
				log.Info("Updated management mode based on Turbonomic changes", "oldMode", currentMode, "newMode", newMode, "changedResources", changedResources)

				marshaledObj, err := json.Marshal(incomingObject)
				if err != nil {
					log.Error(err, "failed to marshal incoming object")
					return admission.Errored(http.StatusInternalServerError, err)
				}
				return admission.PatchResponseFromRaw(req.Object.Raw, marshaledObj)
			}
		}

	} else {
		log.V(3).Info("Turbonomic is NOT making this change")

		if value, exists := annotations[managedAnnotation]; exists {
			if !isWorkloadManaged(annotations) {
				log.V(3).Info("This resource is explicitly annotated to NOT do the override", managedAnnotation, value)
				return admission.Allowed("")
			}
		}

		if isWorkloadManaged(oldObjectAnnotations) || isWorkloadManaged(annotations) {
			// Determine the management mode to use
			managementMode := getManagementMode(annotations)
			if managementMode == "" {
				managementMode = getManagementMode(oldObjectAnnotations)
			}

			log.V(3).Info("This is a Turbonomic managed object. Override resources.", "managementMode", managementMode)

			// Preserve the original management mode (don't overwrite "cpu" with "true")
			if managementMode != "" {
				annotations[managedAnnotation] = managementMode
			}
			incomingObject.SetAnnotations(annotations)

			log.V(3).Info("Overriding compute resources", "mode", managementMode)
			err = copyResourcesByContainerSelective(oldObject, incomingObject, managementMode, &log)
			if err != nil {
				log.Error(err, "Could not copy container resources")
				return admission.Errored(http.StatusInternalServerError, err)
			}

			marshaledObj, err := json.Marshal(incomingObject)
			if err != nil {
				log.Error(err, "Could not marshall the incoming object")
				return admission.Errored(http.StatusInternalServerError, err)
			}
			log.Info("Successfully overrode compute resources")
			metrics.TurboOverridesTotal.WithLabelValues(req.Namespace, req.Kind.Kind, req.Name).Inc()
			return admission.PatchResponseFromRaw(req.Object.Raw, marshaledObj)
		}
	}

	return admission.Allowed("")
}

// copyResourcesByContainerSelective copies selective resource fields from source to target based on management mode
func copyResourcesByContainerSelective(source, target *unstructured.Unstructured, managementMode string, log *logr.Logger) error {
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

	// Update target containers with selective source resources
	for i, container := range tgtContainers {
		if cMap, ok := container.(map[string]interface{}); ok {
			name, _, _ := unstructured.NestedString(cMap, "name")
			if srcRes, found := srcResourcesMap[name]; found {
				srcResourcesMap := srcRes.(map[string]interface{})

				// Get or create the target container's resources
				tgtRes, _, _ := unstructured.NestedMap(cMap, "resources")
				if tgtRes == nil {
					tgtRes = make(map[string]interface{})
				}

				// Copy resources selectively based on management mode
				if err := copySelectiveResources(srcResourcesMap, tgtRes, managementMode, log, name); err != nil {
					return fmt.Errorf("failed to copy selective resources for container %s: %v", name, err)
				}

				log.V(1).Info("Overriding container resources selectively", "containerName", name, "mode", managementMode, "resources", tgtRes)
				_ = unstructured.SetNestedMap(cMap, tgtRes, "resources")
				tgtContainers[i] = cMap
			}
		}
	}

	// Save back to target object
	err = unstructured.SetNestedSlice(target.Object, tgtContainers, "spec", "template", "spec", "containers")
	log.V(5).Info("Updated containers", "containers", tgtContainers)
	if err != nil {
		return err
	}
	return nil
}

// copySelectiveResources copies specific resource types based on management mode
func copySelectiveResources(src, tgt map[string]interface{}, managementMode string, log *logr.Logger, containerName string) error {
	resourceCategories := []string{"requests", "limits"}
	resourceTypes := []string{"cpu", "memory"}

	for _, category := range resourceCategories {
		srcCategory, srcCategoryExists, _ := unstructured.NestedMap(src, category)
		if !srcCategoryExists {
			continue
		}

		// Get or create target category
		tgtCategory, _, _ := unstructured.NestedMap(tgt, category)
		if tgtCategory == nil {
			tgtCategory = make(map[string]interface{})
		}

		for _, resourceType := range resourceTypes {
			if shouldManageResource(managementMode, resourceType) {
				if value, exists := srcCategory[resourceType]; exists {
					tgtCategory[resourceType] = value
					log.V(2).Info("Copied resource", "container", containerName, "category", category, "type", resourceType, "value", value)
				}
			}
		}

		// Only set the category if we copied something to it
		if len(tgtCategory) > 0 {
			_ = unstructured.SetNestedMap(tgt, tgtCategory, category)
		}
	}

	return nil
}

// ResourceChangeInfo tracks what types of resources have changed
type ResourceChangeInfo struct {
	CPUChanged    bool
	MemoryChanged bool
}

// analyzeResourceChanges compares old and new objects to determine what resources Turbonomic changed
func analyzeResourceChanges(oldObject, newObject *unstructured.Unstructured, log *logr.Logger) ResourceChangeInfo {
	changes := ResourceChangeInfo{}

	oldContainers, oldFound, err := unstructured.NestedSlice(oldObject.Object, "spec", "template", "spec", "containers")
	if err != nil || !oldFound {
		log.V(3).Info("Could not retrieve old containers for change analysis", "error", err)
		return changes
	}

	newContainers, newFound, err := unstructured.NestedSlice(newObject.Object, "spec", "template", "spec", "containers")
	if err != nil || !newFound {
		log.V(3).Info("Could not retrieve new containers for change analysis", "error", err)
		return changes
	}

	// Create maps for easy lookup by container name
	oldResourcesMap := createContainerResourceMap(oldContainers)
	newResourcesMap := createContainerResourceMap(newContainers)

	// Compare resources for each container
	for containerName, newResources := range newResourcesMap {
		oldResources, exists := oldResourcesMap[containerName]
		if !exists {
			// New container - assume all resources changed
			changes.CPUChanged = true
			changes.MemoryChanged = true
			continue
		}

		// Check each resource category and type
		for _, category := range []string{"requests", "limits"} {
			for _, resourceType := range []string{"cpu", "memory"} {
				oldValue := getResourceValue(oldResources, category, resourceType)
				newValue := getResourceValue(newResources, category, resourceType)

				if oldValue != newValue {
					log.V(2).Info("Resource change detected", "container", containerName, "category", category, "type", resourceType, "old", oldValue, "new", newValue)
					if resourceType == "cpu" {
						changes.CPUChanged = true
					} else if resourceType == "memory" {
						changes.MemoryChanged = true
					}
				}
			}
		}
	}

	return changes
}

// createContainerResourceMap creates a map of container name to resources
func createContainerResourceMap(containers []interface{}) map[string]interface{} {
	resourcesMap := make(map[string]interface{})
	for _, container := range containers {
		if cMap, ok := container.(map[string]interface{}); ok {
			name, _, _ := unstructured.NestedString(cMap, "name")
			if res, found, _ := unstructured.NestedMap(cMap, "resources"); found {
				resourcesMap[name] = res
			}
		}
	}
	return resourcesMap
}

// getResourceValue extracts a specific resource value as a string
func getResourceValue(resources interface{}, category, resourceType string) string {
	if resMap, ok := resources.(map[string]interface{}); ok {
		if categoryMap, catExists, _ := unstructured.NestedMap(resMap, category); catExists {
			if value, exists := categoryMap[resourceType]; exists {
				return fmt.Sprintf("%v", value)
			}
		}
	}
	return ""
}

// determineManagementMode determines the appropriate management mode based on current mode and changes
func determineManagementMode(currentMode string, changes ResourceChangeInfo) string {
	// If memory was changed, always upgrade to "all"
	if changes.MemoryChanged {
		return ManagementModeAll
	}

	// If only CPU changed
	if changes.CPUChanged {
		// If we're already managing all resources, keep it that way
		if currentMode == ManagementModeAll || currentMode == ManagementModeTrue {
			return ManagementModeAll
		}
		// Otherwise, set to CPU-only mode
		return ManagementModeCPU
	}

	// No resource changes detected, keep current mode or default to CPU if empty
	if currentMode == "" {
		return ManagementModeCPU
	}
	return currentMode
}

// log compute resources recommended by Turbonomic
func trackResourcesByContainer(workloadDimensions []string, source *unstructured.Unstructured, log *logr.Logger) error {
	srcContainers, found, err := unstructured.NestedSlice(source.Object, "spec", "template", "spec", "containers")
	if err != nil || !found {
		// this should never happen - containers need to be there
		log.Error(err, "failed to retrieve containers from %s", workloadDimensions)
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
		log.Error(err, "failed to retrieve %s/%s", resourceType, kind)
		return nil, fmt.Errorf("failed to retrieve %s/%s: %v", resourceType, kind, err)
	}
	if found {
		value, err := resource.ParseQuantity(value_str)
		if err != nil {
			log.Error(err, "failed to parse quantify %s/%s", resourceType, kind)
			return nil, fmt.Errorf("failed to parse quantity %s/%s: %v", resourceType, kind, err)
		}
		return &value, nil
	}

	return nil, nil
}
