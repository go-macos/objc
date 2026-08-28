//go:build darwin

package objc

import (
	"errors"
	"sync"
	"testing"
	"time"
)

// resetPump puts the lazily-loaded run-loop seams back for the next test.
func resetPump(t *testing.T) {
	t.Helper()
	open, reg, mode := rlOpen, rlReg, rlModeString
	t.Cleanup(func() {
		rlOpen, rlReg, rlModeString = open, reg, mode
		rlOnce, rlRunFn, rlMode, rlLoadErr = onceAgain(), nil, 0, nil
	})
	rlOnce, rlRunFn, rlMode, rlLoadErr = onceAgain(), nil, 0, nil
}

func TestPumpRunLoopRunsAndReturns(t *testing.T) {
	resetPump(t)
	start := time.Now()
	if err := PumpRunLoop(0.05); err != nil {
		t.Fatalf("PumpRunLoop: %v", err)
	}
	// It really waited: a pump that returned at once would not have serviced
	// anything, which is the whole point of it.
	if waited := time.Since(start); waited < 40*time.Millisecond {
		t.Errorf("returned after %v, asked for 50ms", waited)
	}
	// Twice: the second call must not reload anything.
	if err := PumpRunLoop(0.01); err != nil {
		t.Errorf("second PumpRunLoop: %v", err)
	}
}

func TestPumpRunLoopWithNoTimeDoesNothing(t *testing.T) {
	resetPump(t)
	for _, s := range []float64{0, -1} {
		start := time.Now()
		if err := PumpRunLoop(s); err != nil {
			t.Fatalf("PumpRunLoop(%v): %v", s, err)
		}
		if waited := time.Since(start); waited > 20*time.Millisecond {
			t.Errorf("PumpRunLoop(%v) took %v; it must return at once", s, waited)
		}
	}
}

func TestPumpRunLoopReportsALibraryThatWillNotOpen(t *testing.T) {
	resetPump(t)
	boom := errors.New("no CoreFoundation here")
	rlOpen = func() (uintptr, error) { return 0, boom }

	if err := PumpRunLoop(0.01); !errors.Is(err, boom) {
		t.Fatalf("PumpRunLoop = %v, want the open error", err)
	}
	// And it stays failed rather than trying again on every call.
	if err := PumpRunLoop(0.01); !errors.Is(err, boom) {
		t.Fatalf("second call = %v, want the same error", err)
	}
}

// onceAgain is a fresh sync.Once, so a test can make the lazy load happen again.
func onceAgain() sync.Once { return sync.Once{} }
