//go:build darwin

package objc

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ebitengine/purego"
)

// These tests run on a real macOS device (the darwin CI lane and a developer
// Mac). They drive the actual Objective-C runtime through the package — real
// objc_getClass / objc_msgSend, real NSString bridging, a runtime-registered
// class with dispatched methods, and a live Foundation run loop — so the darwin
// wiring is proven, not merely compiled.

func init() { _ = Load(Foundation, AppKit) }

func TestOnDevice_ClassLookupAndMsgSend(t *testing.T) {
	// A real class lookup, cached, and a real message send returning an object.
	proc := ClassID("NSProcessInfo").Send(Sel("processInfo"))
	if proc == 0 {
		t.Fatal("NSProcessInfo processInfo returned nil")
	}
	// -processorCount returns an NSUInteger scalar via objc_msgSend.
	n := int(proc.Send(Sel("processorCount")))
	if n <= 0 {
		t.Fatalf("processorCount = %d, want > 0", n)
	}
	t.Logf("on-device: NSProcessInfo.processorCount = %d", n)

	// GetClass caches: a second lookup returns the identical class object.
	if ClassID("NSProcessInfo") == 0 || GetClass("NSProcessInfo") != GetClass("NSProcessInfo") {
		t.Fatal("class cache inconsistent")
	}
}

func TestOnDevice_NSStringRoundTrip(t *testing.T) {
	for _, s := range []string{"", "hello", "héllo — accents & em-dash", "日本語テスト"} {
		got := GoString(NSString(s))
		if got != s {
			t.Fatalf("NSString round-trip: got %q, want %q", got, s)
		}
	}
	// The nil object yields "".
	if got := GoString(0); got != "" {
		t.Fatalf("GoString(nil) = %q, want empty", got)
	}
	t.Logf("on-device: NSString<->Go round-trip verified for ASCII, accented, CJK")
}

func TestOnDevice_TypedFloatReturn(t *testing.T) {
	// -[NSString doubleValue] returns a CGFloat/double; the typed Send marshals
	// the float register correctly.
	got := Send[float64](NSString("3.5"), Sel("doubleValue"))
	if got != 3.5 {
		t.Fatalf("Send[float64] doubleValue = %v, want 3.5", got)
	}
	t.Logf("on-device: Send[float64] CGFloat return = %v", got)
}

func TestOnDevice_RegisterClassAndDispatch(t *testing.T) {
	got := make(chan int, 1)
	cls, err := RegisterClass("GoMacosObjcTestTarget", GetClass("NSObject"),
		[]MethodDef{
			{Cmd: Sel("fire:"), Fn: func(_ ID, _ SEL, sender ID) {
				got <- int(sender.Send(Sel("tag")))
			}},
		})
	if err != nil {
		t.Fatalf("RegisterClass: %v", err)
	}
	target := ID(cls).Send(Sel("alloc")).Send(Sel("init"))

	// A menu item carries an integer tag; sending fire: with it as sender must
	// dispatch into the Go closure and hand back the tag.
	item := ClassID("NSMenuItem").Send(Sel("alloc")).
		Send(Sel("initWithTitle:action:keyEquivalent:"), NSString("x"), Sel("fire:"), NSString(""))
	item.Send(Sel("setTag:"), 42)
	target.Send(Sel("fire:"), item)

	select {
	case tag := <-got:
		if tag != 42 {
			t.Fatalf("dispatched tag = %d, want 42", tag)
		}
		t.Logf("on-device: RegisterClass method dispatch delivered tag %d", tag)
	case <-time.After(2 * time.Second):
		t.Fatal("registered method was not dispatched")
	}
}

func TestOnDevice_App(t *testing.T) {
	// App resolves the shared NSApplication. (RunApp is intentionally not
	// exercised: [NSApp run] enters a run loop that never returns without a
	// terminating UI event, so it cannot be driven from a unit test.)
	if App() == 0 {
		t.Fatal("App() returned nil NSApplication")
	}
	t.Log("on-device: App() resolved the shared NSApplication")
}

