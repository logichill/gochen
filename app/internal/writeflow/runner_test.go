package writeflow

import (
	"context"
	"testing"

	"gochen/errors"
)

func TestPostCommitsFiltersNilCallbacks(t *testing.T) {
	var called bool
	callbacks := PostCommits(nil, func(context.Context) error {
		called = true
		return nil
	})

	if len(callbacks) != 1 {
		t.Fatalf("expected one callback, got %d", len(callbacks))
	}
	if err := callbacks[0](context.Background()); err != nil {
		t.Fatalf("callback returned error: %v", err)
	}
	if !called {
		t.Fatal("expected callback to run")
	}
}

func TestCallbacksForBuildsCallbacksInInputOrder(t *testing.T) {
	items := []int{1, 2, 3}
	var got []int
	callbacks := CallbacksFor(items, func(item int) func(context.Context) error {
		if item == 2 {
			return nil
		}
		return func(context.Context) error {
			got = append(got, item)
			return nil
		}
	})
	if len(callbacks) != 2 {
		t.Fatalf("len(callbacks) = %d", len(callbacks))
	}
	for _, callback := range callbacks {
		if err := callback(context.Background()); err != nil {
			t.Fatalf("callback() error = %v", err)
		}
	}
	if len(got) != 2 || got[0] != 1 || got[1] != 3 {
		t.Fatalf("callback order = %v", got)
	}
}

func TestValidateBatchSizeBoundaries(t *testing.T) {
	if err := ValidateBatchSize(3, 3); err != nil {
		t.Fatalf("expected boundary size to pass: %v", err)
	}
	if err := ValidateBatchSize(4, 3); !errors.Is(err, errors.Validation) {
		t.Fatalf("expected validation error, got %v", err)
	}
}

func TestRun_NoTxPostCommitFailsBeforeOutsidePreflight(t *testing.T) {
	var beforeCalled bool
	var validateCalled bool
	var writeCalled bool

	err := Run(context.Background(), nil, Plan{
		Before: func(context.Context) error {
			beforeCalled = true
			return nil
		},
		Validate: func(context.Context) error {
			validateCalled = true
			return nil
		},
		Write: func(context.Context) error {
			writeCalled = true
			return nil
		},
		PostCommits: []PostCommit{
			func(context.Context) error { return nil },
		},
		BeforeValidateOutsideTx: true,
	})

	if !errors.Is(err, errors.Unsupported) {
		t.Fatalf("expected Unsupported error, got %v", err)
	}
	if beforeCalled || validateCalled || writeCalled {
		t.Fatalf("expected no preflight/write side effects, got before=%v validate=%v write=%v", beforeCalled, validateCalled, writeCalled)
	}
}

func TestRun_NoTxOutsidePreflightStillRunsWithoutPostCommit(t *testing.T) {
	var beforeCalled bool
	var validateCalled bool
	var writeCalled bool

	err := Run(context.Background(), nil, Plan{
		Before: func(context.Context) error {
			beforeCalled = true
			return nil
		},
		Validate: func(context.Context) error {
			validateCalled = true
			return nil
		},
		Write: func(context.Context) error {
			writeCalled = true
			return nil
		},
		BeforeValidateOutsideTx: true,
	})

	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !beforeCalled || !validateCalled || !writeCalled {
		t.Fatalf("expected preflight/write to run, got before=%v validate=%v write=%v", beforeCalled, validateCalled, writeCalled)
	}
}
