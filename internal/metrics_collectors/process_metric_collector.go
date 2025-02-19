package metrics_collectors

import (
	"context"

	"github.com/benmeehan/iot-agent/internal/models"
	"github.com/rs/zerolog"
	"github.com/shirou/gopsutil/process"
)

// ProcessMetricCollector collects process-specific metrics.
type ProcessMetricCollector struct {
	Logger zerolog.Logger
}

func (p *ProcessMetricCollector) Name() string {
	return "Process"
}

func (p *ProcessMetricCollector) Collect(ctx context.Context) interface{} {
	procs, err := process.Processes()
	if err != nil {
		p.Logger.Error().Err(err).Msg("Failed to get process metrics")
		return nil
	}

	processMetrics := make(map[string]*models.ProcessMetrics)
	for _, proc := range procs {
		name, err := proc.Name()
		if err != nil {
			continue
		}
		procMetrics := &models.ProcessMetrics{}
		cpuPercent, err := proc.CPUPercent()
		if err == nil {
			procMetrics.CPUUsage = &cpuPercent
		}
		memInfo, err := proc.MemoryInfo()
		if err == nil {
			rss := float64(memInfo.RSS)
			procMetrics.Memory = &rss
		}
		ioCounters, err := proc.IOCounters()
		if err == nil {
			readOps := float64(ioCounters.ReadCount)
			writeOps := float64(ioCounters.WriteCount)
			procMetrics.ReadOps = &readOps
			procMetrics.WriteOps = &writeOps
		}
		processMetrics[name] = procMetrics
	}
	return processMetrics
}

func (p *ProcessMetricCollector) IsEnabled(config *models.MetricsConfig) bool {
	return len(config.ProcessNames) > 0 && (config.MonitorProcCPU || config.MonitorProcMem)
}

func (p *ProcessMetricCollector) Unit() string {
	return "varied (CPU: percentage, Memory: bytes, I/O: operations)"
}

func (p *ProcessMetricCollector) Description() string {
	return "Collects CPU usage (%), memory consumption (bytes), and I/O operations (read/write counts) for individual processes."
}
