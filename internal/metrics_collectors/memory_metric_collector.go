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
	return "Memory"
}

func (m *MemoryMetricCollector) Collect(ctx context.Context) interface{} {
	memStats, err := mem.VirtualMemory()
	if err != nil {
		m.Logger.Error().Err(err).Msg("Failed to get memory stats")
		return nil
	}
	return &memStats.UsedPercent
}

func (m *MemoryMetricCollector) IsEnabled(config *models.MetricsConfig) bool {
	return config.MonitorMemory
}

func (m *MemoryMetricCollector) Unit() string {
	return "percentage"
}

func (m *MemoryMetricCollector) Description() string {
	return "Percentage of used virtual memory."
}
