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
	"time"

	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Shared test timeouts
const (
	defaultTimeout  = time.Second * 30
	defaultInterval = time.Second * 1
)

// TestResource represents a test resource with cleanup capability
type TestResource struct {
	Object client.Object
	Client client.Client
	ctx    context.Context
}

// NewTestResource creates a new test resource
func NewTestResource(obj client.Object, client client.Client, ctx context.Context) *TestResource {
	return &TestResource{
		Object: obj,
		Client: client,
		ctx:    ctx,
	}
}

// Create creates the test resource
func (tr *TestResource) Create() {
	Expect(tr.Client.Create(tr.ctx, tr.Object)).Should(Succeed())
}

// Delete deletes the test resource
func (tr *TestResource) Delete() {
	Expect(tr.Client.Delete(tr.ctx, tr.Object)).Should(Succeed())
}

// Get retrieves the test resource
func (tr *TestResource) Get() {
	Expect(tr.Client.Get(tr.ctx, types.NamespacedName{
		Name:      tr.Object.GetName(),
		Namespace: tr.Object.GetNamespace(),
	}, tr.Object)).Should(Succeed())
}

// Update updates the test resource
func (tr *TestResource) Update() {
	Expect(tr.Client.Update(tr.ctx, tr.Object)).Should(Succeed())
}

// WaitForExists waits for the resource to exist
func (tr *TestResource) WaitForExists(timeout time.Duration) {
	Eventually(func() error {
		return tr.Client.Get(tr.ctx, types.NamespacedName{
			Name:      tr.Object.GetName(),
			Namespace: tr.Object.GetNamespace(),
		}, tr.Object)
	}, timeout, time.Second).Should(Succeed())
}

// WaitForDeletion waits for the resource to be deleted
func (tr *TestResource) WaitForDeletion(timeout time.Duration) {
	Eventually(func() error {
		return tr.Client.Get(tr.ctx, types.NamespacedName{
			Name:      tr.Object.GetName(),
			Namespace: tr.Object.GetNamespace(),
		}, tr.Object)
	}, timeout, time.Second).ShouldNot(Succeed())
}

// TestNamespace represents a test namespace
type TestNamespace struct {
	*TestResource
}

// NewTestNamespace creates a new test namespace
func NewTestNamespace(name string, client client.Client, ctx context.Context) *TestNamespace {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				"turbo.ibm.com/override": "true",
			},
		},
	}

	return &TestNamespace{
		TestResource: NewTestResource(ns, client, ctx),
	}
}

// TestDeployment represents a test deployment
type TestDeployment struct {
	*TestResource
}

// NewTestDeployment creates a new test deployment
func NewTestDeployment(name, namespace string, client client.Client, ctx context.Context) *TestDeployment {
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: func() *int32 { val := int32(1); return &val }(),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": name,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": name,
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "main",
							Image: "nginx:latest",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("1"),
									corev1.ResourceMemory: resource.MustParse("1Gi"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("2"),
									corev1.ResourceMemory: resource.MustParse("2Gi"),
								},
							},
						},
					},
				},
			},
		},
	}

	return &TestDeployment{
		TestResource: NewTestResource(deployment, client, ctx),
	}
}

// SetResources sets the resource requirements for the deployment
func (td *TestDeployment) SetResources(cpuRequest, cpuLimit, memoryRequest, memoryLimit string) {
	deployment := td.Object.(*appsv1.Deployment)
	deployment.Spec.Template.Spec.Containers[0].Resources = corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse(cpuRequest),
			corev1.ResourceMemory: resource.MustParse(memoryRequest),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse(cpuLimit),
			corev1.ResourceMemory: resource.MustParse(memoryLimit),
		},
	}
}

// GetResources returns the current resource requirements
func (td *TestDeployment) GetResources() corev1.ResourceRequirements {
	deployment := td.Object.(*appsv1.Deployment)
	return deployment.Spec.Template.Spec.Containers[0].Resources
}

