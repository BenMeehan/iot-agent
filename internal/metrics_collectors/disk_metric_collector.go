package metrics_collectors

import (
	"context"

	"github.com/benmeehan/iot-agent/internal/models"
	"github.com/rs/zerolog"
	"github.com/shirou/gopsutil/disk"
)

// DiskMetricCollector collects disk usage percentage for the root filesystem.
type DiskMetricCollector struct {
	Logger zerolog.Logger
}

// Name returns the identifier for the disk metric collector.
func (d *DiskMetricCollector) Name() string {
	return "disk"
}

// Collect retrieves the disk usage percentage for the root filesystem.
func (d *DiskMetricCollector) Collect(ctx context.Context) interface{} {
	d.Logger.Debug().Msg("Collecting disk usage metrics")

	diskUsage, err := disk.Usage("/")
	if err != nil {
		d.Logger.Error().Err(err).Msg("Failed to retrieve disk usage metrics")
		return nil
	}

	d.Logger.Debug().Float64("disk_usage", diskUsage.UsedPercent).Msg("Disk usage metrics collected successfully")
	return &diskUsage.UsedPercent
}

// IsEnabled checks if disk monitoring is enabled in the configuration.
func (d *DiskMetricCollector) IsEnabled(config *models.MetricsConfig) bool {
	if !config.MonitorDisk {
		d.Logger.Debug().Msg("Disk monitoring is disabled in configuration")
	}
	return config.MonitorDisk
}

// Unit specifies the unit for the disk usage metric.
func (d *DiskMetricCollector) Unit() string {
	return "percentage"
}

// Description provides a summary of the disk metric collected.
func (d *DiskMetricCollector) Description() string {
	return "Percentage of disk space used on the root filesystem."
}
