package metrics_collectors

import (
	"context"
	"strings"
	"sync"

	"github.com/benmeehan/iot-agent/internal/models"
	"github.com/rs/zerolog"
	"github.com/shirou/gopsutil/process"
)

// ProcessMetricCollector collects CPU, memory, and I/O metrics for specified processes.
type ProcessMetricCollector struct {
	Logger         zerolog.Logger
	ProcessNames   map[string]struct{}
	MonitorProcCPU bool
	MonitorProcMem bool
	MonitorProcIO  bool
}

func (p *ProcessMetricCollector) Name() string {
	return "process"
}

func (p *ProcessMetricCollector) Collect(ctx context.Context) interface{} {
	procs, err := process.Processes()
	if err != nil {
		p.Logger.Error().Err(err).Msg("Failed to retrieve process list")
		return nil
	}

	if !p.MonitorProcCPU {
		p.Logger.Warn().Msg("CPU monitoring for processes is disabled in configuration")
	}
	if !p.MonitorProcMem {
		p.Logger.Warn().Msg("Memory monitoring for processes is disabled in configuration")
	}
	if !p.MonitorProcIO {
		p.Logger.Warn().Msg("IOps monitoring for processes is disabled in configuration")
	}

	processMetrics := make(map[string]*models.ProcessMetrics)
	var wg sync.WaitGroup
	var metricsMutex sync.Mutex

	// Iterate over all processes and collect metrics concurrently
	for _, proc := range procs {
		// Filter out processes not in the monitored list before spawning goroutines
		name, err := proc.Name()
		if err != nil {
			p.Logger.Warn().Err(err).Int32("pid", proc.Pid).Msg("Failed to get process name")
			continue
		}
		if _, exists := p.ProcessNames[strings.ToLower(name)]; !exists {
			continue
		}

		wg.Add(1)
		go func(proc *process.Process, name string) {
			defer wg.Done()

			procMetrics := &models.ProcessMetrics{}

			// CPU usage collection
			if p.MonitorProcCPU {
				if cpuPercent, err := proc.CPUPercent(); err == nil {
					procMetrics.CPUUsage = cpuPercent
				} else {
					p.Logger.Warn().Err(err).Str("process", name).Int32("pid", proc.Pid).Msg("Failed to get CPU usage")
				}
			}

			// Memory usage collection
			if p.MonitorProcMem {
				if memInfo, err := proc.MemoryInfo(); err == nil {
					rss := float64(memInfo.RSS)
					procMetrics.Memory = rss
				} else {
					p.Logger.Warn().Err(err).Str("process", name).Int32("pid", proc.Pid).Msg("Failed to get memory information")
				}
			}

			// I/O operations collection
			if p.MonitorProcIO {
				if ioCounters, err := proc.IOCounters(); err == nil {
					readOps := float64(ioCounters.ReadCount)
					writeOps := float64(ioCounters.WriteCount)
					procMetrics.ReadOps = readOps
					procMetrics.WriteOps = writeOps
				} else {
					p.Logger.Warn().Err(err).Str("process", name).Int32("pid", proc.Pid).Msg("Failed to get I/O counters")
				}
			}

			// Safely add the collected metrics to the shared map
			metricsMutex.Lock()
			processMetrics[name] = procMetrics
			metricsMutex.Unlock()
		}(proc, name)
	}

	wg.Wait()
	p.Logger.Debug().Msg("Process metrics collection completed successfully")
	return processMetrics
}

func (p *ProcessMetricCollector) IsEnabled(config *models.MetricsConfig) bool {
	if len(config.ProcessNames) == 0 {
		p.Logger.Warn().Msg("Process monitoring disabled: no processes specified in configuration")
	}
	if !(config.MonitorProcCPU || config.MonitorProcMem || config.MonitorProcIOps) {
		p.Logger.Warn().Msg("Process monitoring disabled: no process metrics (CPU, Memory, I/O) selected")
	}
	return len(config.ProcessNames) > 0 &&
		(config.MonitorProcCPU || config.MonitorProcMem || config.MonitorProcIOps)
}

func (p *ProcessMetricCollector) Unit() string {
	return "varied (CPU: %, Memory: bytes, I/O: operations)"
}

func (p *ProcessMetricCollector) Description() string {
	return "Collects CPU usage (%), memory consumption (bytes), and I/O operations (read/write counts) for specified processes."
}
