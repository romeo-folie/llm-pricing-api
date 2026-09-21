package worker

import (
	"errors"
	"testing"

	"github.com/hibiken/asynq"
	"github.com/prometheus/client_golang/prometheus"
)

// mockQueueInspector is a queueInspector whose queues and per-queue results the
// test controls.
type mockQueueInspector struct {
	queues  []string
	infos   map[string]*asynq.QueueInfo
	errs    map[string]error
	listErr error
}

func (m *mockQueueInspector) Queues() ([]string, error) {
	return m.queues, m.listErr
}

func (m *mockQueueInspector) GetQueueInfo(queue string) (*asynq.QueueInfo, error) {
	if err := m.errs[queue]; err != nil {
		return nil, err
	}
	return m.infos[queue], nil
}

// TestQueueSampler_PublishesEveryState verifies every asynq task state is
// published for a queue, including the zeroes: an absent series is ambiguous
// (sampler down? queue gone?) whereas a zero is not.
func TestQueueSampler_PublishesEveryState(t *testing.T) {
	const queue = "test_queue_states"
	info := &asynq.QueueInfo{
		Queue:       queue,
		Pending:     3,
		Active:      2,
		Scheduled:   1,
		Retry:       4,
		Archived:    5,
		Completed:   6,
		Aggregating: 7,
	}
	s := &QueueSampler{inspector: &mockQueueInspector{
		queues: []string{queue},
		infos:  map[string]*asynq.QueueInfo{queue: info},
	}}

	if err := s.Sample(); err != nil {
		t.Fatalf("Sample: %v", err)
	}

	want := map[string]float64{
		"pending":     3,
		"active":      2,
		"scheduled":   1,
		"retry":       4,
		"archived":    5,
		"completed":   6,
		"aggregating": 7,
	}
	for state, value := range want {
		got, ok := queueTaskCount(t, queue, state)
		if !ok {
			t.Errorf("state %q has no sample", state)
			continue
		}
		if got != value {
			t.Errorf("state %q = %v; want %v", state, got, value)
		}
	}

	if got := gaugeSamples(t, "llm_asynq_queue_latency_seconds", "queue")[queue]; got != 0 {
		t.Errorf("latency = %v; want 0", got)
	}
	if got := gaugeSamples(t, "llm_asynq_queue_paused", "queue")[queue]; got != 0 {
		t.Errorf("paused = %v; want 0 for a running queue", got)
	}
}

// TestQueueSampler_PausedQueue covers the paused flag, which is how a queue
// that was paused by an operator — or by a bug — becomes visible rather than
// merely quiet.
func TestQueueSampler_PausedQueue(t *testing.T) {
	const queue = "test_queue_paused"
	s := &QueueSampler{inspector: &mockQueueInspector{
		queues: []string{queue},
		infos:  map[string]*asynq.QueueInfo{queue: {Queue: queue, Paused: true, Pending: 9}},
	}}

	if err := s.Sample(); err != nil {
		t.Fatalf("Sample: %v", err)
	}

	if got := gaugeSamples(t, "llm_asynq_queue_paused", "queue")[queue]; got != 1 {
		t.Errorf("paused = %v; want 1", got)
	}
	if got, ok := queueTaskCount(t, queue, "pending"); !ok || got != 9 {
		t.Errorf("pending = %v (present=%v); want 9", got, ok)
	}
}

// TestQueueSampler_OneBadQueueDoesNotBlankTheOthers verifies a per-queue read
// failure is reported but does not stop the remaining queues from being
// sampled. Losing every sample because one queue is unreadable would hide a
// real backlog elsewhere.
func TestQueueSampler_OneBadQueueDoesNotBlankTheOthers(t *testing.T) {
	const good = "test_queue_good"
	const bad = "test_queue_bad"
	queueErr := errors.New("redis timeout")

	s := &QueueSampler{inspector: &mockQueueInspector{
		queues: []string{bad, good},
		infos:  map[string]*asynq.QueueInfo{good: {Queue: good, Pending: 2}},
		errs:   map[string]error{bad: queueErr},
	}}

	err := s.Sample()
	if err == nil {
		t.Fatal("expected the per-queue error to be reported, got nil")
	}
	if !errors.Is(err, queueErr) {
		t.Errorf("error chain should contain the queue error; got: %v", err)
	}
	if got, ok := queueTaskCount(t, good, "pending"); !ok || got != 2 {
		t.Errorf("pending for the readable queue = %v (present=%v); want 2", got, ok)
	}
	assertGaugeAbsentByLabel(t, "llm_asynq_queue_paused", "queue", bad)
}

// TestQueueSampler_ListFailureIsFatal verifies that being unable to list queues
// at all is returned to the caller rather than silently publishing nothing.
func TestQueueSampler_ListFailureIsFatal(t *testing.T) {
	listErr := errors.New("connection refused")
	s := &QueueSampler{inspector: &mockQueueInspector{listErr: listErr}}

	err := s.Sample()
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if !errors.Is(err, listErr) {
		t.Errorf("error chain should contain the list error; got: %v", err)
	}
}

// queueTaskCount reads one llm_asynq_queue_tasks sample by the (queue, state)
// pair. The generic gaugeSamples helper keys by a single label, which cannot
// distinguish the same state across queues in a shared registry.
func queueTaskCount(t *testing.T, queue, state string) (float64, bool) {
	t.Helper()
	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather default registry: %v", err)
	}
	for _, family := range families {
		if family.GetName() != "llm_asynq_queue_tasks" {
			continue
		}
		for _, m := range family.GetMetric() {
			var gotQueue, gotState string
			for _, l := range m.GetLabel() {
				switch l.GetName() {
				case "queue":
					gotQueue = l.GetValue()
				case "state":
					gotState = l.GetValue()
				}
			}
			if gotQueue == queue && gotState == state {
				return m.GetGauge().GetValue(), true
			}
		}
	}
	return 0, false
}

// assertGaugeAbsentByLabel is the label-general form of assertGaugeAbsent.
func assertGaugeAbsentByLabel(t *testing.T, name, label, value string) {
	t.Helper()
	if _, ok := gaugeSamples(t, name, label)[value]; ok {
		t.Errorf("%s must have no sample for %s %q; got one", name, label, value)
	}
}
