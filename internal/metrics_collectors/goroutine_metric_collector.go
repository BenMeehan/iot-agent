package metrics_collectors

import (
	"context"
	"runtime"

	"github.com/benmeehan/iot-agent/internal/models"
	"github.com/rs/zerolog"
)

// GoroutineMetricCollector collects the number of active goroutines.
type GoroutineMetricCollector struct {
	Logger zerolog.Logger
}

// Name returns the identifier for the goroutine metric collector.
func (g *GoroutineMetricCollector) Name() string {
	return "goroutines"
}

// Collect retrieves the number of active goroutines.
func (g *GoroutineMetricCollector) Collect(ctx context.Context) interface{} {
	n := float64(runtime.NumGoroutine())
	g.Logger.Debug().Float64("goroutines", n).Msg("Goroutine count collected")
	return &n
}

// IsEnabled checks if goroutine monitoring is enabled in the configuration.
func (g *GoroutineMetricCollector) IsEnabled(config *models.MetricsConfig) bool {
	if !config.MonitorGoroutines {
		g.Logger.Debug().Msg("Disk monitoring is disabled in configuration")
	}
	return config.MonitorGoroutines
}

// Unit specifies the unit for the goroutine count metric.
func (g *GoroutineMetricCollector) Unit() string {
	return "count"
}

// Description provides a summary of the goroutine metric collected.
func (g *GoroutineMetricCollector) Description() string {
	return "Number of active goroutines in the runtime."
}