func TestOnDevice_RegisterClassWithProtocols(t *testing.T) {
	// The regression this guards: WKWebView's -setURLSchemeHandler:forURLScheme:
	// checks conformsToProtocol:(WKURLSchemeHandler), so a scheme-handler class
	// must formally declare the protocol, not merely implement its methods.
	if err := Load(WebKit); err != nil {
		t.Skipf("WebKit unavailable: %v", err)
	}
	proto := GetProtocol("WKURLSchemeHandler")
	if proto == nil {
		t.Fatal("GetProtocol(WKURLSchemeHandler) = nil on a device with WebKit")
	}
	cls, err := RegisterClassWithProtocols("GoMacosObjcTestSchemeHandler", GetClass("NSObject"),
		[]*Protocol{proto},
		[]MethodDef{
			{Cmd: Sel("webView:startURLSchemeTask:"), Fn: func(_ ID, _ SEL, _ ID, _ ID) {}},
			{Cmd: Sel("webView:stopURLSchemeTask:"), Fn: func(_ ID, _ SEL, _ ID, _ ID) {}},
		})
	if err != nil {
		t.Fatalf("RegisterClassWithProtocols: %v", err)
	}
	inst := ID(cls).Send(Sel("alloc")).Send(Sel("init"))
	// The exact conformance check WKWebView performs. purego passes the *Protocol
	// pointer straight through as the argument.
	if inst.Send(Sel("conformsToProtocol:"), proto) == 0 {
		t.Fatal("registered class does not conformsToProtocol: WKURLSchemeHandler")
	}
	// A class registered WITHOUT the protocol must NOT conform — proving the
	// declaration is what carries conformance.
	plain, err := RegisterClass("GoMacosObjcTestPlain", GetClass("NSObject"), nil)
	if err != nil {
		t.Fatalf("RegisterClass plain: %v", err)
	}
	if ID(plain).Send(Sel("alloc")).Send(Sel("init")).Send(Sel("conformsToProtocol:"), proto) != 0 {
		t.Fatal("plain class unexpectedly conforms to WKURLSchemeHandler")
	}
	t.Log("on-device: RegisterClassWithProtocols yields a class conforming to WKURLSchemeHandler; plain class does not")
}

func TestOnDevice_AutoreleasePool(t *testing.T) {
	ran := false
	AutoreleasePool(func() {
		ran = true
		// Allocate an autoreleased object inside the pool.
		_ = NSString("pooled")
	})
	if !ran {
		t.Fatal("AutoreleasePool did not run fn")
	}
	t.Log("on-device: AutoreleasePool scope executed and drained")
}

func TestOnDevice_DictRoundTripAndStringify(t *testing.T) {
	in := map[string]string{"alpha": "one", "beta": "two"}
	out := DictToMap(MapToDict(in))
	if len(out) != len(in) {
		t.Fatalf("dict round-trip size = %d, want %d (%v)", len(out), len(in), out)
	}
	for k, v := range in {
		if out[k] != v {
			t.Fatalf("dict[%q] = %q, want %q", k, out[k], v)
		}
	}
	// Stringify of the nil object, an NSString, and a non-string (NSNumber via
	// -description) object.
	if Stringify(0) != "" {
		t.Fatal("Stringify(nil) != empty")
	}
	if Stringify(NSString("hi")) != "hi" {
		t.Fatal("Stringify(NSString) != hi")
	}
	num := ClassID("NSNumber").Send(Sel("numberWithInt:"), 7)
	if Stringify(num) != "7" {
		t.Fatalf("Stringify(NSNumber 7) = %q, want 7", Stringify(num))
	}
	// DictToMap of the nil dictionary is empty.
	if len(DictToMap(0)) != 0 {
		t.Fatal("DictToMap(nil) not empty")
	}
	t.Logf("on-device: NSDictionary<->map round-trip and Stringify verified")
}

