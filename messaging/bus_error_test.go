package messaging_test

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"gochen/errors"
	"gochen/messaging"
)

type failingBatchTransport struct {
	messaging.ITransport
	err error
}

func (r *failingBatchTransport) PublishAll(context.Context, []messaging.IMessage) error {
	return r.err
}

type codedPublishError struct{}

func (codedPublishError) Error() string               { return "queue unavailable" }
func (codedPublishError) ErrorCode() errors.ErrorCode { return errors.Queue }

func TestPublishAllPreservesErrorChainAndCode(t *testing.T) {
	sibling := errors.New("second transport failure")
	plain := errors.New("connection reset")
	appErr := errors.NewCode(errors.Queue, "queue full").WithContext("queue", "orders")
	originalDetails := appErr.Details()
	for _, size := range []int{1, 2} {
		for _, boundary := range []string{"transport", "middleware"} {
			for _, tc := range []struct {
				name  string
				err   error
				code  errors.ErrorCode
				cause error
				text  string
			}{
				{"joined", errors.Join(appErr, sibling), errors.Queue, sibling, "second transport failure"},
				{"wrapped", fmt.Errorf("publishing orders: %w", appErr), errors.Queue, nil, "publishing orders"},
				{"plain", plain, "", plain, "connection reset"},
				{"explicit_internal", errors.NewCode(errors.Internal, "internal failure"), errors.Internal, nil, "internal failure"},
				{"coded", codedPublishError{}, errors.Queue, nil, "queue unavailable"},
				{"app_error", appErr, errors.Queue, nil, "queue full"},
			} {
				t.Run(fmt.Sprintf("%s/%s/%d", boundary, tc.name, size), func(t *testing.T) {
					transport := &failingBatchTransport{}
					bus := messaging.NewMessageBus(transport)
					wantCode := tc.code
					if boundary == "transport" {
						transport.err = tc.err
						if wantCode == "" {
							wantCode = errors.Dependency
						}
					} else {
						bus.Use(batchMiddlewareFunc(func(context.Context, messaging.IMessage, messaging.HandlerFunc) error { return tc.err }))
						if wantCode == "" {
							wantCode = errors.Internal
						}
					}
					messages := make([]messaging.IMessage, size)
					for i := range messages {
						messages[i] = &messaging.Message{ID: fmt.Sprint(i + 1), Type: "E"}
					}
					err := bus.PublishAll(t.Context(), messages)
					if errors.Code(err) != wantCode {
						t.Fatalf("code=%s, want %s: %v", errors.Code(err), wantCode, err)
					}
					if tc.cause != nil && !errors.Is(err, tc.cause) {
						t.Fatalf("lost cause %v: %v", tc.cause, err)
					}
					if err == nil || !strings.Contains(err.Error(), tc.text) {
						t.Fatalf("lost error context %q: %v", tc.text, err)
					}
				})
			}
		}
	}
	if details := appErr.Details(); !reflect.DeepEqual(details, originalDetails) {
		t.Fatalf("publishing mutated the original error details: %v", details)
	}
}
