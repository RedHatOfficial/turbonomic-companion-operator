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
	"time"

	"github.com/RedHatOfficial/turbonomic-companion-operator/internal/metrics"
	. "github.com/onsi/gomega"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// MetricsTestHelper provides utilities for testing metrics
type MetricsTestHelper struct {
	namespace string
	kind      string
	name      string
	container string
}

// NewMetricsTestHelper creates a new metrics test helper
func NewMetricsTestHelper(namespace, kind, name, container string) *MetricsTestHelper {
	return &MetricsTestHelper{
		namespace: namespace,
		kind:      kind,
		name:      name,
		container: container,
	}
}

// AssertTurboOverridesTotal asserts the TurboOverridesTotal metric value
func (mth *MetricsTestHelper) AssertTurboOverridesTotal(expectedValue float64) {
	Eventually(func() float64 {
		return testutil.ToFloat64(metrics.TurboOverridesTotal.WithLabelValues(mth.namespace, mth.kind, mth.name))
	}, defaultTimeout, defaultInterval).Should(Equal(expectedValue))
}

// AssertTurboRecommendedTotal asserts the TurboRecommendedTotal metric value
func (mth *MetricsTestHelper) AssertTurboRecommendedTotal(expectedValue float64) {
	Eventually(func() float64 {
		return testutil.ToFloat64(metrics.TurboRecommendedTotal.WithLabelValues(mth.namespace, mth.kind, mth.name))
	}, defaultTimeout, defaultInterval).Should(Equal(expectedValue))
}

// AssertTurboRecommendedLimitCpuGauge asserts the TurboRecommendedLimitCpuGauge metric value
func (mth *MetricsTestHelper) AssertTurboRecommendedLimitCpuGauge(expectedValue float64) {
	Eventually(func() float64 {
		return testutil.ToFloat64(metrics.TurboRecommendedLimitCpuGauge.WithLabelValues(mth.namespace, mth.kind, mth.name, mth.container))
	}, defaultTimeout, defaultInterval).Should(Equal(expectedValue))
}

// AssertTurboRecommendedRequestCpuGauge asserts the TurboRecommendedRequestCpuGauge metric value
func (mth *MetricsTestHelper) AssertTurboRecommendedRequestCpuGauge(expectedValue float64) {
	Eventually(func() float64 {
		return testutil.ToFloat64(metrics.TurboRecommendedRequestCpuGauge.WithLabelValues(mth.namespace, mth.kind, mth.name, mth.container))
	}, defaultTimeout, defaultInterval).Should(Equal(expectedValue))
}

// AssertTurboRecommendedLimitMemoryGauge asserts the TurboRecommendedLimitMemoryGauge metric value
func (mth *MetricsTestHelper) AssertTurboRecommendedLimitMemoryGauge(expectedValue float64) {
	Eventually(func() float64 {
		return testutil.ToFloat64(metrics.TurboRecommendedLimitMemoryGauge.WithLabelValues(mth.namespace, mth.kind, mth.name, mth.container))
	}, defaultTimeout, defaultInterval).Should(Equal(expectedValue))
}

// AssertTurboRecommendedRequestMemoryGauge asserts the TurboRecommendedRequestMemoryGauge metric value
func (mth *MetricsTestHelper) AssertTurboRecommendedRequestMemoryGauge(expectedValue float64) {
	Eventually(func() float64 {
		return testutil.ToFloat64(metrics.TurboRecommendedRequestMemoryGauge.WithLabelValues(mth.namespace, mth.kind, mth.name, mth.container))
	}, defaultTimeout, defaultInterval).Should(Equal(expectedValue))
}

// AssertAllMetrics asserts all metrics for a given workload
func (mth *MetricsTestHelper) AssertAllMetrics(
	overridesTotal, recommendedTotal float64,
	cpuRequest, cpuLimit, memoryRequest, memoryLimit float64,
) {
	mth.AssertTurboOverridesTotal(overridesTotal)
	mth.AssertTurboRecommendedTotal(recommendedTotal)
	mth.AssertTurboRecommendedRequestCpuGauge(cpuRequest)
	mth.AssertTurboRecommendedLimitCpuGauge(cpuLimit)
	mth.AssertTurboRecommendedRequestMemoryGauge(memoryRequest)
	mth.AssertTurboRecommendedLimitMemoryGauge(memoryLimit)
}

