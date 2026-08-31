//go:build darwin

package objc

import (
	"errors"
	"runtime"
	"testing"
	"time"
)

// resetStop puts the lazily-loaded stop seams back for the next test.
func resetStop(t *testing.T) {
	t.Helper()
	open, reg := stopOpen, stopReg
	t.Cleanup(func() {
		stopOpen, stopReg = open, reg
		stopOnce, stopGetMain, stopStop, stopLoadErr = onceAgain(), nil, nil, nil
	})
	stopOnce, stopGetMain, stopStop, stopLoadErr = onceAgain(), nil, nil, nil
}

// TestWakeMainRunLoopStopsAWaitingLoop is the whole point, and it is asserted
// against a run loop that is REALLY waiting.
//
// A run loop with nothing attached returns at once, so a test that woke an
// empty one would pass whether or not the wake-up worked. This one attaches a
// timer far enough out that the loop cannot return by itself inside the test's
// patience, and then requires it to return anyway.
func TestWakeMainRunLoopStopsAWaitingLoop(t *testing.T) {
	resetStop(t)

	// The main thread's run loop is the one that is woken, so the loop has to
	// run on the main OS thread.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// The waker is JOINED before the test returns, not left running. The seams
	// it reads are package variables that this test's cleanup puts back, and a
	// goroutine still touching them while that happens is a race -- which is
	// what the detector said the first time this was written.
	woke := make(chan struct{})
	go func() {
		defer close(woke)
		time.Sleep(50 * time.Millisecond)
		WakeMainRunLoop()
	}()

	start := time.Now()
	// Ten seconds, which is far longer than the wake-up should take and still
	// short enough to fail a test rather than hang a suite.
	if err := PumpRunLoop(10); err != nil {
		t.Fatalf("PumpRunLoop: %v", err)
	}
	took := time.Since(start)
	<-woke
	if took > 5*time.Second {
		t.Errorf("the loop ran for %v; the wake-up did not reach it", took)
	}
}

// TestStopAppIsSafeWithNothingRunning: stop: on an application that is not in
// its run loop is a no-op, and so is stopping a loop that is not running. A
// caller that stops twice, or stops something that never started, must not
// crash -- which is exactly what a program tidying up after a failure does.
func TestStopAppIsSafeWithNothingRunning(t *testing.T) {
	resetStop(t)
	StopApp()
	StopApp()
}

func TestWakeMainRunLoopReportsNothingWhenTheLibraryWillNotOpen(t *testing.T) {
	resetStop(t)
	stopOpen = func() (uintptr, error) { return 0, errors.New("no CoreFoundation here") }
	// It says nothing, because there is nobody to say it to: waking a run loop
	// is best-effort by nature, and a caller stopping a program it has already
	// decided to stop has no use for an error. What it must NOT do is panic on
	// the nil function pointers the failed load leaves behind.
	WakeMainRunLoop()
	if stopLoadErr == nil {
		t.Error("the load failure was not recorded")
	}
	// Twice: the second call takes the recorded-failure path rather than
	// loading again.
	WakeMainRunLoop()
}

func TestWakeMainRunLoopWithNoMainLoop(t *testing.T) {
	resetStop(t)
	stopReg = func(uintptr) {
		stopGetMain = func() uintptr { return 0 }
		stopStop = func(uintptr) { t.Error("a run loop that is not there was stopped") }
	}
	WakeMainRunLoop()
}
