package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	ctrmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

// Define custom metrics as package-level variables
var (
	TurboOverridesTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "turbonomic_companion_operator_turbo_override_total",
		Help: "Total number of compute resource overrides performed by the webhook",
	},
		[]string{"namespace", "kind", "name"})
)

// RegisterMetrics registers custom metrics with the global Prometheus registry
func RegisterMetrics() {
	ctrmetrics.Registry.MustRegister(TurboOverridesTotal)
}
