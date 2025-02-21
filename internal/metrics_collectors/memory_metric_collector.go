package metrics_collectors

import (
	"context"

	"github.com/benmeehan/iot-agent/internal/models"
	"github.com/rs/zerolog"
	"github.com/shirou/gopsutil/mem"
)

// MemoryMetricCollector collects the percentage of used virtual memory.
type MemoryMetricCollector struct {
	Logger zerolog.Logger
}

// Name returns the identifier for the memory metric collector.
func (m *MemoryMetricCollector) Name() string {
	return "memory"
}

// Collect retrieves the percentage of used virtual memory.
func (m *MemoryMetricCollector) Collect(ctx context.Context) interface{} {
	m.Logger.Debug().Msg("Collecting memory usage metrics")

	memStats, err := mem.VirtualMemory()
	if err != nil {
		m.Logger.Error().Err(err).Msg("Failed to retrieve memory statistics")
		return nil
	}

	m.Logger.Debug().
		Float64("memory_usage_percent", memStats.UsedPercent).
		Msg("Memory usage collected successfully")

	return &memStats.UsedPercent
}

// IsEnabled checks if memory monitoring is enabled in the configuration.
func (m *MemoryMetricCollector) IsEnabled(config *models.MetricsConfig) bool {
	if !config.MonitorMemory {
		m.Logger.Debug().Msg("Memory monitoring is disabled in configuration")
	}
	return config.MonitorMemory
}

// Unit specifies the unit for memory usage metrics.
func (m *MemoryMetricCollector) Unit() string {
	return "percentage"
}

// Description provides details of the memory usage metrics collected.
func (m *MemoryMetricCollector) Description() string {
	return "Percentage of used virtual memory."
}
