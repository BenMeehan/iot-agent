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
	return "CPU"
}

func (c *CPUMetricCollector) Collect(ctx context.Context) interface{} {
	cpuPercentages, err := cpu.Percent(0, false)
	if err != nil {
		c.Logger.Error().Err(err).Msg("Failed to get CPU usage")
		return nil
	}
	if len(cpuPercentages) > 0 {
		return &cpuPercentages[0]
	}
	return nil
}

func (c *CPUMetricCollector) IsEnabled(config *models.MetricsConfig) bool {
	return config.MonitorCPU
}

func (c *CPUMetricCollector) Unit() string {
	return "percentage"
}

func (c *CPUMetricCollector) Description() string {
	return "Percentage of CPU utilization across all cores."
}
