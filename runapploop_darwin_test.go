//go:build darwin

package objc

import (
	"errors"
	"runtime"
	"testing"
	"time"
)

// TestRunAppLoopStopsBeforeItWaits: a caller that already wants to stop must
// not be made to wait for an event first. That is the ordinary shape of a
// program told to quit while it was still starting up, and it is the half of
// this loop a test in this package can actually reach.
//
// THE OTHER HALF IS NOT TESTED HERE, and pretending otherwise would be worse
// than saying so. Being woken out of the wait is what makes this loop worth
// having, and the wake-up goes to the MAIN thread's run loop: `go test` runs
// every test on a goroutine of its own, so a test can lock itself to a thread
// but never to that one. A test written against it hung for ten minutes, which
// is exactly right -- it was waiting on a loop nothing could reach.
//
// It is verified in the program that needed it instead: a menu-bar desk that
// enters this loop while it waits for a headset, finds one, and gets its thread
// back with nobody at the machine.
func TestRunAppLoopStopsBeforeItWaits(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	done := make(chan struct{})
	go func() {
		defer close(done)
		RunAppLoop(1, func() bool { return true })
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("a loop told to stop at once did not return")
	}
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
