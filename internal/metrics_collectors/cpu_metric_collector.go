package metrics_collectors

import (
	"context"

	"github.com/benmeehan/iot-agent/internal/models"
	"github.com/rs/zerolog"
	"github.com/shirou/gopsutil/cpu"
)

// CPUMetricCollector collects CPU usage metrics.
type CPUMetricCollector struct {
	Logger zerolog.Logger
}

func (c *CPUMetricCollector) Name() string {
	return "cpu"
}

func (c *CPUMetricCollector) Collect(ctx context.Context) interface{} {
	cpuPercentages, err := cpu.Percent(0, false)
	if err != nil {
		c.Logger.Error().Err(err).Msg("Failed to get CPU usage")
		return nil
	}

	if len(cpuPercentages) == 0 {
		c.Logger.Warn().Msg("CPU usage data is empty")
		return nil
	}

	c.Logger.Debug().Float64("cpu_usage", cpuPercentages[0]).Msg("CPU usage collected successfully")
	return &cpuPercentages[0]
}

func (c *CPUMetricCollector) IsEnabled(config *models.MetricsConfig) bool {
	if !config.MonitorCPU {
		c.Logger.Warn().Msg("CPU monitoring is disabled in configuration")
	}
	return config.MonitorCPU
}

func (c *CPUMetricCollector) Unit() string {
	return "percentage"
}

func (c *CPUMetricCollector) Description() string {
	return "Percentage of CPU utilization across all cores."
}
