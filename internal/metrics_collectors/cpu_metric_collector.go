package metrics_collectors

import (
	"context"

	"github.com/benmeehan/iot-agent/internal/models"
	"github.com/rs/zerolog"
	"github.com/shirou/gopsutil/cpu"
)

// CPUMetricCollector collects overall CPU usage metrics.
type CPUMetricCollector struct {
	Logger zerolog.Logger
}

// Name returns the identifier for the CPU metric collector.
func (c *CPUMetricCollector) Name() string {
	return "cpu"
}

// Collect retrieves the current CPU utilization percentage.
func (c *CPUMetricCollector) Collect(ctx context.Context) interface{} {
	cpuUsage, err := cpu.Percent(0, false)
	if err != nil {
		c.Logger.Error().Err(err).Msg("Failed to retrieve CPU usage")
		return nil
	}

	if len(cpuUsage) == 0 {
		c.Logger.Warn().Msg("No CPU usage data retrieved")
		return nil
	}

	c.Logger.Debug().Float64("cpu_usage", cpuUsage[0]).Msg("CPU usage collected successfully")
	return &cpuUsage[0]
}

// IsEnabled determines if CPU monitoring is enabled in the configuration.
func (c *CPUMetricCollector) IsEnabled(config *models.MetricsConfig) bool {
	if !config.MonitorCPU {
		c.Logger.Debug().Msg("CPU monitoring is disabled in configuration")
	}
	return config.MonitorCPU
}

// Unit specifies the unit for the CPU usage metric.
func (c *CPUMetricCollector) Unit() string {
	return "percentage"
}

// Description provides a summary of the CPU metric collected.
func (c *CPUMetricCollector) Description() string {
	return "Percentage of CPU utilization across all cores."
}
