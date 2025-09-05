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

	"github.com/RedHatOfficial/turbonomic-companion-operator/internal/metrics"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/prometheus/client_golang/prometheus/testutil"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var _ = Describe("WorkloadResourcesMutator webhook", func() {

	log := logf.Log.WithName("workload-resources-mutator-test")

	ctx := context.Background()

	Describe("Workload", func() {

		Context("has compute resources managed by Turbonomic and is otherwise managed by CI/CD", func() {

			const (
				namespaceName = "resource-override-test"
				workloadName  = "workload"
			)

			It("should pass the initial request through unchanged", func() {

				By("Creating a Namespace")

				ns := &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: namespaceName,
						Labels: map[string]string{
							"turbo.ibm.com/override": "true",
						},
					},
				}
				Expect(k8sClient.Create(ctx, ns)).Should(Succeed())

				By("Creating a Workload")
				deployment := createDeployment(workloadName, namespaceName)
				Expect(k8sClient.Create(ctx, deployment)).Should(Succeed())

				By("Reading persisted Workload")
				workload := &appsv1.Deployment{}
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: workloadName, Namespace: namespaceName}, workload)).Should(Succeed())

				By("Checking that compute resources did not change")
				resources := workload.Spec.Template.Spec.Containers[0].Resources
				Expect(resources.Requests[corev1.ResourceCPU]).Should(Equal(resource.MustParse("1")))
				Expect(resources.Requests[corev1.ResourceMemory]).Should(Equal(resource.MustParse("1Gi")))
				Expect(resources.Limits[corev1.ResourceCPU]).Should(Equal(resource.MustParse("2")))
				Expect(resources.Limits[corev1.ResourceMemory]).Should(Equal(resource.MustParse("2Gi")))

				By("Initializing metrics")
				metrics.TurboOverridesTotal.WithLabelValues(namespaceName, "Deployment", workloadName).Inc()
			})

			It("should accept Turbonomic recommendation and annotate workload to enable override", func() {
				By("Reading persisted Workload")
				workload := &appsv1.Deployment{}
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: workloadName, Namespace: namespaceName}, workload)).Should(Succeed())

				By("Simulating changes made by Turbonomic")
				resources := workload.Spec.Template.Spec.Containers[0].Resources
				resources.Requests[corev1.ResourceCPU] = resource.MustParse("500m")
				resources.Requests[corev1.ResourceMemory] = resource.MustParse("2Gi")
				resources.Limits[corev1.ResourceCPU] = resource.MustParse("1")
				resources.Limits[corev1.ResourceMemory] = resource.MustParse("4Gi")
				Expect(k8sClientTurbo.Update(ctx, workload)).Should(Succeed())

				By("Checking that turbo.ibm.com/override annotation is set to true")
				Eventually(func() string {
					Expect(k8sClient.Get(ctx, types.NamespacedName{Name: workloadName, Namespace: namespaceName}, workload)).Should(Succeed())

					return workload.ObjectMeta.Annotations[managedAnnotation]
				}).Should(Equal("true"))

				By("Checking that compute resources changed to Turbonomic recommendation")
				resources = workload.Spec.Template.Spec.Containers[0].Resources
				Expect(resources.Requests[corev1.ResourceCPU]).Should(Equal(resource.MustParse("500m")))
				Expect(resources.Requests[corev1.ResourceMemory]).Should(Equal(resource.MustParse("2Gi")))
				Expect(resources.Limits[corev1.ResourceCPU]).Should(Equal(resource.MustParse("1")))
				Expect(resources.Limits[corev1.ResourceMemory]).Should(Equal(resource.MustParse("4Gi")))

				By("Ensuring that TurboOverridesTotal is NOT incremented")
				Expect(testutil.ToFloat64(metrics.TurboOverridesTotal.WithLabelValues(namespaceName, "Deployment", workloadName))).Should(Equal(float64(1)))

				By("Ensuring that TurboRecommendedTotal is incremented")
				Expect(testutil.ToFloat64(metrics.TurboRecommendedTotal.WithLabelValues(namespaceName, "Deployment", workloadName))).Should(Equal(float64(1)))

				By("Ensuring that metrics are set accordingly to Turbonomic recommendation")
				Expect(testutil.ToFloat64(metrics.TurboRecommendedLimitCpuGauge.WithLabelValues(namespaceName, "Deployment", workloadName, "main"))).Should(Equal(float64(1000)))
				Expect(testutil.ToFloat64(metrics.TurboRecommendedRequestCpuGauge.WithLabelValues(namespaceName, "Deployment", workloadName, "main"))).Should(Equal(float64(500)))
				Expect(testutil.ToFloat64(metrics.TurboRecommendedLimitMemoryGauge.WithLabelValues(namespaceName, "Deployment", workloadName, "main"))).Should(Equal(float64(4 * 1024 * 1024 * 1024)))
				Expect(testutil.ToFloat64(metrics.TurboRecommendedRequestMemoryGauge.WithLabelValues(namespaceName, "Deployment", workloadName, "main"))).Should(Equal(float64(2 * 1024 * 1024 * 1024)))
			})

			It("should NOT let the Workload owner manage resources from The Source of Truth which were set by Turbonomic", func() {
				By("Taking Workload from The Source of Truth")
				workload := createDeployment(workloadName, namespaceName)

				By("Simulating changes made by the Workload owner in The Source of Truth")
				resources := workload.Spec.Template.Spec.Containers[0].Resources
				resources.Requests[corev1.ResourceCPU] = resource.MustParse("4")
				resources.Requests[corev1.ResourceMemory] = resource.MustParse("10Gi")
				resources.Limits[corev1.ResourceCPU] = resource.MustParse("8")
				resources.Limits[corev1.ResourceMemory] = resource.MustParse("20Gi")
				workload.ObjectMeta.Annotations = map[string]string{"foo": "bar"}
				Expect(k8sClient.Update(ctx, workload)).Should(Succeed())

				By("Ensuring changes made by owner and NOT managed by Turbonomic did NOT get lost")
				Eventually(func() string {
					Expect(k8sClient.Get(ctx, types.NamespacedName{Name: workloadName, Namespace: namespaceName}, workload)).Should(Succeed())

					return workload.ObjectMeta.Annotations["foo"]
				}).Should(Equal("bar"))

				By("Ensuring that resources defined earlier by Turbonomic did not change (owner changes dropped)")
				Expect(workload.ObjectMeta.Annotations[managedAnnotation]).Should(Equal("true"))
				resources = workload.Spec.Template.Spec.Containers[0].Resources
				Expect(resources.Requests[corev1.ResourceCPU]).Should(Equal(resource.MustParse("500m")))
				Expect(resources.Requests[corev1.ResourceMemory]).Should(Equal(resource.MustParse("2Gi")))
				Expect(resources.Limits[corev1.ResourceCPU]).Should(Equal(resource.MustParse("1")))
				Expect(resources.Limits[corev1.ResourceMemory]).Should(Equal(resource.MustParse("4Gi")))

				By("Ensuring that TurboOverridesTotal is incremented")
				Expect(testutil.ToFloat64(metrics.TurboOverridesTotal.WithLabelValues(namespaceName, "Deployment", workloadName))).Should(Equal(float64(2)))

				By("Ensuring that TurboRecommendedTotal is NOT incremented")
				Expect(testutil.ToFloat64(metrics.TurboRecommendedTotal.WithLabelValues(namespaceName, "Deployment", workloadName))).Should(Equal(float64(1)))

				By("Ensuring that metrics are set accordingly to Turbonomic recommendation (i.e. no change)")
				Expect(testutil.ToFloat64(metrics.TurboRecommendedLimitCpuGauge.WithLabelValues(namespaceName, "Deployment", workloadName, "main"))).Should(Equal(float64(1000)))
				Expect(testutil.ToFloat64(metrics.TurboRecommendedRequestCpuGauge.WithLabelValues(namespaceName, "Deployment", workloadName, "main"))).Should(Equal(float64(500)))
				Expect(testutil.ToFloat64(metrics.TurboRecommendedLimitMemoryGauge.WithLabelValues(namespaceName, "Deployment", workloadName, "main"))).Should(Equal(float64(4 * 1024 * 1024 * 1024)))
				Expect(testutil.ToFloat64(metrics.TurboRecommendedRequestMemoryGauge.WithLabelValues(namespaceName, "Deployment", workloadName, "main"))).Should(Equal(float64(2 * 1024 * 1024 * 1024)))
			})

			It("should let Turbonomic make further updates to resources", func() {
				By("Reading persisted Workload")
				workload := &appsv1.Deployment{}
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: workloadName, Namespace: namespaceName}, workload)).Should(Succeed())

				By("Simulating changes made by Turbonomic")
				resources := workload.Spec.Template.Spec.Containers[0].Resources
				resources.Requests[corev1.ResourceCPU] = resource.MustParse("400m")
				resources.Requests[corev1.ResourceMemory] = resource.MustParse("1536Mi")
				resources.Limits[corev1.ResourceCPU] = resource.MustParse("600m")
				resources.Limits[corev1.ResourceMemory] = resource.MustParse("2Gi")
				Expect(k8sClientTurbo.Update(ctx, workload)).Should(Succeed())

				By("Ensuring changes made by Turbonomic got applied")
				Eventually(func() resource.Quantity {
					Expect(k8sClient.Get(ctx, types.NamespacedName{Name: workloadName, Namespace: namespaceName}, workload)).Should(Succeed())

					return workload.Spec.Template.Spec.Containers[0].Resources.Requests[corev1.ResourceCPU]
				}).Should(Equal(resource.MustParse("400m")))
				Expect(workload.ObjectMeta.Annotations[managedAnnotation]).Should(Equal("true"))
				Expect(resources.Requests[corev1.ResourceMemory]).Should(Equal(resource.MustParse("1536Mi")))
				Expect(resources.Limits[corev1.ResourceCPU]).Should(Equal(resource.MustParse("600m")))
				Expect(resources.Limits[corev1.ResourceMemory]).Should(Equal(resource.MustParse("2Gi")))

				By("Ensuring that TurboOverridesTotal is NOT incremented")
				Expect(testutil.ToFloat64(metrics.TurboOverridesTotal.WithLabelValues(namespaceName, "Deployment", workloadName))).Should(Equal(float64(2)))

				By("Ensuring that TurboRecommendedTotal is incremented")
				Expect(testutil.ToFloat64(metrics.TurboRecommendedTotal.WithLabelValues(namespaceName, "Deployment", workloadName))).Should(Equal(float64(2)))

				By("Ensuring that metrics are set accordingly to new Turbonomic recommendation")
				Expect(testutil.ToFloat64(metrics.TurboRecommendedLimitCpuGauge.WithLabelValues(namespaceName, "Deployment", workloadName, "main"))).Should(Equal(float64(600)))
				Expect(testutil.ToFloat64(metrics.TurboRecommendedRequestCpuGauge.WithLabelValues(namespaceName, "Deployment", workloadName, "main"))).Should(Equal(float64(400)))
				Expect(testutil.ToFloat64(metrics.TurboRecommendedLimitMemoryGauge.WithLabelValues(namespaceName, "Deployment", workloadName, "main"))).Should(Equal(float64(2 * 1024 * 1024 * 1024)))
				Expect(testutil.ToFloat64(metrics.TurboRecommendedRequestMemoryGauge.WithLabelValues(namespaceName, "Deployment", workloadName, "main"))).Should(Equal(float64(1536 * 1024 * 1024)))
			})

			It("should NOT override resources when owner changed turbo.ibm.com/override annotation to false", func() {
				By("Taking Workload from The Source of Truth")
				workload := createDeployment(workloadName, namespaceName)

				By("Simulating changes made by the Workload owner in The Source of Truth")
				resources := workload.Spec.Template.Spec.Containers[0].Resources
				resources.Requests[corev1.ResourceCPU] = resource.MustParse("4")
				resources.Requests[corev1.ResourceMemory] = resource.MustParse("10Gi")
				resources.Limits[corev1.ResourceCPU] = resource.MustParse("8")
				resources.Limits[corev1.ResourceMemory] = resource.MustParse("20Gi")
				workload.ObjectMeta.Annotations = map[string]string{managedAnnotation: "false"}
				Expect(k8sClient.Update(ctx, workload)).Should(Succeed())

				log.Info("Workload", "annotations", workload.GetAnnotations(), "labels", workload.GetLabels())

				By("Ensuring that resources defined by owner are effective")
				Eventually(func() string {
					Expect(k8sClient.Get(ctx, types.NamespacedName{Name: workloadName, Namespace: namespaceName}, workload)).Should(Succeed())

					return workload.ObjectMeta.Annotations[managedAnnotation]
				}).Should(Equal("false"))

				Expect(workload.ObjectMeta.Annotations[managedAnnotation]).Should(Equal("false"))
				Expect(resources.Requests[corev1.ResourceCPU]).Should(Equal(resource.MustParse("4")))
				Expect(resources.Requests[corev1.ResourceMemory]).Should(Equal(resource.MustParse("10Gi")))
				Expect(resources.Limits[corev1.ResourceCPU]).Should(Equal(resource.MustParse("8")))
				Expect(resources.Limits[corev1.ResourceMemory]).Should(Equal(resource.MustParse("20Gi")))

				By("Ensuring that TurboOverridesTotal is NOT incremented")
				Expect(testutil.ToFloat64(metrics.TurboOverridesTotal.WithLabelValues(namespaceName, "Deployment", workloadName))).Should(Equal(float64(2)))
			})

		})

	})

})

func createDeployment(name string, namespace string) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:   namespace,
			Name:        name,
			Labels:      map[string]string{},
			Annotations: map[string]string{},
		},
		Spec: appsv1.DeploymentSpec{
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
							Image: "docker.io/some/image:latest",
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
}
