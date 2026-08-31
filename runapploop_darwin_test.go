//go:build darwin

package objc

import (
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
