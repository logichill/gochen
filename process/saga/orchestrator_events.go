package saga

import (
	"context"
	"time"

	"gochen/eventing"
	"gochen/logging"
)

type sagaEventPayload struct {
	SagaID    string         `json:"saga_id"`
	Step      string         `json:"step,omitempty"`
	Status    string         `json:"status,omitempty"`
	Error     string         `json:"error,omitempty"`
	Timestamp time.Time      `json:"timestamp"`
	Extra     map[string]any `json:"extra,omitempty"`
}

func (o *SagaOrchestrator) publishEvent(ctx context.Context, eventType SagaEventType, sagaID string, data map[string]any) {
	if o.eventBus == nil {
		return
	}

	payload := sagaEventPayload{
		SagaID:    sagaID,
		Status:    eventType.String(),
		Timestamp: o.clock.Now(),
	}
	if data != nil {
		payload.Extra = data
		if step, ok := data["step"].(string); ok {
			payload.Step = step
		}
		if errStr, ok := data["error"].(string); ok {
			payload.Error = errStr
		}
	}

	evt := eventing.NewEvent(0, "Saga", eventType.String(), uint64(payload.Timestamp.UnixNano()), payload)
	meta := evt.GetMetadata()
	meta.Set("saga_id", sagaID)
	meta.Set("status", eventType.String())
	if payload.Step != "" {
		meta.Set("step", payload.Step)
	}

	if err := o.eventBus.PublishEvent(ctx, evt); err != nil {
		o.logger.Warn(ctx, "failed to publish saga event", logging.Error(err),
			logging.String("event_type", eventType.String()),
			logging.String("saga_id", sagaID))
		return
	}

	o.logger.Debug(ctx, "saga event published",
		logging.String("event_type", eventType.String()),
		logging.String("saga_id", sagaID))
}
