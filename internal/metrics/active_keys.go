package metrics

import (
	"sync"
	"time"
)

const (
	activeKeysWindow        = time.Hour
	activeKeysPruneInterval = time.Minute
)

type activeKeyTracker struct {
	mu   sync.Mutex
	seen map[string]map[string]time.Time // tier -> key_hash -> last seen
}

var tracker = &activeKeyTracker{seen: make(map[string]map[string]time.Time)}

func init() {
	go func() {
		ticker := time.NewTicker(activeKeysPruneInterval)
		defer ticker.Stop()
		for now := range ticker.C {
			tracker.prune(now)
		}
	}()
}

// ObserveActiveKey records key activity and updates llm_api_active_keys gauges.
// Distinct key hashes seen within the last hour are counted per tier.
func ObserveActiveKey(tier, keyHash string) {
	if tier == "" || keyHash == "" {
		return
	}
	now := time.Now().UTC()

	tracker.mu.Lock()
	defer tracker.mu.Unlock()

	if tracker.seen[tier] == nil {
		tracker.seen[tier] = make(map[string]time.Time)
	}
	tracker.seen[tier][keyHash] = now
	tracker.pruneLocked(now)
}

func (t *activeKeyTracker) prune(now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.pruneLocked(now)
}

func (t *activeKeyTracker) pruneLocked(now time.Time) {
	cutoff := now.Add(-activeKeysWindow)
	for tier, keys := range t.seen {
		for hash, seenAt := range keys {
			if seenAt.Before(cutoff) {
				delete(keys, hash)
			}
		}
		if len(keys) == 0 {
			delete(t.seen, tier)
		}
	}

	// Recompute gauges after pruning.
	for _, tier := range []string{"free", "developer", "pro"} {
		count := 0
		if keys, ok := t.seen[tier]; ok {
			count = len(keys)
		}
		ActiveKeys.WithLabelValues(tier).Set(float64(count))
	}
}
