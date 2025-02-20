package metrics_collectors

import (
	"context"

	"github.com/benmeehan/iot-agent/internal/models"
	"github.com/rs/zerolog"
	"github.com/shirou/gopsutil/net"
)

// NetworkMetrics contains network I/O metrics.
type NetworkMetrics struct {
	NetworkIn  float64 `json:"network_in,omitempty"`
	NetworkOut float64 `json:"network_out,omitempty"`
}

// NetworkMetricCollector collects network I/O metrics.
type NetworkMetricCollector struct {
	Logger zerolog.Logger
}

func (n *NetworkMetricCollector) Name() string {
	return "network"
}

func (n *NetworkMetricCollector) Collect(ctx context.Context) interface{} {
	n.Logger.Debug().Msg("Starting network I/O metrics collection")
	netStats, err := net.IOCounters(false)
	if err != nil {
		n.Logger.Error().Err(err).Msg("Failed to get network stats")
		return nil
	}
	if len(netStats) > 0 {
		bytesRecv := float64(netStats[0].BytesRecv)
		bytesSent := float64(netStats[0].BytesSent)
		n.Logger.Debug().Float64("network_in", bytesRecv).Float64("network_out", bytesSent).Msg("Network I/O metrics collected successfully")
		return NetworkMetrics{
			NetworkIn:  bytesRecv,
			NetworkOut: bytesSent,
		}
	}
	n.Logger.Warn().Msg("No network statistics available")
	return nil
}

func (n *NetworkMetricCollector) IsEnabled(config *models.MetricsConfig) bool {
	if !config.MonitorNetwork {
		n.Logger.Warn().Msg("Network monitoring is disabled in configuration")
	}
	return config.MonitorNetwork
}

func (n *NetworkMetricCollector) Unit() string {
	return "bytes"
}

func (n *NetworkMetricCollector) Description() string {
	return "Total bytes received (NetworkIn) and sent (NetworkOut) over the network interfaces."
}
