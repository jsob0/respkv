package main

import (
	"errors"
	"net/http"
	"net/http/pprof"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	metricsNamespace    = "respkv"
	unknownCommandLabel = "unknown"
)

type metrics struct {
	registry         *prometheus.Registry
	commandDuration  map[string]prometheus.Observer
	unknownCommand   prometheus.Observer
	connectedClients prometheus.Gauge
	connectionsTotal prometheus.Counter
}

func newMetrics(store *Store) *metrics {
	duration := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: metricsNamespace,
		Name:      "command_duration_seconds",
		Help:      "Time spent executing commands, by command name.",
		Buckets:   prometheus.ExponentialBuckets(1e-6, 4, 8),
	}, []string{"command"})

	m := &metrics{
		registry:        prometheus.NewRegistry(),
		commandDuration: make(map[string]prometheus.Observer, len(commands)),
		unknownCommand:  duration.WithLabelValues(unknownCommandLabel),
		connectedClients: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: metricsNamespace,
			Name:      "connected_clients",
			Help:      "Number of open client connections.",
		}),
		connectionsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Name:      "connections_total",
			Help:      "Client connections accepted since start.",
		}),
	}
	for name := range commands {
		observer := duration.WithLabelValues(name)
		m.commandDuration[name] = observer
		m.commandDuration[strings.ToUpper(name)] = observer
	}

	keys := prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Namespace: metricsNamespace,
		Name:      "keys",
		Help:      "Number of keys in the store, including expired keys not removed yet.",
	}, func() float64 {
		return float64(store.Len())
	})

	m.registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		duration,
		m.connectedClients,
		m.connectionsTotal,
		keys,
	)
	return m
}

func (m *metrics) observeCommand(name string, d time.Duration) {
	observer, ok := m.commandDuration[name]
	if !ok {
		observer, ok = m.commandDuration[strings.ToLower(name)]
	}
	if !ok {
		observer = m.unknownCommand
	}
	observer.Observe(d.Seconds())
}

func (m *metrics) registerAOF(aof *AOF) {
	m.registry.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Namespace: metricsNamespace,
		Name:      "aof_failed",
		Help:      "1 if writing the append-only file failed and new writes are not persisted, 0 otherwise.",
	}, func() float64 {
		if err := aof.Err(); err != nil && !errors.Is(err, errAOFClosed) {
			return 1
		}
		return 0
	}))
}

func (m *metrics) handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{}))
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	return mux
}