func TestOnDevice_RunLoopAndSubmit(t *testing.T) {
	cls, err := RegisterClass("GoMacosObjcTestRunner", GetClass("NSObject"),
		[]MethodDef{
			{Cmd: Sel("keepAlive:"), Fn: func(_ ID, _ SEL, _ ID) {}},
		})
	if err != nil {
		t.Fatalf("RegisterClass runner: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	var runner *Runner
	ready := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, cls, func(r *Runner) {
			runner = r
			if r.Object() == 0 {
				t.Error("runner Object() is nil")
			}
			close(ready)
		})
	}()

	<-ready
	// Submit work onto the run-loop thread and confirm it executes there.
	ran := make(chan struct{}, 1)
	runner.Submit(func() { ran <- struct{}{} })
	select {
	case <-ran:
	case <-time.After(2 * time.Second):
		t.Fatal("Submit task did not run on the run loop")
	}

	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run returned %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after ctx cancel")
	}
	t.Log("on-device: Foundation run loop serviced a submitted task and stopped on cancel")
}

func TestOnDevice_LoadRealFramework(t *testing.T) {
	if err := Load(Foundation); err != nil {
		t.Fatalf("Load(Foundation) = %v, want nil", err)
	}
	// A bogus path must fail through the real dlopen.
	if err := Load("/no/such/framework.dylib"); !errors.Is(err, ErrDlopen) {
		t.Fatalf("Load(bogus) = %v, want ErrDlopen", err)
	}
	t.Log("on-device: Load opened Foundation and rejected a bogus path")
}

func TestOnDevice_DispatchMainLoadsLibdispatch(t *testing.T) {
	// Drive loadDispatch directly against the real libSystem: it must resolve
	// dispatch_async and &_dispatch_main_q on this device, and a real
	// libdispatch block must be creatable for a function.
	savedQ, savedErr := dispatchMainQ, dispatchLoadErr
	defer func() { dispatchMainQ, dispatchLoadErr = savedQ, savedErr }()
	dispatchMainQ, dispatchLoadErr = 0, nil
	loadDispatch()
	if dispatchLoadErr != nil {
		t.Fatalf("loadDispatch on-device: %v", dispatchLoadErr)
	}
	if dispatchMainQ == 0 {
		t.Fatal("loadDispatch resolved a nil main queue")
	}
	if mkBlock(func() {}) == 0 {
		t.Fatal("mkBlock returned a nil block")
	}
	t.Logf("on-device: libdispatch resolved, main queue = %#x", dispatchMainQ)
}

func TestOnDevice_DispatchMainScheduledHop(t *testing.T) {
	// Fire dispatchOnce via a real DispatchMain, then cover the scheduled branch
	// deterministically: a fake dispatch_async "delivers" the block by running
	// its captured body, so no serviced main queue is required.
	DispatchMain(func() {})
	if dispatchLoadErr != nil || dispatchMainQ == 0 {
		t.Fatalf("real load failed: err=%v q=%#x", dispatchLoadErr, dispatchMainQ)
	}
	savedAsync, savedBlock := dispatchAsyncFn, mkBlock
	defer func() { dispatchAsyncFn, mkBlock = savedAsync, savedBlock }()

	const sentinel uintptr = 0xB10C
	var captured func()
	var gotQ, gotBlock uintptr
	mkBlock = func(fn func()) uintptr { captured = fn; return sentinel }
	dispatchAsyncFn = func(q, block uintptr) {
		gotQ, gotBlock = q, block
		if block == sentinel && captured != nil {
			captured()
		}
	}

	ran := false
	DispatchMain(func() { ran = true })
	if gotQ != dispatchMainQ {
		t.Fatalf("dispatch_async queue = %#x, want %#x", gotQ, dispatchMainQ)
	}
	if gotBlock != sentinel {
		t.Fatalf("dispatch_async block = %#x, want %#x", gotBlock, sentinel)
	}
	if !ran {
		t.Fatal("DispatchMain did not run the scheduled fn")
	}
	t.Log("on-device: DispatchMain scheduled fn onto the main queue via dispatch_async")
}

