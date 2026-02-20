package reconciler

import (
	"context"
	"encoding/json"
	"time"

	"github.com/redis/go-redis/v9"

	"llm-pricing-api/internal/webhooks"
)

const (
	redisPubSubChannel = "price:changes"
	redisSeqKey        = "price:changes:seq"
	redisBufferKey     = "price:changes:buffer"
	redisBufferMaxSize = 1000
	redisBufferTTL     = 24 * time.Hour
)

// SSEEvent is the canonical event shape published to Redis Pub/Sub and stored
// in the replay buffer. It extends webhooks.Payload with a monotonic EventID.
type SSEEvent struct {
	webhooks.Payload
	EventID int64 `json:"event_id"`
}

// publishEvent publishes a confirmed price event to Redis Pub/Sub and stores it
// in the replay buffer sorted set. It is fire-and-forget: any Redis error is
// logged as a warning but never returned — the confirmed price write already
// succeeded and must not be rolled back.
func (r *Reconciler) publishEvent(ctx context.Context, event SSEEvent) {
	if r.redisClient == nil {
		return
	}

	// Generate a monotonic event_id.
	eventID, err := r.redisClient.Incr(ctx, redisSeqKey).Result()
	if err != nil {
		r.logger.Warn().Err(err).Msg("reconciler: publishEvent INCR failed")
		return
	}
	event.EventID = eventID

	data, err := json.Marshal(event)
	if err != nil {
		r.logger.Warn().Err(err).Msg("reconciler: publishEvent marshal failed")
		return
	}
	payload := string(data)

	// Publish to Pub/Sub channel.
	if err := r.redisClient.Publish(ctx, redisPubSubChannel, payload).Err(); err != nil {
		r.logger.Warn().Int64("event_id", eventID).Err(err).Msg("reconciler: publishEvent PUBLISH failed")
		// Continue — still write to buffer so reconnecting clients can replay.
	}

	// Store in replay buffer (sorted set, score = event_id).
	if err := r.redisClient.ZAdd(ctx, redisBufferKey, redis.Z{
		Score:  float64(eventID),
		Member: payload,
	}).Err(); err != nil {
		r.logger.Warn().Int64("event_id", eventID).Err(err).Msg("reconciler: publishEvent ZADD failed")
		return
	}

	// Trim buffer to newest 1,000 members.
	if err := r.redisClient.ZRemRangeByRank(ctx, redisBufferKey, 0, -int64(redisBufferMaxSize+1)).Err(); err != nil {
		r.logger.Warn().Err(err).Msg("reconciler: publishEvent ZREMRANGEBYRANK failed")
	}

	// Refresh 24h TTL on every write.
	if err := r.redisClient.Expire(ctx, redisBufferKey, redisBufferTTL).Err(); err != nil {
		r.logger.Warn().Err(err).Msg("reconciler: publishEvent EXPIRE failed")
	}
}
