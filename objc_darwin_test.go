//go:build darwin

package objc

import (
	"context"
	"errors"
	"testing"
	"time"
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
