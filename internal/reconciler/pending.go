package reconciler

import (
	"context"
	"encoding/json"
	"time"
)

// redisPendingKey is the Redis hash that mirrors the in-memory pending map so
// the 2-consecutive-fetch confirmation counter survives a worker restart.
// Field = the in-memory pending key (slug+":"+field+":"+effectiveProvider),
// value = a JSON-encoded pendingEntry. The hash carries a rolling pendingTTL.
const redisPendingKey = "reconciler:pending"

// redisPendingOpTimeout bounds each individual Redis pending operation so a slow
// Redis cannot stall a reconcile cycle.
const redisPendingOpTimeout = 2 * time.Second

// pendingEntry is the JSON-serialisable form of pendingChange stored in Redis.
type pendingEntry struct {
	Value      float64   `json:"v"`
	Source     string    `json:"s"`
	FetchCount int       `json:"fc"`
	LastSeen   time.Time `json:"ls"`
}

// snapshotPending copies a live pendingChange into a value pendingEntry. Callers
// take the snapshot while holding the reconciler mutex, then persist it to Redis
// outside the lock without racing on the live entry.
func snapshotPending(e *pendingChange) *pendingEntry {
	return &pendingEntry{
		Value:      e.value,
		Source:     e.source,
		FetchCount: e.fetchCount,
		LastSeen:   e.lastSeen,
	}
}

// loadPendingFromRedis hydrates the in-memory pending map from Redis exactly
// once per process (guarded by pendingLoaded). It is a no-op when no Redis client
// is configured. Entries older than pendingTTL are skipped. Errors are logged and
// swallowed: a load failure simply means the reconciler starts with whatever it
// has in memory (empty on a fresh process), which is the pre-persistence behaviour.
func (r *Reconciler) loadPendingFromRedis(ctx context.Context) {
	if r.redisClient == nil {
		return
	}

	r.mu.Lock()
	if r.pendingLoaded {
		r.mu.Unlock()
		return
	}
	r.mu.Unlock()

	entries, err := r.redisClient.HGetAll(ctx, redisPendingKey).Result()
	if err != nil {
		r.logger.Warn().Err(err).Msg("reconciler: loadPendingFromRedis HGETALL failed")
		return
	}

	cutoff := time.Now().Add(-pendingTTL)
	r.mu.Lock()
	defer r.mu.Unlock()
	// Re-check under lock in case a concurrent Reconcile already loaded.
	if r.pendingLoaded {
		return
	}
	for key, raw := range entries {
		var pe pendingEntry
		if err := json.Unmarshal([]byte(raw), &pe); err != nil {
			r.logger.Warn().Str("key", key).Err(err).Msg("reconciler: skipping unparseable pending entry")
			continue
		}
		if pe.LastSeen.Before(cutoff) {
			continue // stale — let it lapse; sweep will HDEL it below
		}
		// Do not overwrite an entry already created in memory this process.
		if _, exists := r.pending[key]; exists {
			continue
		}
		r.pending[key] = &pendingChange{
			value:      pe.Value,
			source:     pe.Source,
			fetchCount: pe.FetchCount,
			lastSeen:   pe.LastSeen,
		}
	}
	r.pendingLoaded = true
}

// persistPending writes a single pending entry to the Redis hash and refreshes
// the hash TTL. Fire-and-forget: errors are logged, never returned. No-op when
// Redis is not configured. Called outside the reconciler mutex with a value
// snapshot (not the live *pendingChange) so it never races with a concurrent
// cycle mutating the in-memory entry.
func (r *Reconciler) persistPending(key string, e pendingEntry) {
	if r.redisClient == nil {
		return
	}
	data, err := json.Marshal(e)
	if err != nil {
		r.logger.Warn().Str("key", key).Err(err).Msg("reconciler: marshal pending entry failed")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), redisPendingOpTimeout)
	defer cancel()
	if err := r.redisClient.HSet(ctx, redisPendingKey, key, string(data)).Err(); err != nil {
		r.logger.Warn().Str("key", key).Err(err).Msg("reconciler: HSET pending failed")
		return
	}
	if err := r.redisClient.Expire(ctx, redisPendingKey, pendingTTL).Err(); err != nil {
		r.logger.Warn().Err(err).Msg("reconciler: EXPIRE pending hash failed")
	}
}

// deletePendingFromRedis removes one or more fields from the Redis pending hash.
// Fire-and-forget: errors are logged, never returned. No-op when Redis is not
// configured or no keys are given. Called outside the reconciler mutex.
func (r *Reconciler) deletePendingFromRedis(keys ...string) {
	if r.redisClient == nil || len(keys) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), redisPendingOpTimeout)
	defer cancel()
	if err := r.redisClient.HDel(ctx, redisPendingKey, keys...).Err(); err != nil {
		r.logger.Warn().Err(err).Msg("reconciler: HDEL pending failed")
	}
}
