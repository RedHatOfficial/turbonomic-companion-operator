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

		Context("has all compute resources managed by Turbonomic and is otherwise managed by CI/CD", func() {

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

				By("Checking that turbo.ibm.com/override annotation is set based on what was changed")
				Eventually(func() string {
					Expect(k8sClient.Get(ctx, types.NamespacedName{Name: workloadName, Namespace: namespaceName}, workload)).Should(Succeed())

					return workload.ObjectMeta.Annotations[managedAnnotation]
				}).Should(Equal("all")) // Turbonomic changed both CPU and memory in this test

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
				Expect(workload.ObjectMeta.Annotations[managedAnnotation]).Should(Equal("all"))
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
				Expect(workload.ObjectMeta.Annotations[managedAnnotation]).Should(Equal("all"))
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

	Context("has cpu compute resources managed by Turbonomic and is otherwise managed by CI/CD", func() {
		const (
			namespaceName = "selective-management-test"
			workloadName  = "test-workload"
		)

		It("should start with 'cpu' mode when Turbonomic initially updates only CPU, allowing CI/CD to manage memory", func() {
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

			By("Creating a fresh workload")
			deployment := createDeployment(workloadName, namespaceName)
			Expect(k8sClient.Create(ctx, deployment)).Should(Succeed())

			By("Turbonomic makes initial update to CPU only")
			workload := &appsv1.Deployment{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: workloadName, Namespace: namespaceName}, workload)
			}).Should(Succeed())
			workload.Spec.Template.Spec.Containers[0].Resources.Requests[corev1.ResourceCPU] = resource.MustParse("750m") // Was 1
			workload.Spec.Template.Spec.Containers[0].Resources.Limits[corev1.ResourceCPU] = resource.MustParse("1500m")  // Was 2
			Expect(k8sClientTurbo.Update(ctx, workload)).Should(Succeed())

			By("Verifying override annotation is set to 'cpu'")
			Eventually(func() string {
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: workloadName, Namespace: namespaceName}, workload)).Should(Succeed())
				return workload.ObjectMeta.Annotations[managedAnnotation]
			}).Should(Equal("cpu"))

			By("CI/CD agent can update memory but not CPU")
			workload.Spec.Template.Spec.Containers[0].Resources.Requests[corev1.ResourceCPU] = resource.MustParse("2")      // Should be overridden back
			workload.Spec.Template.Spec.Containers[0].Resources.Requests[corev1.ResourceMemory] = resource.MustParse("4Gi") // Should be preserved
			workload.Spec.Template.Spec.Containers[0].Resources.Limits[corev1.ResourceCPU] = resource.MustParse("4")        // Should be overridden back
			workload.Spec.Template.Spec.Containers[0].Resources.Limits[corev1.ResourceMemory] = resource.MustParse("8Gi")   // Should be preserved
			Expect(k8sClient.Update(ctx, workload)).Should(Succeed())

			By("Verifying CPU is overridden back to Turbonomic values but memory changes are preserved")
			Eventually(func() bool {
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: workloadName, Namespace: namespaceName}, workload)).Should(Succeed())
				resources := workload.Spec.Template.Spec.Containers[0].Resources

				// CPU should be overridden back to Turbonomic values
				cpuRequestOK := resources.Requests[corev1.ResourceCPU].Equal(resource.MustParse("750m"))
				cpuLimitOK := resources.Limits[corev1.ResourceCPU].Equal(resource.MustParse("1500m"))

				// Memory should preserve CI/CD changes
				memRequestOK := resources.Requests[corev1.ResourceMemory].Equal(resource.MustParse("4Gi"))
				memLimitOK := resources.Limits[corev1.ResourceMemory].Equal(resource.MustParse("8Gi"))

				return cpuRequestOK && cpuLimitOK && memRequestOK && memLimitOK
			}).Should(BeTrue())
		})

		It("should upgrade to 'all' mode when Turbonomic later updates memory, preventing CI/CD from managing either resource", func() {
			By("Turbonomic now updates memory resources as well")
			workload := &appsv1.Deployment{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: workloadName, Namespace: namespaceName}, workload)
			}).Should(Succeed())
			workload.Spec.Template.Spec.Containers[0].Resources.Requests[corev1.ResourceMemory] = resource.MustParse("512Mi") // Memory change triggers upgrade
			Expect(k8sClientTurbo.Update(ctx, workload)).Should(Succeed())

			By("Verifying override annotation is upgraded to 'all'")
			Eventually(func() string {
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: workloadName, Namespace: namespaceName}, workload)).Should(Succeed())
				return workload.ObjectMeta.Annotations[managedAnnotation]
			}).Should(Equal("all"))

			By("CI/CD agent can no longer update either CPU or memory")
			workload.Spec.Template.Spec.Containers[0].Resources.Requests[corev1.ResourceCPU] = resource.MustParse("2")       // Should be overridden back
			workload.Spec.Template.Spec.Containers[0].Resources.Requests[corev1.ResourceMemory] = resource.MustParse("16Gi") // Should be overridden back
			workload.Spec.Template.Spec.Containers[0].Resources.Limits[corev1.ResourceCPU] = resource.MustParse("4")         // Should be overridden back
			workload.Spec.Template.Spec.Containers[0].Resources.Limits[corev1.ResourceMemory] = resource.MustParse("32Gi")   // Should be overridden back
			Expect(k8sClient.Update(ctx, workload)).Should(Succeed())

			By("Verifying both CPU and memory are overridden back to Turbonomic values")
			Eventually(func() bool {
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: workloadName, Namespace: namespaceName}, workload)).Should(Succeed())
				resources := workload.Spec.Template.Spec.Containers[0].Resources

				// Both CPU and memory should be overridden back to Turbonomic values
				cpuRequestOK := resources.Requests[corev1.ResourceCPU].Equal(resource.MustParse("750m"))
				cpuLimitOK := resources.Limits[corev1.ResourceCPU].Equal(resource.MustParse("1500m"))
				memRequestOK := resources.Requests[corev1.ResourceMemory].Equal(resource.MustParse("512Mi"))
				memLimitOK := resources.Limits[corev1.ResourceMemory].Equal(resource.MustParse("8Gi")) // Previous limit preserved from earlier

				return cpuRequestOK && cpuLimitOK && memRequestOK && memLimitOK
			}).Should(BeTrue())
		})

		It("should stay in 'all' mode when Turbonomic makes subsequent CPU-only updates (no downgrade)", func() {
			By("Turbonomic makes another update to CPU only")
			workload := &appsv1.Deployment{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: workloadName, Namespace: namespaceName}, workload)
			}).Should(Succeed())
			workload.Spec.Template.Spec.Containers[0].Resources.Requests[corev1.ResourceCPU] = resource.MustParse("500m") // CPU-only change
			workload.Spec.Template.Spec.Containers[0].Resources.Limits[corev1.ResourceCPU] = resource.MustParse("1")      // CPU-only change
			Expect(k8sClientTurbo.Update(ctx, workload)).Should(Succeed())

			By("Verifying override annotation remains 'all' (no downgrade)")
			Eventually(func() string {
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: workloadName, Namespace: namespaceName}, workload)).Should(Succeed())
				return workload.ObjectMeta.Annotations[managedAnnotation]
			}).Should(Equal("all"))

			By("Verifying CI/CD still cannot update either CPU or memory")
			workload.Spec.Template.Spec.Containers[0].Resources.Requests[corev1.ResourceCPU] = resource.MustParse("2")       // Should be overridden back
			workload.Spec.Template.Spec.Containers[0].Resources.Requests[corev1.ResourceMemory] = resource.MustParse("16Gi") // Should be overridden back
			Expect(k8sClient.Update(ctx, workload)).Should(Succeed())

			By("Verifying both resources are still managed by Turbonomic")
			Eventually(func() bool {
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: workloadName, Namespace: namespaceName}, workload)).Should(Succeed())
				resources := workload.Spec.Template.Spec.Containers[0].Resources

				// Both CPU and memory should be overridden back to Turbonomic values
				cpuRequestOK := resources.Requests[corev1.ResourceCPU].Equal(resource.MustParse("500m")) // Latest Turbonomic value
				cpuLimitOK := resources.Limits[corev1.ResourceCPU].Equal(resource.MustParse("1"))        // Latest Turbonomic value
				memRequestOK := resources.Requests[corev1.ResourceMemory].Equal(resource.MustParse("512Mi"))
				memLimitOK := resources.Limits[corev1.ResourceMemory].Equal(resource.MustParse("8Gi"))

				return cpuRequestOK && cpuLimitOK && memRequestOK && memLimitOK
			}).Should(BeTrue())
		})
	})

	Context("validates turbo.ibm.com/override annotation values", func() {
		const workloadName = "test-workload"

		It("should accept valid annotation values: false", func() {
			namespaceName := "annotation-validation-false"

			By("Creating a Namespace")
			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: namespaceName,
					Annotations: map[string]string{
						"turbo.ibm.com/override": "enabled", // Required for webhook to trigger
					},
				},
			}
			Expect(k8sClient.Create(ctx, ns)).Should(Succeed())

			By("Creating a fresh workload")
			deployment := createDeployment(workloadName, namespaceName)
			Expect(k8sClient.Create(ctx, deployment)).Should(Succeed())

			By("Waiting for workload to be available")
			workload := &appsv1.Deployment{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: workloadName, Namespace: namespaceName}, workload)
			}).Should(Succeed())

			By("Setting turbo.ibm.com/override to 'false'")
			workload.ObjectMeta.Annotations = map[string]string{managedAnnotation: "false"}
			Expect(k8sClient.Update(ctx, workload)).Should(Succeed())

			By("Verifying the annotation was accepted")
			Eventually(func() string {
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: workloadName, Namespace: namespaceName}, workload)).Should(Succeed())
				return workload.ObjectMeta.Annotations[managedAnnotation]
			}).Should(Equal("false"))
		})

		It("should accept valid annotation values: cpu", func() {
			namespaceName := "annotation-validation-cpu"

			By("Creating a Namespace")
			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: namespaceName,
					Annotations: map[string]string{
						"turbo.ibm.com/override": "enabled", // Required for webhook to trigger
					},
				},
			}
			Expect(k8sClient.Create(ctx, ns)).Should(Succeed())

			By("Creating a fresh workload")
			deployment := createDeployment(workloadName, namespaceName)
			Expect(k8sClient.Create(ctx, deployment)).Should(Succeed())

			By("Waiting for workload to be available")
			workload := &appsv1.Deployment{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: workloadName, Namespace: namespaceName}, workload)
			}).Should(Succeed())

			By("Setting turbo.ibm.com/override to 'cpu'")
			workload.ObjectMeta.Annotations = map[string]string{managedAnnotation: "cpu"}
			Expect(k8sClient.Update(ctx, workload)).Should(Succeed())

			By("Verifying the annotation was accepted")
			Eventually(func() string {
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: workloadName, Namespace: namespaceName}, workload)).Should(Succeed())
				return workload.ObjectMeta.Annotations[managedAnnotation]
			}).Should(Equal("cpu"))
		})

		It("should accept valid annotation values: all", func() {
			namespaceName := "annotation-validation-all"

			By("Creating a Namespace")
			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: namespaceName,
					Annotations: map[string]string{
						"turbo.ibm.com/override": "enabled", // Required for webhook to trigger
					},
				},
			}
			Expect(k8sClient.Create(ctx, ns)).Should(Succeed())

			By("Creating a fresh workload")
			deployment := createDeployment(workloadName, namespaceName)
			Expect(k8sClient.Create(ctx, deployment)).Should(Succeed())

			By("Waiting for workload to be available")
			workload := &appsv1.Deployment{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: workloadName, Namespace: namespaceName}, workload)
			}).Should(Succeed())

			By("Setting turbo.ibm.com/override to 'all'")
			workload.ObjectMeta.Annotations = map[string]string{managedAnnotation: "all"}
			Expect(k8sClient.Update(ctx, workload)).Should(Succeed())

			By("Verifying the annotation was accepted")
			Eventually(func() string {
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: workloadName, Namespace: namespaceName}, workload)).Should(Succeed())
				return workload.ObjectMeta.Annotations[managedAnnotation]
			}).Should(Equal("all"))
		})

		It("should reject invalid annotation values", func() {
			namespaceName := "annotation-validation-invalid"

			By("Creating a Namespace")
			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: namespaceName,
					Annotations: map[string]string{
						"turbo.ibm.com/override": "enabled", // Required for webhook to trigger
					},
				},
			}
			Expect(k8sClient.Create(ctx, ns)).Should(Succeed())

			By("Creating a fresh workload")
			deployment := createDeployment(workloadName, namespaceName)
			Expect(k8sClient.Create(ctx, deployment)).Should(Succeed())

			By("Waiting for workload to be available")
			workload := &appsv1.Deployment{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: workloadName, Namespace: namespaceName}, workload)
			}).Should(Succeed())

			By("Attempting to set turbo.ibm.com/override to invalid value 'invalid'")
			workload.ObjectMeta.Annotations = map[string]string{managedAnnotation: "invalid"}
			err := k8sClient.Update(ctx, workload)
			Expect(err).Should(HaveOccurred())
			Expect(err.Error()).Should(ContainSubstring("Invalid value 'invalid' for annotation 'turbo.ibm.com/override'"))
			Expect(err.Error()).Should(ContainSubstring("Valid values are: false, cpu, all"))
		})

		It("should reject empty string as annotation value", func() {
			namespaceName := "annotation-validation-empty"

			By("Creating a Namespace")
			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: namespaceName,
					Annotations: map[string]string{
						"turbo.ibm.com/override": "enabled", // Required for webhook to trigger
					},
				},
			}
			Expect(k8sClient.Create(ctx, ns)).Should(Succeed())

			By("Creating a fresh workload")
			deployment := createDeployment(workloadName, namespaceName)
			Expect(k8sClient.Create(ctx, deployment)).Should(Succeed())

			By("Waiting for workload to be available")
			workload := &appsv1.Deployment{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: workloadName, Namespace: namespaceName}, workload)
			}).Should(Succeed())

			By("Attempting to set turbo.ibm.com/override to empty string")
			workload.ObjectMeta.Annotations = map[string]string{managedAnnotation: ""}
			err := k8sClient.Update(ctx, workload)
			Expect(err).Should(HaveOccurred())
			Expect(err.Error()).Should(ContainSubstring("Invalid value '' for annotation 'turbo.ibm.com/override'"))
		})

		It("should accept 'true' as annotation value (legacy value, normalized to 'all')", func() {
			namespaceName := "annotation-validation-true"

			By("Creating a Namespace")
			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: namespaceName,
					Annotations: map[string]string{
						"turbo.ibm.com/override": "enabled", // Required for webhook to trigger
					},
				},
			}
			Expect(k8sClient.Create(ctx, ns)).Should(Succeed())

			By("Creating a fresh workload")
			deployment := createDeployment(workloadName, namespaceName)
			Expect(k8sClient.Create(ctx, deployment)).Should(Succeed())

			By("Waiting for workload to be available")
			workload := &appsv1.Deployment{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: workloadName, Namespace: namespaceName}, workload)
			}).Should(Succeed())

			By("Setting turbo.ibm.com/override to 'true' (legacy value)")
			workload.ObjectMeta.Annotations = map[string]string{managedAnnotation: "true"}
			Expect(k8sClient.Update(ctx, workload)).Should(Succeed())

			By("Verifying the annotation was accepted and normalized to 'all'")
			Eventually(func() string {
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: workloadName, Namespace: namespaceName}, workload)).Should(Succeed())
				return workload.ObjectMeta.Annotations[managedAnnotation]
			}).Should(Equal("all")) // The webhook accepts "true" but normalizes it to "all"
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