// SetAnnotation sets an annotation on the deployment
func (td *TestDeployment) SetAnnotation(key, value string) {
	deployment := td.Object.(*appsv1.Deployment)
	if deployment.Annotations == nil {
		deployment.Annotations = make(map[string]string)
	}
	deployment.Annotations[key] = value
}

// GetAnnotation gets an annotation from the deployment
func (td *TestDeployment) GetAnnotation(key string) string {
	deployment := td.Object.(*appsv1.Deployment)
	return deployment.Annotations[key]
}

// TestSuite represents a test suite with common setup and teardown
type TestSuite struct {
	Namespace   *TestNamespace
	Client      client.Client
	TurboClient client.Client
	ctx         context.Context
	config      *TestConfig
}

// NewTestSuite creates a new test suite
func NewTestSuite(namespaceName string, client, turboClient client.Client, ctx context.Context, config *TestConfig) *TestSuite {
	return &TestSuite{
		Namespace:   NewTestNamespace(namespaceName, client, ctx),
		Client:      client,
		TurboClient: turboClient,
		ctx:         ctx,
		config:      config,
	}
}

// Setup creates the test namespace
func (ts *TestSuite) Setup() {
	ts.Namespace.Create()
	ts.Namespace.WaitForExists(ts.config.TestTimeout)
}

// Teardown deletes the test namespace
func (ts *TestSuite) Teardown() {
	ts.Namespace.Delete()
	ts.Namespace.WaitForDeletion(ts.config.TestTimeout)
}

// CreateDeployment creates a deployment in the test namespace
func (ts *TestSuite) CreateDeployment(name string) *TestDeployment {
	deployment := NewTestDeployment(name, ts.Namespace.Object.GetName(), ts.Client, ts.ctx)
	deployment.Create()
	deployment.WaitForExists(ts.config.TestTimeout)
	return deployment
}

// CreateDeploymentWithTurbo creates a deployment using the Turbonomic client
func (ts *TestSuite) CreateDeploymentWithTurbo(name string) *TestDeployment {
	deployment := NewTestDeployment(name, ts.Namespace.Object.GetName(), ts.TurboClient, ts.ctx)
	deployment.Create()
	deployment.WaitForExists(ts.config.TestTimeout)
	return deployment
}

// AssertResourceValues asserts that the deployment has the expected resource values
func AssertResourceValues(deployment *TestDeployment, expectedCPURequest, expectedCPULimit, expectedMemoryRequest, expectedMemoryLimit string) {
	resources := deployment.GetResources()
	Expect(resources.Requests[corev1.ResourceCPU]).Should(Equal(resource.MustParse(expectedCPURequest)))
	Expect(resources.Requests[corev1.ResourceMemory]).Should(Equal(resource.MustParse(expectedMemoryRequest)))
	Expect(resources.Limits[corev1.ResourceCPU]).Should(Equal(resource.MustParse(expectedCPULimit)))
	Expect(resources.Limits[corev1.ResourceMemory]).Should(Equal(resource.MustParse(expectedMemoryLimit)))
}

// AssertAnnotationValue asserts that the deployment has the expected annotation value
func AssertAnnotationValue(deployment *TestDeployment, key, expectedValue string) {
	Eventually(func() string {
		deployment.Get()
		return deployment.GetAnnotation(key)
	}, defaultTimeout, defaultInterval).Should(Equal(expectedValue))
}

// WaitForResourceChange waits for the deployment resources to change to expected values
func WaitForResourceChange(deployment *TestDeployment, expectedCPURequest, expectedCPULimit, expectedMemoryRequest, expectedMemoryLimit string, timeout time.Duration) {
	Eventually(func() bool {
		deployment.Get()
		resources := deployment.GetResources()
		return resources.Requests[corev1.ResourceCPU].Equal(resource.MustParse(expectedCPURequest)) &&
			resources.Requests[corev1.ResourceMemory].Equal(resource.MustParse(expectedMemoryRequest)) &&
			resources.Limits[corev1.ResourceCPU].Equal(resource.MustParse(expectedCPULimit)) &&
			resources.Limits[corev1.ResourceMemory].Equal(resource.MustParse(expectedMemoryLimit))
	}, timeout, defaultInterval).Should(BeTrue())
}
