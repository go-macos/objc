//go:build !darwin

package objc

import (
	"context"
	"errors"
	"testing"
)

func TestObserveStubsReportUnsupported(t *testing.T) {
	if _, err := Observe(context.Background(), DefaultCenter, "x"); !errors.Is(err, ErrUnsupported) {
		t.Errorf("Observe = %v", err)
	}
	if err := Post(DefaultCenter, "x"); !errors.Is(err, ErrUnsupported) {
		t.Errorf("Post = %v", err)
	}
	if err := RunMainLoop(context.Background()); !errors.Is(err, ErrUnsupported) {
		t.Errorf("RunMainLoop = %v", err)
	}
}
