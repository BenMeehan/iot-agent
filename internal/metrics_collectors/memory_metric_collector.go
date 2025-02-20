package metrics_collectors

import (
	"context"

	"github.com/benmeehan/iot-agent/internal/models"
	"github.com/rs/zerolog"
	"github.com/shirou/gopsutil/mem"
)

// MemoryMetricCollector collects memory usage metrics.
type MemoryMetricCollector struct {
	Logger zerolog.Logger
}

func (m *MemoryMetricCollector) Name() string {
	return "memory"
}

func (m *MemoryMetricCollector) Collect(ctx context.Context) interface{} {
	m.Logger.Debug().Msg("Starting memory usage collection")
	memStats, err := mem.VirtualMemory()
	if err != nil {
		m.Logger.Error().Err(err).Msg("Failed to get memory stats")
		return nil
	}
	m.Logger.Debug().Float64("memory_usage", memStats.UsedPercent).Msg("Memory usage collected successfully")
	return &memStats.UsedPercent
}

func (m *MemoryMetricCollector) IsEnabled(config *models.MetricsConfig) bool {
	if !config.MonitorMemory {
		m.Logger.Warn().Msg("Memory monitoring is disabled in configuration")
	}
	return config.MonitorMemory
}

func (m *MemoryMetricCollector) Unit() string {
	return "percentage"
}

func (m *MemoryMetricCollector) Description() string {
	return "Percentage of used virtual memory."
}
