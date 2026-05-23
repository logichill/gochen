package writeflow

import (
	"context"
	"testing"

	"gochen/errors"
)

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