func TestOnDevice_DispatchMainNilAndFallback(t *testing.T) {
	DispatchMain(nil) // nil fn is a no-op, must not panic.

	DispatchMain(func() {}) // ensure dispatchOnce has fired.
	savedQ, savedErr := dispatchMainQ, dispatchLoadErr
	defer func() { dispatchMainQ, dispatchLoadErr = savedQ, savedErr }()

	// Fallback with an unresolved queue: fn runs inline.
	dispatchMainQ, dispatchLoadErr = 0, nil
	ran := false
	DispatchMain(func() { ran = true })
	if !ran {
		t.Fatal("DispatchMain (nil queue) did not run fn inline")
	}

	// Fallback with a load error: fn runs inline.
	dispatchMainQ, dispatchLoadErr = 0x1, errors.New("boom")
	ran = false
	DispatchMain(func() { ran = true })
	if !ran {
		t.Fatal("DispatchMain (load error) did not run fn inline")
	}
}

func TestDispatchLoadBranches(t *testing.T) {
	savedOpen, savedSym, savedReg := ldOpen, ldSym, ldReg
	savedQ, savedErr := dispatchMainQ, dispatchLoadErr
	defer func() {
		ldOpen, ldSym, ldReg = savedOpen, savedSym, savedReg
		dispatchMainQ, dispatchLoadErr = savedQ, savedErr
	}()
	ldReg = func(uintptr) {} // never touch the real symbol table under fakes.

	// dlopen failure.
	ldOpen = func() (uintptr, error) { return 0, errors.New("no libSystem") }
	dispatchMainQ, dispatchLoadErr = 0, nil
	loadDispatch()
	if dispatchLoadErr == nil {
		t.Fatal("expected a load error on dlopen failure")
	}

	// dlsym failure.
	ldOpen = func() (uintptr, error) { return 1, nil }
	ldSym = func(uintptr) (uintptr, error) { return 0, errors.New("no symbol") }
	dispatchMainQ, dispatchLoadErr = 0, nil
	loadDispatch()
	if dispatchLoadErr == nil {
		t.Fatal("expected a load error on dlsym failure")
	}

	// dlsym returns a nil queue with no error (defensive q==0 branch).
	ldSym = func(uintptr) (uintptr, error) { return 0, nil }
	dispatchMainQ, dispatchLoadErr = 0, nil
	loadDispatch()
	if dispatchLoadErr == nil {
		t.Fatal("expected a load error on a nil main queue")
	}

	// success with fakes.
	ldSym = func(uintptr) (uintptr, error) { return 0x2000, nil }
	dispatchMainQ, dispatchLoadErr = 0, nil
	loadDispatch()
	if dispatchLoadErr != nil || dispatchMainQ != 0x2000 {
		t.Fatalf("expected success, got err=%v q=%#x", dispatchLoadErr, dispatchMainQ)
	}
}

func TestOnDevice_DispatchBlockInvokes(t *testing.T) {
	// Prove mkBlock's block actually runs its Go body under libdispatch. A global
	// concurrent queue with dispatch_sync executes the block synchronously, so no
	// serviced main run loop is needed (which a unit-test process lacks).
	h, err := purego.Dlopen(LibSystem, purego.RTLD_LAZY|purego.RTLD_GLOBAL)
	if err != nil {
		t.Fatalf("dlopen libSystem: %v", err)
	}
	var dispatchSync func(queue, block uintptr)
	purego.RegisterLibFunc(&dispatchSync, h, "dispatch_sync")
	var getGlobalQueue func(identity int, flags uintptr) uintptr
	purego.RegisterLibFunc(&getGlobalQueue, h, "dispatch_get_global_queue")

	q := getGlobalQueue(0, 0) // DISPATCH_QUEUE_PRIORITY_DEFAULT
	if q == 0 {
		t.Fatal("dispatch_get_global_queue returned nil")
	}
	ran := make(chan struct{}, 1)
	dispatchSync(q, mkBlock(func() { ran <- struct{}{} }))
	select {
	case <-ran:
	case <-time.After(2 * time.Second):
		t.Fatal("libdispatch did not invoke the block body")
	}
	t.Log("on-device: mkBlock's block body executed under libdispatch")
}

