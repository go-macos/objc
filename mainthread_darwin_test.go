//go:build darwin

package objc

import (
	"fmt"
	"os"
	"runtime"
	"testing"

	"github.com/ebitengine/purego"
)

// The process's MAIN OS thread, made reachable from a test.
//
// ⛔ runtime.LockOSThread PINS THE CURRENT THREAD, NOT THE MAIN ONE. `go test`
// runs every test function on a goroutine of its own, and the scheduler puts
// that goroutine on whatever thread is free, so locking inside a test pins
// whichever thread it happened to land on. That is not the main thread, except
// by luck -- and measured here on 2026-10-11 the luck is good enough to hide
// the problem: CFRunLoopGetCurrent() == CFRunLoopGetMain() on most runs of this
// suite and not on all of them.
//
// Which matters because [WakeMainRunLoop] stops CFRunLoopGetMain, while
// [PumpRunLoop] runs the CURRENT thread's loop. On the runs where the two
// differ, the wake-up cannot reach the loop being pumped, and the test waits
// out its own timer instead:
//
//	stopapp_darwin_test.go:58: the loop ran for 10.057432292s;
//	                           the wake-up did not reach it
//
// That is the CI failure of 2026-10-10 -- one red against nine green, on a
// commit that only raised the Go directive, and not reproducible locally five
// times under either toolchain. A fixture racing the thread scheduler looks
// exactly like that.
//
// runapploop_darwin_test.go said a test "can lock itself to a thread but never
// to that one", and left half of RunAppLoop untested on those grounds. The
// measurement above corrects it in one word: not NEVER, but not RELIABLY -- and
// the main goroutine, the one TestMain runs on, really is on that thread. So it
// is kept there and made to serve this channel, and a test that needs the main
// thread sends its work here rather than hoping.
var mainThread = make(chan func())

// init locks the goroutine that will run [TestMain] to the thread it starts on,
// which is the process's main thread.
//
// It is done in an init function rather than at the top of TestMain because the
// guarantee is about how early it happens: the main goroutine starts on the
// main thread, and nothing moves it off until something gives the scheduler a
// chance to. Package initialisation is the first such chance -- this file sorts
// before objc_darwin_test.go, whose own init loads two frameworks -- so the
// lock is taken before anything in this suite can block.
//
// It is not taken on faith either: [theWokenRunLoopIsThisThreads] is asserted
// by the tests that rely on it, so a future Go that schedules this differently
// fails a test with a sentence instead of flaking at ten seconds.
func init() { runtime.LockOSThread() }

// TestMain keeps the main thread serving [mainThread] for the life of the
// suite, and runs the tests themselves on another goroutine as usual.
//
// ⛔ IT ALSO CREATES THE SHARED APPLICATION HERE, ON THE MAIN THREAD, AND THAT
// IS NOT TIDINESS. +[NSApplication sharedApplication] binds the application to
// the thread that first asks for it, and a test goroutine is not the main one
// (see [mainThread]). Create it anywhere else and the MAIN thread can no longer
// take an event: -nextEventMatchingMask:untilDate:inMode:dequeue: dies with
//
//	SIGTRAP: trace trap
//	PC=0x18ae2b508 m=0 sigcode=0
//	signal arrived during cgo execution
//
// which names nothing of this package and nothing of the test that caused it.
// Measured on 2026-10-11: TestRunAppLoopStopsWhileItIsWaiting passed alone and
// trapped after TestOnDevice_App, which calls App() from its own goroutine; the
// pair sweep put every other AppKit test in the clear.
//
// The measurement also says how narrow the rule is, which is why it is stated
// rather than guessed at: it is the FIRST call that binds. With the application
// created here, the suite's existing off-thread messages to it -- App(),
// -setApplicationIconImage:, -stop: -- all pass. So this is not a ban on
// touching NSApplication from a goroutine; it is where it must be BORN.
func TestMain(m *testing.M) {
	App()

	code := make(chan int, 1)
	go func() { code <- m.Run() }()
	for {
		select {
		case fn := <-mainThread:
			fn()
		case c := <-code:
			os.Exit(c)
		}
	}
}

// onMainThread runs fn on the main OS thread and waits for it to finish.
//
// fn runs on TestMain's goroutine, not the test's, so it must not call
// t.Fatal, t.Skip or anything else that unwinds a test goroutine: those end the
// wrong one. Report by returning a value and assert in the caller.
func onMainThread(fn func()) {
	done := make(chan struct{})
	mainThread <- func() { defer close(done); fn() }
	<-done
}

// theWokenRunLoopIsThisThreads says whether the loop [WakeMainRunLoop] stops is
// the one this goroutine's thread would run -- the premise every test of the
// wake-up stands on. Call it from the thread in question.
func theWokenRunLoopIsThisThreads() (bool, error) {
	h, err := purego.Dlopen(CoreFoundation, purego.RTLD_LAZY|purego.RTLD_GLOBAL)
	if err != nil {
		return false, fmt.Errorf("CoreFoundation: %w", err)
	}
	var current, main func() uintptr
	purego.RegisterLibFunc(&current, h, "CFRunLoopGetCurrent")
	purego.RegisterLibFunc(&main, h, "CFRunLoopGetMain")
	return current() == main(), nil
}

// TestTheMainThreadQueueReachesTheWokenThread is the control for the fixture
// above, and it is a POSITIVE one: it asserts that work sent to [mainThread]
// lands where the wake-up points.
//
// Without it the queue could be serving any thread at all and every test built
// on it would pass or fail for reasons of its own.
func TestTheMainThreadQueueReachesTheWokenThread(t *testing.T) {
	var same bool
	var err error
	onMainThread(func() { same, err = theWokenRunLoopIsThisThreads() })
	if err != nil {
		t.Fatalf("asking which run loop this is: %v", err)
	}
	if !same {
		t.Error("work sent to the main-thread queue did not run on the thread whose run loop WakeMainRunLoop stops")
	}
}

// TestATestGoroutineIsNotReliablyOnTheMainThread records WHY the queue exists,
// without asserting the flake it is built around.
//
// A test goroutine lands on the main thread often enough that asserting either
// answer would be a test that fails on somebody else's machine. So this one
// only reports what it found, and exists so that the next person reading the
// queue above can see the measurement rather than take it from a comment.
func TestATestGoroutineIsNotReliablyOnTheMainThread(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	same, err := theWokenRunLoopIsThisThreads()
	if err != nil {
		t.Fatalf("asking which run loop this is: %v", err)
	}
	t.Logf("a locked test goroutine is on the woken thread this run: %v", same)
}
