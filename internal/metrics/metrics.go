// Package metrics exposes Prometheus instrumentation for ingestion.
package metrics

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	runsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "charging_ingest_runs_total",
		Help: "Ingestion passes by CPO, kind and result.",
	}, []string{"cpo", "kind", "result"})

	rowsSeen = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "charging_ingest_rows_seen",
		Help: "Connectors seen in the last pass.",
	}, []string{"cpo", "kind"})

	changesTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "charging_ingest_tariff_changes_total",
		Help: "Tariff version changes recorded.",
	}, []string{"cpo"})

	lastSuccess = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "charging_ingest_last_success_timestamp_seconds",
		Help: "Unix time of the last error-free pass.",
	}, []string{"cpo", "kind"})

	duration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "charging_ingest_duration_seconds",
		Help:    "Ingestion pass duration.",
		Buckets: prometheus.DefBuckets,
	}, []string{"cpo", "kind"})
)

// Observe records the outcome of one ingestion pass. Matches the signature of
// ingest.Engine.OnRun. now is passed in to keep it testable/deterministic.
func Observe(now time.Time, cpo, kind string, rows, changes int, dur time.Duration, err error) {
	result := "ok"
	if err != nil {
		result = "error"
	}
	runsTotal.WithLabelValues(cpo, kind, result).Inc()
	rowsSeen.WithLabelValues(cpo, kind).Set(float64(rows))
	duration.WithLabelValues(cpo, kind).Observe(dur.Seconds())
	if changes > 0 {
		changesTotal.WithLabelValues(cpo).Add(float64(changes))
	}
	if err == nil {
		lastSuccess.WithLabelValues(cpo, kind).Set(float64(now.Unix()))
	}
}

// Handler serves the Prometheus exposition format.
func Handler() http.Handler { return promhttp.Handler() }