func TestOnDevice_BlockRoundTripsThroughNSArray(t *testing.T) {
	// The real proof that the block ABI is right: hand a Go closure to a genuine
	// Cocoa API — -[NSArray enumerateObjectsUsingBlock:], whose block type is
	// void (^)(id obj, NSUInteger idx, BOOL *stop) — and check it was invoked
	// once per element with the objects and indices Foundation passed in. A
	// wrong block layout, signature encoding or argument order fails this test
	// (or crashes), which is the point.
	want := []string{"alpha", "beta", "gamma", "délta"}
	arr := ClassID("NSMutableArray").Send(Sel("array"))
	if arr == 0 {
		t.Fatal("NSMutableArray array returned nil")
	}
	for _, s := range want {
		arr.Send(Sel("addObject:"), NSString(s))
	}
	if n := int(arr.Send(Sel("count"))); n != len(want) {
		t.Fatalf("array count = %d, want %d", n, len(want))
	}

	var gotStrings []string
	var gotIndices []int
	var gotSelf []Block
	block := NewBlock(func(b Block, obj ID, idx uint, stop *bool) {
		gotSelf = append(gotSelf, b)
		gotStrings = append(gotStrings, Stringify(obj))
		gotIndices = append(gotIndices, int(idx))
		_ = stop
	})
	defer block.Release()
	if block == 0 {
		t.Fatal("NewBlock returned the nil block")
	}

	arr.Send(Sel("enumerateObjectsUsingBlock:"), block)

	if len(gotStrings) != len(want) {
		t.Fatalf("block ran %d times, want %d (%q)", len(gotStrings), len(want), gotStrings)
	}
	for i, s := range want {
		if gotStrings[i] != s {
			t.Fatalf("block arg %d = %q, want %q", i, gotStrings[i], s)
		}
		if gotIndices[i] != i {
			t.Fatalf("block index %d = %d, want %d", i, gotIndices[i], i)
		}
		if gotSelf[i] != block {
			t.Fatalf("block self pointer %d = %#x, want %#x", i, uintptr(gotSelf[i]), uintptr(block))
		}
	}
	t.Logf("on-device: NSArray enumerateObjectsUsingBlock: invoked the Go block %d times with %q", len(gotStrings), gotStrings)
}

func TestOnDevice_BlockStopsEnumeration(t *testing.T) {
	// The third block argument is a BOOL* the callee writes to abort the
	// enumeration. Writing through it from Go proves the pointer argument
	// really is Foundation's stop flag, not a stray register.
	arr := ClassID("NSMutableArray").Send(Sel("array"))
	for _, s := range []string{"one", "two", "three", "four"} {
		arr.Send(Sel("addObject:"), NSString(s))
	}
	calls := 0
	block := NewBlock(func(_ Block, _ ID, _ uint, stop *bool) {
		calls++
		*stop = true
	})
	defer block.Release()

	arr.Send(Sel("enumerateObjectsUsingBlock:"), block)
	if calls != 1 {
		t.Fatalf("block ran %d times after setting *stop, want 1", calls)
	}
	t.Log("on-device: writing *stop aborted enumerateObjectsUsingBlock: after one element")
}

func TestOnDevice_BlockCopyAndRelease(t *testing.T) {
	// Copy yields a block that is still callable (the copy shares the cached Go
	// function), and each Copy owes a Release.
	arr := ClassID("NSMutableArray").Send(Sel("array"))
	arr.Send(Sel("addObject:"), NSString("solo"))

	var seen string
	block := NewBlock(func(_ Block, obj ID, _ uint, _ *bool) { seen = Stringify(obj) })
	copied := block.Copy()
	if copied == 0 {
		t.Fatal("Block.Copy returned the nil block")
	}
	arr.Send(Sel("enumerateObjectsUsingBlock:"), copied)
	if seen != "solo" {
		t.Fatalf("copied block delivered %q, want %q", seen, "solo")
	}
	copied.Release()
	block.Release()
	t.Log("on-device: a copied block invoked the same Go closure; both references released")
}

func TestOnDevice_NewBlockRejectsMissingBlockParameter(t *testing.T) {
	// The documented contract: the Go function's first parameter must be a
	// Block. purego panics otherwise, so consumers get a loud failure rather
	// than a shifted-by-one argument list at runtime.
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("NewBlock(func(ID)) did not panic")
		}
		t.Logf("on-device: NewBlock rejected a non-Block first parameter: %v", r)
	}()
	_ = NewBlock(func(ID) {})
}
