package metrics_collectors

import (
	"context"

	"github.com/benmeehan/iot-agent/internal/models"
)

// MetricCollector defines the interface for collecting a specific metric.
type MetricCollector interface {
	Name() string                                // Name of the metric (e.g., "CPU", "Memory")
	Collect(ctx context.Context) interface{}     // Collect the metric data
	IsEnabled(config *models.MetricsConfig) bool // Check if the metric is enabled in the config
	Unit() string                                // Unit of the metric (e.g., "percentage", "bytes")
	Description() string                         // Description of the metric
}
