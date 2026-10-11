//go:build darwin

package objc

import (
	"errors"
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
// against a run loop that is REALLY waiting on the thread that is REALLY woken.
//
// Both halves of that sentence were defects. The loop must be waiting, because
// one with nothing attached returns at once and a test that woke an empty one
// would pass whether or not the wake-up worked; a bare PumpRunLoop(10) on this
// thread was measured at 10.003s, so the main thread's loop does have sources
// and this assertion has teeth. And it must be the WOKEN thread: see
// [mainThread] for the scheduler race that cost a CI red, and for why locking
// inside a test is not enough to get there.
//
// ⛔ THE WAKE-UP IS ALSO EDGE-TRIGGERED, AND THIS FIXTURE USED TO BET ON THE
// EDGE. It slept 50 ms once and woke the loop, on the assumption that
// PumpRunLoop had entered CFRunLoopRunInMode by then. WakeMainRunLoop is
// documented as a no-op when nothing is waiting, so a wake delivered early is
// simply lost -- [TestAWakeUpBeforeTheLoopWaitsIsLost] pins that. The remedy is
// to keep waking until the loop returns, so a lost edge costs one interval
// instead of the whole test.
//
// The waker is JOINED before the test returns, not left running: the seams it
// reads are package variables this test's cleanup puts back, and a goroutine
// still touching them while that happens is a race -- which is what the
// detector said the first time this was written.
func TestWakeMainRunLoopStopsAWaitingLoop(t *testing.T) {
	resetStop(t)

	done := make(chan struct{})
	woke := make(chan struct{})
	go func() {
		defer close(woke)
		tick := time.NewTicker(50 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-done:
				return
			case <-tick.C:
				WakeMainRunLoop()
			}
		}
	}()

	// The pump runs on the main thread, and nothing in there may unwind this
	// test's goroutine, so everything is reported back by value.
	var (
		took     time.Duration
		pumpErr  error
		onWoken  bool
		whichErr error
	)
	onMainThread(func() {
		onWoken, whichErr = theWokenRunLoopIsThisThreads()
		if whichErr != nil || !onWoken {
			return
		}
		start := time.Now()
		// Ten seconds, which is far longer than the wake-up should take and
		// still short enough to fail a test rather than hang a suite.
		pumpErr = PumpRunLoop(10)
		took = time.Since(start)
	})
	close(done)
	<-woke

	if whichErr != nil {
		t.Fatalf("asking which run loop the pump would run: %v", whichErr)
	}
	if !onWoken {
		t.Fatal("the main-thread queue did not reach the thread whose run loop WakeMainRunLoop stops, so this test could only have passed by luck")
	}
	if pumpErr != nil {
		t.Fatalf("PumpRunLoop: %v", pumpErr)
	}
	if took > 5*time.Second {
		t.Errorf("the loop ran for %v; the wake-up did not reach it", took)
	}
}

// TestAWakeUpBeforeTheLoopWaitsIsLost is the premise the test above is built
// on, pinned deterministically because the timing that exposes it on CI cannot
// be forced here.
//
// ⛔ The wake-up is EDGE-TRIGGERED. WakeMainRunLoop is documented as a no-op
// when nothing is waiting, and this shows what that costs: a wake delivered
// before the loop enters CFRunLoopRunInMode is not remembered, so the loop
// then runs its timer out. That is why the fixture above wakes on a TICKER
// rather than once -- a lost edge costs one interval instead of the test.
//
// The timer is one second rather than ten: the point is that the loop ran to
// its own expiry, and a short expiry says that as well as a long one while
// costing a suite nine seconds less.
func TestAWakeUpBeforeTheLoopWaitsIsLost(t *testing.T) {
	resetStop(t)

	var (
		took     time.Duration
		pumpErr  error
		onWoken  bool
		whichErr error
	)
	onMainThread(func() {
		onWoken, whichErr = theWokenRunLoopIsThisThreads()
		if whichErr != nil || !onWoken {
			return
		}
		// Woken BEFORE anything is waiting, which the documentation says does
		// nothing. Nothing is what it must do here too.
		WakeMainRunLoop()

		start := time.Now()
		pumpErr = PumpRunLoop(1)
		took = time.Since(start)
	})

	if whichErr != nil {
		t.Fatalf("asking which run loop the pump would run: %v", whichErr)
	}
	if !onWoken {
		// On the WRONG thread this test passes for the wrong reason: the loop
		// would run its timer out whatever the wake-up did.
		t.Fatal("the main-thread queue did not reach the woken thread, so a lost wake-up cannot be told from a wake-up that went elsewhere")
	}
	if pumpErr != nil {
		t.Fatalf("PumpRunLoop: %v", pumpErr)
	}

	// It ran the timer out, give or take the scheduler. If this ever returns
	// early, the wake-up has become level-triggered and the ticker above is no
	// longer needed -- which is worth finding out from a failure here rather
	// than by guessing.
	if took < 900*time.Millisecond {
		t.Errorf("the loop returned after %v; an early wake-up was remembered, so it is no longer edge-triggered", took)
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
