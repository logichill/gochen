package redisstreams

import (
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"gochen/messaging"
)

func TestEncodeDecodeRoundTrip(t *testing.T) {
	ts := time.Unix(0, 1700000000000000000)
	msg := &messaging.Message{
		ID:        "msg-1",
		Type:      "order.created",
		Timestamp: ts,
		Payload:   map[string]interface{}{"order_id": 42},
		Metadata:  map[string]interface{}{"correlation_id": "cor-123"},
	}

	values, err := encodeMessage(msg)
	require.NoError(t, err)

	entry := redis.XMessage{ID: "1-0", Values: values}
	decoded, err := decodeMessage(entry)
	require.NoError(t, err)

	require.Equal(t, msg.ID, decoded.GetID())
	require.Equal(t, msg.Type, decoded.GetType())
	require.Equal(t, ts.UnixNano(), decoded.GetTimestamp().UnixNano())

	payload := decoded.GetPayload().(map[string]interface{})
	require.Equal(t, float64(42), payload["order_id"]) // JSON numbers decode as float64
	metadata := decoded.GetMetadata()
	require.Equal(t, "cor-123", metadata["correlation_id"])
}

func TestDecodeFallbackTimestamp(t *testing.T) {
	entry := redis.XMessage{ID: "2-0", Values: map[string]interface{}{
		"id":        "msg-2",
		"type":      "order.created",
		"timestamp": "1700000000000000000",
		"payload":   "{}",
		"metadata":  "{}",
	}}
	decoded, err := decodeMessage(entry)
	require.NoError(t, err)
	require.Equal(t, int64(1700000000000000000), decoded.GetTimestamp().UnixNano())
}
