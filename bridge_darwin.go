//go:build darwin

package objc

import (
	"context"
	"runtime"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

// init points the OS-independent seams (declared in objc.go) at the real
// purego/objc-backed implementations. Everything above these seams — the
// caches, name validation, the GoString marshalling logic and Load — is shared
// with every platform; only these leaf calls are darwin-specific.
func init() {
	resolveSel = func(name string) uintptr { return uintptr(objc.RegisterName(name)) }
	resolveClass = func(name string) uintptr { return uintptr(objc.GetClass(name)) }
	dlopenFn = func(path string, mode int) (uintptr, error) { return purego.Dlopen(path, mode) }
	lengthOfBytesFn = func(id ID) int {
		return int(id.Send(objc.RegisterName("lengthOfBytesUsingEncoding:"), nsUTF8Encoding))
	}
	getCStringFn = func(id ID, buf []byte) bool {
		return id.Send(objc.RegisterName("getCString:maxLength:encoding:"),
			unsafe.Pointer(&buf[0]), len(buf), nsUTF8Encoding) != 0
	}
}

// Send sends selector sel to id with args and returns the result typed as T.
// Use it when the message returns a scalar or a by-value struct (e.g. a CGFloat
// via Send[float64] or an NSRect via a matching struct), so purego marshals the
// return through the correct calling convention. For an object (or ignored)
// result, use the inherited ID.Send, which returns an [ID].
func Send[T any](id ID, sel SEL, args ...any) T {
	return objc.Send[T](id, sel, args...)
}

// NSString builds an autoreleased NSString from a Go string.
func NSString(s string) ID {
	return ClassID("NSString").Send(Sel("stringWithUTF8String:"), s)
}

// RegisterClass defines a new Objective-C class named name with superclass
// super and the given instance methods, and returns it. Each method's Fn is a
// Go closure whose first two parameters are the receiver [ID] and the invoked
// [SEL]; RegisterClass wraps it into an IMP. It is the runtime-class mechanism
// behind delegates, timer targets and menu-action handlers. When the class must
// formally conform to a protocol, use [RegisterClassWithProtocols]. (Instance
// variables are intentionally omitted; the fleet's classes need none.)
func RegisterClass(name string, super Class, methods []MethodDef) (Class, error) {
	return RegisterClassWithProtocols(name, super, nil, methods)
}

// GetProtocol returns the Objective-C protocol named name (a nil pointer if no
// such protocol is registered in the process). Pass the result to
// [RegisterClassWithProtocols] so a runtime class formally conforms to it —
// required, for example, by APIs that check conformsToProtocol: such as
// -[WKWebViewConfiguration setURLSchemeHandler:forURLScheme:], which demands a
// WKURLSchemeHandler.
func GetProtocol(name string) *Protocol { return objc.GetProtocol(name) }

// RegisterClassWithProtocols is [RegisterClass] plus formal protocol
// conformance: the returned class declares each of protocols (so
// conformsToProtocol: reports true) in addition to implementing methods. Use it
// when an API validates protocol conformance rather than merely probing
// respondsToSelector:.
func RegisterClassWithProtocols(name string, super Class, protocols []*Protocol, methods []MethodDef) (Class, error) {
	return objc.RegisterClass(name, super, protocols, nil, methods)
}

// AutoreleasePool runs fn inside a fresh NSAutoreleasePool, draining the pool
// when fn returns (even if it panics). Use it around a burst of autoreleased
// allocations on a thread that has no ambient pool.
//
// The goroutine is PINNED to its OS thread for the whole of it.
//
// An NSAutoreleasePool belongs to the thread that created it, and Go moves an
// unlocked goroutine to another thread at any preemption point — a function
// call, an allocation, a channel operation, anything fn does. The pool is then
// drained on a thread that never owned it, and the Objective-C runtime does not
// return from that: it is a SIGSEGV inside objc_autoreleasePoolPop, at a
// program counter with nothing of this package on the stack.
//
// It is a race, so it does not fail on the machine it was written on. It failed
// on a CI runner on 2026-08-27, in a caller that had done nothing but add two
// more shortcuts to look up.
func AutoreleasePool(fn func()) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	pool := ClassID("NSAutoreleasePool").Send(Sel("alloc")).Send(Sel("init"))
	defer pool.Send(Sel("drain"))
	fn()
}

// appKitOnce loads AppKit the first time anything here asks for the shared
// application.
var appKitOnce sync.Once

// App returns the shared NSApplication instance (+[NSApplication
// sharedApplication]), creating it on first call, and loads AppKit first.
//
// The load belongs here rather than with the caller. This is the function that
// names NSApplication, and a caller who forgets gets a nil class, a nil
// application, and every message after it returning zero — in silence, because
// Objective-C does not complain about a message to nil. A menu-bar program
// built that way starts, prints its greeting and exits in a millisecond, with
// nothing on screen and nothing in a log. It cost six attempts to find once.
func App() ID {
	appKitOnce.Do(func() { _ = Load(AppKit) })
	return ClassID("NSApplication").Send(Sel("sharedApplication"))
}

