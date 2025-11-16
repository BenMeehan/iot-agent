package metrics_collectors

import (
	"context"
	"time"

	"github.com/benmeehan/iot-agent/internal/models"
	"github.com/rs/zerolog"
	"github.com/shirou/gopsutil/net"
)

// NetworkMetrics holds the network I/O rate metrics.
type NetworkMetrics struct {
	NetworkInRate  float64 `json:"network_in,omitempty"`  // bytes/sec
	NetworkOutRate float64 `json:"network_out,omitempty"` // bytes/sec
}

// NetworkMetricCollector collects network I/O rates.
type NetworkMetricCollector struct {
	Logger zerolog.Logger

	// cache previous values for rate calculation
	lastIn   uint64
	lastOut  uint64
	lastTime time.Time
}

// Name returns the identifier for the network metric collector.
func (n *NetworkMetricCollector) Name() string {
	return "network"
}

// Collect retrieves the network I/O rates.
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

	curr := netStats[0]
	now := time.Now()

	// first run → cannot compute rate
	if n.lastTime.IsZero() {
		n.lastIn = curr.BytesRecv
		n.lastOut = curr.BytesSent
		n.lastTime = now
		return nil
	}

	// time delta
	secs := now.Sub(n.lastTime).Seconds()
	if secs <= 0 {
		return nil
	}

	inRate := float64(curr.BytesRecv-n.lastIn) / secs
	outRate := float64(curr.BytesSent-n.lastOut) / secs

	// update cache
	n.lastIn = curr.BytesRecv
	n.lastOut = curr.BytesSent
	n.lastTime = now

	metrics := NetworkMetrics{
		NetworkInRate:  inRate,
		NetworkOutRate: outRate,
	}

	n.Logger.Debug().
		Float64("network_in", metrics.NetworkInRate).
		Float64("network_out", metrics.NetworkOutRate).
		Msg("Network I/O rate collected successfully")

	return metrics
}

// IsEnabled checks if network monitoring is enabled in the configuration.
func (n *NetworkMetricCollector) IsEnabled(config *models.MetricsConfig) bool {
	if !config.MonitorNetwork {
		n.Logger.Debug().Msg("Network monitoring is disabled in configuration")
	}
	return config.MonitorNetwork
}

// Unit specifies the unit for the network I/O rate metric.
func (n *NetworkMetricCollector) Unit() string {
	return "bytes per second"
}

// Description provides a summary of the network metric collected.
func (n *NetworkMetricCollector) Description() string {
	return "Network receive/send rate in bytes per second."
}
