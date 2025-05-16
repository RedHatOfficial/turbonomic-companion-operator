package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	ctrmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

// Define custom metrics as package-level variables
var (
	dimensions = []string{"workload_namespace", "workload_kind", "workload_name"}

	dimensions_with_container = append(dimensions, "workload_container")

	TurboOverridesTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "turbonomic_companion_operator_turbo_override_total",
		Help: "Total number of compute resource overrides performed by the webhook",
	}, dimensions)

	TurboRecommendedRequestCpuGauge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "turbonomic_companion_operator_turbo_recommended_request_cpu",
		Help: "Cpu requests recommended by Turbonomic",
	}, dimensions_with_container)

	TurboRecommendedRequestMemoryGauge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "turbonomic_companion_operator_turbo_recommended_request_memory",
		Help: "Memory requests recommended by Turbonomic",
	}, dimensions_with_container)

	TurboRecommendedLimitCpuGauge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "turbonomic_companion_operator_turbo_recommended_limit_cpu",
		Help: "Cpu limits recommended by Turbonomic",
	}, dimensions_with_container)

	TurboRecommendedLimitMemoryGauge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "turbonomic_companion_operator_turbo_recommended_limit_memory",
		Help: "Memory limits recommended by Turbonomic",
	}, dimensions_with_container)
)

// RegisterMetrics registers custom metrics with the global Prometheus registry
func RegisterMetrics() {
	ctrmetrics.Registry.MustRegister(TurboOverridesTotal)
}
