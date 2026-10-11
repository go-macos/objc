//go:build darwin

package objc

import (
	"errors"
	"testing"
	"time"
)

// TestRunAppLoopStopsBeforeItWaits: a caller that already wants to stop must
// not be made to wait for an event first. That is the ordinary shape of a
// program told to quit while it was still starting up.
//
// ⛔ THIS COMMENT USED TO SAY THE OTHER HALF COULD NOT BE TESTED HERE, on the
// grounds that `go test` runs every test on a goroutine of its own, so a test
// "can lock itself to a thread but never to that one". Measured on 2026-10-11,
// that is wrong in one word: not NEVER, but not RELIABLY -- a test goroutine
// lands on the main thread often, which is worse than never, because it makes a
// fixture that cannot work pass most of the time. It cost a CI red in
// stopapp_darwin_test.go, and [mainThread] now reaches that thread on purpose.
// The other half is [TestRunAppLoopStopsWhileItIsWaiting], below.
func TestRunAppLoopStopsBeforeItWaits(t *testing.T) {
	// ⛔ THIS USED TO RUN THE LOOP ON A GOROUTINE OF ITS OWN, which breaks the
	// precondition RunAppLoop states in its own documentation -- the process
	// main OS thread -- and it got away with it because a stop that answers at
	// once returns before any event is asked for. What it did not get away with
	// is leaving AppKit behind: -setActivationPolicy: and -finishLaunching ran
	// on a thread that was not the main one, and the NEXT test to ask the main
	// thread for an event died in -nextEventMatchingMask: with
	//
	//	SIGTRAP: trace trap ... signal arrived during cgo execution
	//
	// measured here on 2026-10-11. Alone, each test passed. A test that breaks
	// its subject's documented precondition can poison the process for every
	// test after it, and the one that fails is not the one at fault.
	done := make(chan struct{})
	go func() {
		defer close(done)
		onMainThread(func() { RunAppLoop(1, func() bool { return true }) })
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		// The loop is on the main thread, which is also the thread serving
		// mainThread, so this says the suite is wedged as well as failing.
		t.Fatal("a loop told to stop at once did not return")
	}
}

// TestRunAppLoopStopsWhileItIsWaiting is the half this file called untestable,
// and it is the half the loop exists for: a program told to stop while it sits
// in -nextEventMatchingMask:untilDate:inMode:dequeue: with NOBODY AT THE
// MACHINE must get its thread back anyway.
//
// -[NSApplication run] does not, which is why this loop was written: its stop:
// flag is read after an event is processed, and no event is coming. What makes
// this one different is that the wait is BOUNDED by loopWaitSeconds, so the
// wait expires on its own and the question gets asked again.
//
// That is what the count asserts, and it is the assertion that matters. A test
// that only checked the loop returned would pass against a loop that asked
// once and never waited at all -- which is TestRunAppLoopStopsBeforeItWaits,
// already covered above. Being asked AGAIN, with no event sent and no input
// device touched, is the bounded wait working.
//
// The stop function carries its own deadline rather than reading a flag the
// test sets later: this runs on the main thread, which is also the thread
// serving [mainThread], so a loop that failed to stop would hang the whole
// suite with no test timeout able to reach it. A deadline inside the loop ends
// it whatever happens.
func TestRunAppLoopStopsWhileItIsWaiting(t *testing.T) {
	const waiting = 300 * time.Millisecond // six turns of loopWaitSeconds

	var (
		asked    int
		took     time.Duration
		onWoken  bool
		whichErr error
	)
	onMainThread(func() {
		onWoken, whichErr = theWokenRunLoopIsThisThreads()
		if whichErr != nil || !onWoken {
			return
		}
		// Accessory: a menu-bar policy, so this does not take over the screen
		// of whoever is running the suite.
		deadline := time.Now().Add(waiting)
		start := time.Now()
		RunAppLoop(1, func() bool {
			asked++
			return time.Now().After(deadline)
		})
		took = time.Since(start)
	})

	if whichErr != nil {
		t.Fatalf("asking which run loop this is: %v", whichErr)
	}
	if !onWoken {
		t.Fatal("the main-thread queue did not reach the main thread, and RunAppLoop must run there")
	}
	if took < waiting {
		t.Errorf("the loop returned after %v, before its own deadline at %v; it cannot have waited", took, waiting)
	}
	// Generous: the point is that it came back at all, not when.
	if took > 5*time.Second {
		t.Errorf("the loop ran for %v; a stop decided mid-wait did not get the thread back", took)
	}
	// Six turns are expected; three is enough to say it was asked again after
	// a wait expired rather than only on the way in.
	if asked < 3 {
		t.Errorf("the stop function was asked %d times in %v; the wait is not bounded, so the question is only asked when an event arrives", asked, took)
	}
	t.Logf("asked %d times in %v, with no event sent", asked, took)
}

// TestWaitUntilBuildsARealDate checks the one call that needed a bridge of its
// own: a double goes in a floating-point register, and the general Send passes
// everything in integer ones.
//
// The date is compared against NOW rather than merely being non-nil: a date
// built from the wrong register is still an NSDate, and would sail through any
// test that only asked whether it existed. Fifty milliseconds from now must be
// after a date made a moment ago and before one made a second later.
func TestWaitUntilBuildsARealDate(t *testing.T) {
	before := ClassID("NSDate").Send(Sel("date"))
	got := waitUntil(loopWaitSeconds)
	if got == 0 {
		t.Fatal("no date at all")
	}
	if got.Send(Sel("laterDate:"), before) != got {
		t.Error("a date fifty milliseconds from now is not later than one made just before it")
	}
	// And bounded above: distantFuture is what a wrong register might land on.
	far := ClassID("NSDate").Send(Sel("distantFuture"))
	if got.Send(Sel("laterDate:"), far) == got {
		t.Error("the date is at or beyond the distant future")
	}
}

// TestWaitUntilWithNoRuntime: a runtime that will not load leaves the caller
// with no deadline rather than a bad one, which is what the loop is written to
// cope with.
func TestWaitUntilWithNoRuntime(t *testing.T) {
	open, reg := dateOpen, dateReg
	t.Cleanup(func() {
		dateOpen, dateReg = open, reg
		dateOnce, dateFn, dateErr = onceAgain(), nil, nil
	})
	dateOnce, dateFn, dateErr = onceAgain(), nil, nil
	dateOpen = func() (uintptr, error) { return 0, errors.New("no runtime here") }
	if got := waitUntil(1); got != 0 {
		t.Errorf("waitUntil = %v with no runtime, want 0", got)
	}
	// Twice: the second call takes the recorded-failure path.
	if got := waitUntil(1); got != 0 {
		t.Errorf("second waitUntil = %v", got)
	}
}