// WaitForMetricsChange waits for metrics to change to expected values
func (mth *MetricsTestHelper) WaitForMetricsChange(
	expectedOverridesTotal, expectedRecommendedTotal float64,
	expectedCpuRequest, expectedCpuLimit, expectedMemoryRequest, expectedMemoryLimit float64,
	timeout time.Duration,
) {
	Eventually(func() bool {
		overridesTotal := testutil.ToFloat64(metrics.TurboOverridesTotal.WithLabelValues(mth.namespace, mth.kind, mth.name))
		recommendedTotal := testutil.ToFloat64(metrics.TurboRecommendedTotal.WithLabelValues(mth.namespace, mth.kind, mth.name))
		cpuRequest := testutil.ToFloat64(metrics.TurboRecommendedRequestCpuGauge.WithLabelValues(mth.namespace, mth.kind, mth.name, mth.container))
		cpuLimit := testutil.ToFloat64(metrics.TurboRecommendedLimitCpuGauge.WithLabelValues(mth.namespace, mth.kind, mth.name, mth.container))
		memoryRequest := testutil.ToFloat64(metrics.TurboRecommendedRequestMemoryGauge.WithLabelValues(mth.namespace, mth.kind, mth.name, mth.container))
		memoryLimit := testutil.ToFloat64(metrics.TurboRecommendedLimitMemoryGauge.WithLabelValues(mth.namespace, mth.kind, mth.name, mth.container))

		return overridesTotal == expectedOverridesTotal &&
			recommendedTotal == expectedRecommendedTotal &&
			cpuRequest == expectedCpuRequest &&
			cpuLimit == expectedCpuLimit &&
			memoryRequest == expectedMemoryRequest &&
			memoryLimit == expectedMemoryLimit
	}, timeout, defaultInterval).Should(BeTrue())
}

// ResetMetrics resets all metrics for testing
func (mth *MetricsTestHelper) ResetMetrics() {
	// Note: Prometheus metrics don't have a built-in reset method
	// This is a placeholder for future implementation if needed
}

// GetCurrentMetrics returns the current metric values
func (mth *MetricsTestHelper) GetCurrentMetrics() map[string]float64 {
	return map[string]float64{
		"overridesTotal":   testutil.ToFloat64(metrics.TurboOverridesTotal.WithLabelValues(mth.namespace, mth.kind, mth.name)),
		"recommendedTotal": testutil.ToFloat64(metrics.TurboRecommendedTotal.WithLabelValues(mth.namespace, mth.kind, mth.name)),
		"cpuRequest":       testutil.ToFloat64(metrics.TurboRecommendedRequestCpuGauge.WithLabelValues(mth.namespace, mth.kind, mth.name, mth.container)),
		"cpuLimit":         testutil.ToFloat64(metrics.TurboRecommendedLimitCpuGauge.WithLabelValues(mth.namespace, mth.kind, mth.name, mth.container)),
		"memoryRequest":    testutil.ToFloat64(metrics.TurboRecommendedRequestMemoryGauge.WithLabelValues(mth.namespace, mth.kind, mth.name, mth.container)),
		"memoryLimit":      testutil.ToFloat64(metrics.TurboRecommendedLimitMemoryGauge.WithLabelValues(mth.namespace, mth.kind, mth.name, mth.container)),
	}
}

// MetricsSnapshot represents a snapshot of metrics at a point in time
type MetricsSnapshot struct {
	OverridesTotal   float64
	RecommendedTotal float64
	CPURequest       float64
	CPULimit         float64
	MemoryRequest    float64
	MemoryLimit      float64
	Timestamp        time.Time
}

// TakeSnapshot takes a snapshot of current metrics
func (mth *MetricsTestHelper) TakeSnapshot() *MetricsSnapshot {
	metrics := mth.GetCurrentMetrics()
	return &MetricsSnapshot{
		OverridesTotal:   metrics["overridesTotal"],
		RecommendedTotal: metrics["recommendedTotal"],
		CPURequest:       metrics["cpuRequest"],
		CPULimit:         metrics["cpuLimit"],
		MemoryRequest:    metrics["memoryRequest"],
		MemoryLimit:      metrics["memoryLimit"],
		Timestamp:        time.Now(),
	}
}

// AssertSnapshotChange asserts that metrics have changed since the snapshot
func (mth *MetricsTestHelper) AssertSnapshotChange(snapshot *MetricsSnapshot, timeout time.Duration) {
	Eventually(func() bool {
		current := mth.GetCurrentMetrics()
		return current["overridesTotal"] != snapshot.OverridesTotal ||
			current["recommendedTotal"] != snapshot.RecommendedTotal ||
			current["cpuRequest"] != snapshot.CPURequest ||
			current["cpuLimit"] != snapshot.CPULimit ||
			current["memoryRequest"] != snapshot.MemoryRequest ||
			current["memoryLimit"] != snapshot.MemoryLimit
	}, timeout, defaultInterval).Should(BeTrue())
}