// RunApp sets the shared application's activation policy and enters the AppKit
// run loop ([NSApp run]), which blocks until the application terminates. It
// must be called on the process main OS thread. policy is an
// NSApplicationActivationPolicy value (0 = Regular, 1 = Accessory/menu-bar).
func RunApp(policy int) {
	app := App()
	app.Send(Sel("setActivationPolicy:"), policy)
	app.Send(Sel("run"))
}

// ---------------------------------------------------------------------------
// NSObject / NSDictionary helpers.
// ---------------------------------------------------------------------------

// Stringify renders any object as a Go string: NSStrings directly, anything
// else through -description (so a non-string value degrades to its textual form
// rather than crashing the getCString path). The nil object yields "".
func Stringify(v ID) string {
	if v == 0 {
		return ""
	}
	if v.Send(Sel("isKindOfClass:"), ClassID("NSString")) != 0 {
		return GoString(v)
	}
	return GoString(v.Send(Sel("description")))
}

// DictToMap flattens an NSDictionary to map[string]string, rendering both keys
// and values with [Stringify]. The nil dictionary yields an empty map.
func DictToMap(dict ID) map[string]string {
	if dict == 0 {
		return map[string]string{}
	}
	keys := dict.Send(Sel("allKeys"))
	n := int(keys.Send(Sel("count")))
	m := make(map[string]string, n)
	for i := 0; i < n; i++ {
		k := keys.Send(Sel("objectAtIndex:"), i)
		v := dict.Send(Sel("objectForKey:"), k)
		m[Stringify(k)] = Stringify(v)
	}
	return m
}

// MapToDict builds an autoreleased NSMutableDictionary of NSString->NSString
// from m.
func MapToDict(m map[string]string) ID {
	dict := ClassID("NSMutableDictionary").Send(Sel("dictionary"))
	for k, v := range m {
		dict.Send(Sel("setObject:forKey:"), NSString(v), NSString(k))
	}
	return dict
}

// ---------------------------------------------------------------------------
// Foundation run-loop runner.
// ---------------------------------------------------------------------------

// runPumpSeconds is how long each run-loop turn blocks waiting for input before
// the loop rechecks cancellation and its task queue. It bounds task latency.
const runPumpSeconds = 0.05

// Runner is the live handle to a [Run] loop. Its methods are safe to call from
// any goroutine; each marshals its work onto the run-loop thread.
type Runner struct {
	obj   ID
	tasks chan func()
}

// Object returns the runner's Objective-C instance (an instance of the class
// passed to [Run]). Observer callbacks and timers should target it. Valid only
// while [Run] is active.
func (r *Runner) Object() ID { return r.obj }

// Submit runs fn on the run-loop thread and blocks until it completes, so all
// Objective-C work stays on the one thread that owns the loop.
func (r *Runner) Submit(fn func()) {
	done := make(chan struct{})
	r.tasks <- func() { fn(); close(done) }
	<-done
}

// Run drives a Foundation run loop on the calling goroutine — which it pins to
// its OS thread — until ctx is cancelled, then returns ctx.Err(). It creates an
// instance of runnerClass (register it with [RegisterClass]), calls setup with
// the [Runner] once the instance exists (attach your observers there, using
// r.Object() and r.Submit), and services work queued through r.Submit on this
// thread. It is the mechanism behind observer-driven Foundation delivery such
// as NSDistributedNotificationCenter, which requires a running CFRunLoop.
//
// runnerClass must implement a no-op keepAlive: method (a repeating timer
// targets it to keep the loop from busy-spinning before observers attach).
// Call Run on a dedicated goroutine; it blocks for the lifetime of ctx.
func Run(ctx context.Context, runnerClass Class, setup func(r *Runner)) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	obj := ID(runnerClass).Send(Sel("alloc")).Send(Sel("init"))
	obj.Send(Sel("retain"))
	r := &Runner{obj: obj, tasks: make(chan func(), 64)}
	if setup != nil {
		setup(r)
	}

	ClassID("NSTimer").Send(Sel("scheduledTimerWithTimeInterval:target:selector:userInfo:repeats:"),
		runPumpSeconds, obj, Sel("keepAlive:"), ID(0), true)

	rl := ClassID("NSRunLoop").Send(Sel("currentRunLoop"))
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case fn := <-r.tasks:
			fn()
			continue
		default:
		}
		date := ClassID("NSDate").Send(Sel("dateWithTimeIntervalSinceNow:"), runPumpSeconds)
		rl.Send(Sel("runUntilDate:"), date)
	}
}
