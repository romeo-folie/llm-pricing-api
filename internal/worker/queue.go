package worker

import (
	"errors"
	"fmt"

	"github.com/hibiken/asynq"

	"llm-pricing-api/internal/metrics"
)

// queueInspector is the narrow slice of *asynq.Inspector the sampler needs, so
// the sampling logic is unit-testable without a live Redis.
type queueInspector interface {
	Queues() ([]string, error)
	GetQueueInfo(queue string) (*asynq.QueueInfo, error)
}

// QueueSampler publishes asynq queue depth, latency and pause state as gauges.
//
// It exists because nothing else could see *stuck* work: a task that keeps
// failing retries with its asynq Unique(24h) lock still held, so a startup
// enqueue is silently deduplicated and a deployed fix appears not to work —
// which is exactly how a scraper fix was masked for a day. Queue depth and
// oldest-pending latency make that visible (#198).
//
// Like the freshness and benchmark samplers it runs on a ticker in the worker,
// independently of the task pipeline: a sampler driven by task completion would
// say nothing at all while nothing completes.
type QueueSampler struct {
	inspector queueInspector
}

// NewQueueSampler returns a sampler backed by a real asynq Inspector.
func NewQueueSampler(redisOpt asynq.RedisClientOpt) *QueueSampler {
	return &QueueSampler{inspector: asynq.NewInspector(redisOpt)}
}

// Sample reads every queue and republishes the gauges.
//
// A queue that cannot be read is skipped and reported, not fatal: one
// unreadable queue must not blank the samples for the others. Only a failure to
// list queues at all short-circuits.
func (s *QueueSampler) Sample() error {
	queues, err := s.inspector.Queues()
	if err != nil {
		return fmt.Errorf("queue sampler: list queues: %w", err)
	}

	var errs []error
	for _, queue := range queues {
		info, infoErr := s.inspector.GetQueueInfo(queue)
		if infoErr != nil {
			errs = append(errs, fmt.Errorf("queue sampler: queue %q: %w", queue, infoErr))
			continue
		}
		publishQueueGauges(info)
	}
	return errors.Join(errs...)
}

// publishQueueGauges writes one sample set for a queue.
//
// Every state is published even when zero: an absent series is ambiguous
// (sampler down? queue gone?), whereas a zero is unambiguous, and these carry
// no user-supplied cardinality.
func publishQueueGauges(info *asynq.QueueInfo) {
	queue := info.Queue

	for state, count := range map[string]int{
		"pending":     info.Pending,
		"active":      info.Active,
		"scheduled":   info.Scheduled,
		"retry":       info.Retry,
		"archived":    info.Archived,
		"completed":   info.Completed,
		"aggregating": info.Aggregating,
	} {
		metrics.QueueTasks.WithLabelValues(queue, state).Set(float64(count))
	}

	metrics.QueueLatencySeconds.WithLabelValues(queue).Set(info.Latency.Seconds())

	paused := 0.0
	if info.Paused {
		paused = 1
	}
	metrics.QueuePaused.WithLabelValues(queue).Set(paused)
}
