package metrics_collectors

import (
	"context"
	"sync"

	"github.com/benmeehan/iot-agent/internal/models"
	"github.com/benmeehan/iot-agent/internal/utils"
	"github.com/rs/zerolog"
	"github.com/shirou/gopsutil/process"
)

// ProcessMetricCollector collects CPU, memory, and I/O metrics for specified processes.
type ProcessMetricCollector struct {
	Logger             zerolog.Logger
	TargetProcessNames map[string]struct{}
	MonitorProcCPU     bool
	MonitorProcMem     bool
	MonitorProcIO      bool
	WorkerPool         *utils.WorkerPool
}

// Name returns the collector's identifier.
func (p *ProcessMetricCollector) Name() string {
	return "process"
}

// Collect gathers process metrics concurrently using a worker pool.
func (p *ProcessMetricCollector) Collect(ctx context.Context) interface{} {
	p.logDisabledMetrics()

	// Retrieve all running processes
	allProcs, err := process.Processes()
	if err != nil {
		p.Logger.Error().Err(err).Msg("Failed to list processes")
		return nil
	}

	processMetrics := make([]*models.ProcessMetrics, 0)
	var wg sync.WaitGroup
	var metricsMutex sync.Mutex

	// Collect metrics for each process concurrently.
	for _, proc := range allProcs {
		name, err := proc.Name()
		if err != nil {
			p.Logger.Debug().Err(err).Int32("pid", proc.Pid).Msg("Skipping process due to name error")
			continue
		}

		// Check if the process is a target
		if _, ok := p.TargetProcessNames[name]; ok {
			wg.Add(1)
			p.WorkerPool.Submit(func(proc *process.Process) func() {
				return func() {
					defer wg.Done()
					metrics := p.collectProcessMetrics(proc, name)
					if metrics != nil {
						metricsMutex.Lock()
						processMetrics = append(processMetrics, metrics)
						metricsMutex.Unlock()
					}
				}
			}(proc))
		}
	}

	wg.Wait()

	// Log a warning if no metrics were collected.
	if len(processMetrics) == 0 {
		p.Logger.Debug().Msg("No process metrics collected. Ensure specified processes are running and accessible.")
	} else {
		p.Logger.Debug().Msg("Process metrics collection completed successfully")
	}

	return processMetrics
}

// collectProcessMetrics gathers enabled metrics (CPU, memory, I/O) for a single process.
func (p *ProcessMetricCollector) collectProcessMetrics(proc *process.Process, name string) *models.ProcessMetrics {
	metrics := &models.ProcessMetrics{}

	metrics.ProcessName = name
	metrics.ProcessID = proc.Pid

	// Collect CPU usage if enabled.
	if p.MonitorProcCPU {
		if cpu, err := proc.CPUPercent(); err == nil {
			metrics.CPUUsage = cpu
		} else {
			p.logMetricError("CPU usage", err, name, proc.Pid)
		}
	}

	// Collect memory usage if enabled.
	if p.MonitorProcMem {
		if memInfo, err := proc.MemoryInfo(); err == nil {
			metrics.Memory = float64(memInfo.RSS)
		} else {
			p.logMetricError("memory info", err, name, proc.Pid)
		}
	}

	// Collect I/O operations if enabled.
	if p.MonitorProcIO {
		if ioCounters, err := proc.IOCounters(); err == nil {
			metrics.ReadOps = float64(ioCounters.ReadCount)
			metrics.WriteOps = float64(ioCounters.WriteCount)
		} else {
			p.logMetricError("I/O counters", err, name, proc.Pid)
		}
	}

	return metrics
}

// logDisabledMetrics logs which metrics are disabled, if any.
func (p *ProcessMetricCollector) logDisabledMetrics() {
	if !p.MonitorProcCPU {
		p.Logger.Debug().Msg("CPU monitoring for processes is disabled in configuration")
	}
	if !p.MonitorProcMem {
		p.Logger.Debug().Msg("Memory monitoring for processes is disabled in configuration")
	}
	if !p.MonitorProcIO {
		p.Logger.Debug().Msg("IOps monitoring for processes is disabled in configuration")
	}
}

// logMetricError logs errors encountered during metric collection with process details.
func (p *ProcessMetricCollector) logMetricError(metric string, err error, name string, pid int32) {
	p.Logger.Warn().
		Err(err).
		Str("process", name).
		Int32("pid", pid).
		Msgf("Failed to get %s", metric)
}

// IsEnabled checks if process metrics collection is enabled based on the configuration.
func (p *ProcessMetricCollector) IsEnabled(config *models.MetricsConfig) bool {
	if len(config.ProcessNames) == 0 {
		p.Logger.Warn().Msg("Process monitoring disabled: no processes specified in configuration")
		return false
	}
	if !(config.MonitorProcCPU || config.MonitorProcMem || config.MonitorProcIOps) {
		p.Logger.Warn().Msg("Process monitoring disabled: no process metrics (CPU, Memory, I/O) selected")
		return false
	}
	return true
}

// Unit returns the units used for each type of metric collected.
func (p *ProcessMetricCollector) Unit() string {
	return "varied (CPU: %, Memory: bytes, I/O: operations)"
}

// Description provides a brief summary of the collected metrics.
func (p *ProcessMetricCollector) Description() string {
	return "Collects CPU usage (%), memory consumption (bytes), and I/O operations (read/write counts) for specified processes."
}
