package metrics_collectors

import (
	"context"

	"github.com/benmeehan/iot-agent/internal/models"
	"github.com/rs/zerolog"
	"github.com/shirou/gopsutil/net"
)

// NetworkMetrics holds the total bytes received and sent over network interfaces.
type NetworkMetrics struct {
	NetworkIn  float64 `json:"network_in,omitempty"`
	NetworkOut float64 `json:"network_out,omitempty"`
}

// NetworkMetricCollector collects network I/O metrics.
type NetworkMetricCollector struct {
	Logger zerolog.Logger
}

// Name returns the identifier for the network metric collector.
func (n *NetworkMetricCollector) Name() string {
	return "network"
}

// Collect retrieves total bytes received and sent across network interfaces.
func (n *NetworkMetricCollector) Collect(ctx context.Context) interface{} {
	n.Logger.Debug().Msg("Collecting network I/O metrics")

	netStats, err := net.IOCounters(false)
	if err != nil {
		n.Logger.Error().Err(err).Msg("Failed to retrieve network statistics")
		return nil
	}
	if len(netStats) == 0 {
		n.Logger.Warn().Msg("No network statistics available")
		return nil
	}

	metrics := NetworkMetrics{
		NetworkIn:  float64(netStats[0].BytesRecv),
		NetworkOut: float64(netStats[0].BytesSent),
	}

	n.Logger.Debug().
		Float64("network_in_bytes", metrics.NetworkIn).
		Float64("network_out_bytes", metrics.NetworkOut).
		Msg("Network I/O metrics collected successfully")

	return metrics
}

// IsEnabled checks if network monitoring is enabled in the configuration.
func (n *NetworkMetricCollector) IsEnabled(config *models.MetricsConfig) bool {
	if !config.MonitorNetwork {
		n.Logger.Debug().Msg("Network monitoring is disabled in configuration")
	}
	return config.MonitorNetwork
}

// Unit specifies the unit for network I/O metrics.
func (n *NetworkMetricCollector) Unit() string {
	return "bytes"
}

// Description provides details of the network I/O metrics collected.
func (n *NetworkMetricCollector) Description() string {
	return "Total bytes received (NetworkIn) and sent (NetworkOut) across network interfaces."
}
