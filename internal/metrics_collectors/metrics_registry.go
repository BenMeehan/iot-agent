package metrics_collectors

// The registry will manage all metric collectors and provide a way to add/remove them dynamically.
type MetricsRegistry struct {
	collectors map[string]MetricCollector
}

// NewMetricsRegistry creates a new MetricsRegistry instance.
func NewMetricsRegistry() *MetricsRegistry {
	return &MetricsRegistry{
		collectors: make(map[string]MetricCollector),
	}
}

// Register adds a new metric collector to the registry.
func (r *MetricsRegistry) Register(collector MetricCollector) {
	r.collectors[collector.Name()] = collector
}

// GetCollectors returns all the metric collectors registered in the registry.
func (r *MetricsRegistry) GetCollectors() map[string]MetricCollector {
	return r.collectors
}
