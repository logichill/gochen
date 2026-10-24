package subscription

import (
	"context"
	stderrors "errors"
	"fmt"
	"runtime/debug"

	"gochen/errors"
	"gochen/eventing"
	"gochen/observe/logging"
	"gochen/policy/retry"
)

// HandlerFunc 订阅处理函数。
type HandlerFunc[ID comparable] func(ctx context.Context, evt eventing.Event[ID]) error

type nonRetriableHandlerError struct {
	cause error
}

func (e nonRetriableHandlerError) Error() string {
	if e.cause == nil {
		return ""
	}
	return e.cause.Error()
}

func (e nonRetriableHandlerError) Unwrap() error {
	return e.cause
}

func (s *Subscription[ID]) handleEvent(ctx context.Context, evt eventing.Event[ID]) error {
	maxAttempts := s.cfg.MaxHandlerRetries + 1
	if maxAttempts <= 0 {
		maxAttempts = 1
	}
	attempts := 0
	cfg := retry.Config{
		MaxAttempts:   maxAttempts,
		InitialDelay:  s.cfg.HandlerRetryBackoff,
		BackoffFactor: 1,
		MaxDelay:      s.cfg.HandlerRetryBackoff,
		JitterRatio:   0,
		RetryIf: func(err error) bool {
			return !isNonRetriableHandlerError(err)
		},
	}
	err := retry.DoWithInfo(ctx, func(attemptCtx context.Context, attempt int) error {
		attempts = attempt
		handleErr := s.callHandler(attemptCtx, evt)
		if handleErr != nil && !isNonRetriableHandlerError(handleErr) && attempt < maxAttempts {
			s.logger.Warn(attemptCtx, "subscription handler retry",
				logging.String("event_id", evt.GetID()),
				logging.String("event_type", evt.GetType()),
				logging.Int("attempt", attempt),
				logging.Error(handleErr),
			)
		}
		return handleErr
	}, cfg)
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if isNonRetriableHandlerError(err) || !s.cfg.SkipFailedAfterMaxRetries {
		return err
	}
	if s.cfg.DeadLetterFunc != nil {
		if dlqErr := s.cfg.DeadLetterFunc(err, &evt); dlqErr != nil {
			s.logger.Error(ctx, "subscription dead-letter hook failed",
				logging.String("event_id", evt.GetID()),
				logging.String("event_type", evt.GetType()),
				logging.Int("attempts", attempts),
				logging.Error(dlqErr),
			)
			return errors.Wrap(dlqErr, errors.Dependency, "subscription dead-letter hook failed").
				WithContext("event_id", evt.GetID())
		}
	}
	s.logger.Error(ctx, "subscription event dead-lettered after handler retries",
		logging.String("event_id", evt.GetID()),
		logging.String("event_type", evt.GetType()),
		logging.Int("attempts", attempts),
		logging.Error(err),
	)
	return nil
}

func (s *Subscription[ID]) callHandler(ctx context.Context, evt eventing.Event[ID]) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			panicValue := fmt.Sprint(recovered)
			stack := string(debug.Stack())
			err = nonRetriableHandlerError{cause: errors.NewCode(errors.Internal, "subscription handler panicked").
				WithContext("panic", panicValue).
				WithContext("panic_stack", stack).
				WithContext("event_id", evt.GetID()).
				WithContext("event_type", evt.GetType())}
			s.logger.Error(ctx, "subscription handler panicked",
				logging.String("event_id", evt.GetID()),
				logging.String("event_type", evt.GetType()),
				logging.Any("panic", recovered),
				logging.String("stack", stack),
			)
		}
	}()
	return s.handler(ctx, evt)
}

func isNonRetriableHandlerError(err error) bool {
	var target nonRetriableHandlerError
	return stderrors.As(err, &target)
}
