package natsjetstream

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"gochen/messaging"
)

func TestMarshalUnmarshal(t *testing.T) {
	ts := time.Unix(0, 1700000000000000000)
	msg := &messaging.Message{
		ID:        "msg-redis",
		Type:      "order.created",
		Timestamp: ts,
		Payload:   map[string]interface{}{"amount": 99.5},
		Metadata:  map[string]interface{}{"tenant": "demo"},
	}
	data, err := marshalMessage(msg)
	require.NoError(t, err)

	decoded, err := unmarshalMessage(data)
	require.NoError(t, err)

	require.Equal(t, msg.ID, decoded.GetID())
	require.Equal(t, msg.Type, decoded.GetType())
	require.Equal(t, ts.UnixNano(), decoded.GetTimestamp().UnixNano())
	payload := decoded.GetPayload().(map[string]interface{})
	require.Equal(t, 99.5, payload["amount"])
	metadata := decoded.GetMetadata()
	require.Equal(t, "demo", metadata["tenant"])
}
