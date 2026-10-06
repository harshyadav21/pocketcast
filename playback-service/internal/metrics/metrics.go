package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	HTTPRequests = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "playback_http_requests_total",
		Help: "HTTP requests by route, method and status code.",
	}, []string{"route", "method", "status"})

	HTTPDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "playback_http_request_duration_seconds",
		Help:    "HTTP request latency by route.",
		Buckets: prometheus.DefBuckets,
	}, []string{"route"})

	EpisodesCompleted = promauto.NewCounter(prometheus.CounterOpts{
		Name: "playback_episodes_completed_total",
		Help: "Episodes marked completed (first completion per user only).",
	})

	PublishFailures = promauto.NewCounter(prometheus.CounterOpts{
		Name: "playback_event_publish_failures_total",
		Help: "episode_completed events that failed to publish and will be retried on the next heartbeat.",
	})
)
