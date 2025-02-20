package metrics_collectors

import (
	"context"

	"github.com/benmeehan/iot-agent/internal/models"
	"github.com/rs/zerolog"
	"github.com/shirou/gopsutil/disk"
)

// DiskMetricCollector collects disk usage metrics.
type DiskMetricCollector struct {
	Logger zerolog.Logger
}

func (d *DiskMetricCollector) Name() string {
	return "disk"
}

func (d *DiskMetricCollector) Collect(ctx context.Context) interface{} {
	d.Logger.Debug().Msg("Starting disk usage collection")
	diskStats, err := disk.Usage("/")
	if err != nil {
		d.Logger.Error().Err(err).Msg("Failed to get disk usage")
		return nil
	}
	d.Logger.Debug().Float64("disk_usage", diskStats.UsedPercent).Msg("Disk usage collected successfully")
	return &diskStats.UsedPercent
}

func (d *DiskMetricCollector) IsEnabled(config *models.MetricsConfig) bool {
	if !config.MonitorDisk {
		d.Logger.Warn().Msg("Disk monitoring is disabled in configuration")
	}
	return config.MonitorDisk
}

func (d *DiskMetricCollector) Unit() string {
	return "percentage"
}

func (d *DiskMetricCollector) Description() string {
	return "Percentage of disk space used on the root filesystem."
}
