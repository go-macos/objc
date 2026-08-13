//go:build !darwin

package objc

import (
	"context"
	"errors"
	"testing"
)

// These tests run on the non-darwin lanes (linux/windows CI). They confirm the
// stub surface: the runtime entry points report unsupported / zero, while the
// convenience scopes (AutoreleasePool, Runner.Submit) still run their closure
// so consumer code stays uniform across platforms.

func TestStub_MessagingReturnsZero(t *testing.T) {
	if ID(1).Send(Sel("x")) != 0 {
		t.Fatal("Send should return 0 on non-darwin")
	}
	if Send[int](ID(1), Sel("x")) != 0 {
		t.Fatal("Send[int] should return zero on non-darwin")
	}
	if NSString("hi") != 0 {
		t.Fatal("NSString should return 0 on non-darwin")
	}
	if GoString(ID(1)) != "" {
		t.Fatal("GoString should return empty on non-darwin (stub seams)")
	}
	if Stringify(ID(1)) != "" {
		t.Fatal("Stringify should return empty on non-darwin")
	}
	if len(DictToMap(ID(1))) != 0 {
		t.Fatal("DictToMap should be empty on non-darwin")
	}
	if MapToDict(map[string]string{"a": "b"}) != 0 {
		t.Fatal("MapToDict should return 0 on non-darwin")
	}
	if App() != 0 {
		t.Fatal("App should return 0 on non-darwin")
	}
	RunApp(0) // no-op, must not panic
}

func TestStub_RegisterClassAndRunUnsupported(t *testing.T) {
	if _, err := RegisterClass("X", GetClass("NSObject"), nil); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("RegisterClass err = %v, want ErrUnsupported", err)
	}
	if GetProtocol("WKURLSchemeHandler") != nil {
		t.Fatal("GetProtocol should be nil on non-darwin")
	}
	if _, err := RegisterClassWithProtocols("X", GetClass("NSObject"), nil, nil); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("RegisterClassWithProtocols err = %v, want ErrUnsupported", err)
	}
	if err := Run(context.Background(), 0, nil); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Run err = %v, want ErrUnsupported", err)
	}
}

func TestStub_ScopesRunClosure(t *testing.T) {
	ran := false
	AutoreleasePool(func() { ran = true })
	if !ran {
		t.Fatal("AutoreleasePool should run fn on non-darwin")
	}

	r := &Runner{}
	if r.Object() != 0 {
		t.Fatal("Runner.Object should be 0 on non-darwin")
	}
	submitted := false
	r.Submit(func() { submitted = true })
	if !submitted {
		t.Fatal("Runner.Submit should run fn on non-darwin")
	}
}

func TestStub_LoadUnsupported(t *testing.T) {
	err := Load(Foundation)
	if !errors.Is(err, ErrDlopen) {
		t.Fatalf("Load err = %v, want ErrDlopen", err)
	}
}

func TestStub_DispatchMain(t *testing.T) {
	DispatchMain(nil) // no-op, must not panic.
	ran := false
	DispatchMain(func() { ran = true })
	if !ran {
		t.Fatal("DispatchMain should run fn inline on non-darwin")
	}
}
