//go:build darwin

package objc

import (
	"fmt"
	"sync"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

// libdispatch main-thread hop.
//
// dispatch_async and the main dispatch queue (libdispatch's
// dispatch_get_main_queue()) are resolved lazily from libSystem on the first
// [DispatchMain]. The FFI leaves are package vars so tests drive every branch —
// the load-failure fallback and the scheduled hop — without a serviced main
// queue (a unit-test process has no running main run loop to drain it).
var (
	dispatchOnce    sync.Once
	dispatchAsyncFn func(queue, block uintptr)
	dispatchMainQ   uintptr
	dispatchLoadErr error

	// ldOpen opens libSystem, which exports libdispatch.
	ldOpen = func() (uintptr, error) {
		return purego.Dlopen(LibSystem, purego.RTLD_LAZY|purego.RTLD_GLOBAL)
	}
	// ldReg binds dispatch_async into dispatchAsyncFn.
	ldReg = func(h uintptr) { purego.RegisterLibFunc(&dispatchAsyncFn, h, "dispatch_async") }
	// ldSym resolves &_dispatch_main_q — the value dispatch_get_main_queue()
	// returns (a global data symbol, not a function, so it is read by address).
	ldSym = func(h uintptr) (uintptr, error) { return purego.Dlsym(h, "_dispatch_main_q") }
	// mkBlock wraps fn in a libdispatch-callable Objective-C block.
	mkBlock = func(fn func()) uintptr {
		return uintptr(objc.NewBlock(func(objc.Block) { fn() }))
	}
)

// loadDispatch resolves dispatch_async and the main queue from libSystem,
// recording the first failure in dispatchLoadErr.
func loadDispatch() {
	h, err := ldOpen()
	if err != nil {
		dispatchLoadErr = fmt.Errorf("objc: load libSystem: %w", err)
		return
	}
	ldReg(h)
	q, err := ldSym(h)
	if err != nil || q == 0 {
		dispatchLoadErr = fmt.Errorf("objc: _dispatch_main_q unavailable: %v", err)
		return
	}
	dispatchMainQ = q
}

// DispatchMain schedules fn to run asynchronously on the main dispatch queue —
// the thread that owns the AppKit event loop. Use it to marshal AppKit / UI work
// back onto the main thread from a background goroutine, for example replying to
// a WKURLSchemeTask after serving its request off-thread (the WKURLSchemeTask
// methods must be messaged on the main thread). A nil fn is a no-op. If
// libdispatch cannot be resolved, fn runs inline on the calling goroutine rather
// than being dropped.
func DispatchMain(fn func()) {
	if fn == nil {
		return
	}
	dispatchOnce.Do(loadDispatch)
	if dispatchLoadErr != nil || dispatchMainQ == 0 {
		fn()
		return
	}
	dispatchAsyncFn(dispatchMainQ, mkBlock(fn))
}
