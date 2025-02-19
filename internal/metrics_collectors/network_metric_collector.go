package metrics_collectors

import (
	"context"

	"github.com/benmeehan/iot-agent/internal/models"
	"github.com/rs/zerolog"
	"github.com/shirou/gopsutil/net"
)

// NetworkMetrics contains network I/O metrics.
type NetworkMetrics struct {
	NetworkIn  *float64
	NetworkOut *float64
}

// NetworkMetricCollector collects network I/O metrics.
type NetworkMetricCollector struct {
	Logger zerolog.Logger
}

func (n *NetworkMetricCollector) Name() string {
	return "Network"
}

func (n *NetworkMetricCollector) Collect(ctx context.Context) interface{} {
	netStats, err := net.IOCounters(false)
	if err != nil {
		n.Logger.Error().Err(err).Msg("Failed to get network stats")
		return nil
	}
	if len(netStats) > 0 {
		bytesRecv := float64(netStats[0].BytesRecv)
		bytesSent := float64(netStats[0].BytesSent)
		return NetworkMetrics{
			NetworkIn:  &bytesRecv,
			NetworkOut: &bytesSent,
		}
	}
	return nil
}

func (n *NetworkMetricCollector) IsEnabled(config *models.MetricsConfig) bool {
	return config.MonitorNetwork
}

func (n *NetworkMetricCollector) Unit() string {
	return "bytes"
}

func (n *NetworkMetricCollector) Description() string {
	return "Total bytes received (NetworkIn) and sent (NetworkOut) over the network interfaces."
}
