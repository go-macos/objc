package objc

import (
	"errors"
	"testing"
)

// These tests exercise the OS-independent core (objc.go) on every GOOS by
// swapping the low-level seams to fakes, so every branch — cache miss/hit,
// name validation, the GoString marshalling states and Load's success/error
// paths — is reachable without a live Objective-C runtime.

// withResolvers swaps the sel/class resolver seams for the duration of fn.
func withResolvers(sel func(string) uintptr, cls func(string) uintptr, fn func()) {
	os, oc := resolveSel, resolveClass
	defer func() { resolveSel, resolveClass = os, oc }()
	if sel != nil {
		resolveSel = sel
	}
	if cls != nil {
		resolveClass = cls
	}
	fn()
}

func TestValidateName(t *testing.T) {
	if err := ValidateName(""); !errors.Is(err, ErrEmptyName) {
		t.Fatalf("empty name err = %v, want ErrEmptyName", err)
	}
	if err := ValidateName("has\x00nul"); !errors.Is(err, ErrNameHasNUL) {
		t.Fatalf("NUL name err = %v, want ErrNameHasNUL", err)
	}
	if err := ValidateName("drawRect:"); err != nil {
		t.Fatalf("valid name err = %v, want nil", err)
	}
}

func TestIndexNUL(t *testing.T) {
	if indexNUL("clean") {
		t.Fatal("indexNUL(clean) = true, want false")
	}
	if !indexNUL("a\x00b") {
		t.Fatal("indexNUL(a\\0b) = false, want true")
	}
}

func TestSelCacheMissAndHit(t *testing.T) {
	var calls int
	withResolvers(func(name string) uintptr {
		calls++
		return 0x1000 + uintptr(len(name))
	}, nil, func() {
		// Empty name short-circuits, resolver untouched.
		if s := Sel(""); s != 0 {
			t.Fatalf("Sel(\"\") = %v, want 0", s)
		}
		if calls != 0 {
			t.Fatalf("resolver called %d times for empty name, want 0", calls)
		}
		name := "uniqueSelForCacheTest:"
		first := Sel(name)
		if first != SEL(0x1000+uintptr(len(name))) {
			t.Fatalf("Sel first = %v", first)
		}
		second := Sel(name) // cache hit: resolver must not run again
		if second != first {
			t.Fatalf("Sel second = %v, want %v", second, first)
		}
		if calls != 1 {
			t.Fatalf("resolver called %d times, want 1 (cache hit expected)", calls)
		}
	})
}

func TestClassCacheMissAndHit(t *testing.T) {
	var calls int
	withResolvers(nil, func(name string) uintptr {
		calls++
		return 0x2000 + uintptr(len(name))
	}, func() {
		if c := GetClass(""); c != 0 {
			t.Fatalf("GetClass(\"\") = %v, want 0", c)
		}
		name := "UniqueClassForCacheTest"
		first := GetClass(name)
		if first != Class(0x2000+uintptr(len(name))) {
			t.Fatalf("GetClass first = %v", first)
		}
		if GetClass(name) != first {
			t.Fatal("GetClass second != first (cache miss)")
		}
		if calls != 1 {
			t.Fatalf("resolver called %d times, want 1", calls)
		}
		if id := ClassID(name); id != ID(first) {
			t.Fatalf("ClassID = %v, want %v", id, ID(first))
		}
	})
}

func TestRegisterNameAliasesSel(t *testing.T) {
	withResolvers(func(name string) uintptr { return 0x3000 + uintptr(len(name)) }, nil, func() {
		name := "registerNameAliasTest:"
		if RegisterName(name) != Sel(name) {
			t.Fatal("RegisterName != Sel (should be an alias)")
		}
	})
}

// withStringSeams swaps the NSString length/getCString seams for fn.
func withStringSeams(length func(ID) int, get func(ID, []byte) bool, fn func()) {
	ol, og := lengthOfBytesFn, getCStringFn
	defer func() { lengthOfBytesFn, getCStringFn = ol, og }()
	if length != nil {
		lengthOfBytesFn = length
	}
	if get != nil {
		getCStringFn = get
	}
	fn()
}

func TestGoStringNil(t *testing.T) {
	if got := GoString(0); got != "" {
		t.Fatalf("GoString(0) = %q, want empty", got)
	}
}

func TestGoStringEmptyLength(t *testing.T) {
	withStringSeams(func(ID) int { return 0 }, nil, func() {
		if got := GoString(ID(1)); got != "" {
			t.Fatalf("GoString len=0 = %q, want empty", got)
		}
	})
	withStringSeams(func(ID) int { return -1 }, nil, func() {
		if got := GoString(ID(1)); got != "" {
			t.Fatalf("GoString len<0 = %q, want empty", got)
		}
	})
}

func TestGoStringGetCStringFails(t *testing.T) {
	withStringSeams(func(ID) int { return 5 }, func(ID, []byte) bool { return false }, func() {
		if got := GoString(ID(1)); got != "" {
			t.Fatalf("GoString getCString-fail = %q, want empty", got)
		}
	})
}

func TestGoStringSuccess(t *testing.T) {
	want := "héllo" // multi-byte to prove byte-length sizing, not rune count
	withStringSeams(
		func(ID) int { return len(want) },
		func(_ ID, buf []byte) bool {
			copy(buf, want)
			buf[len(want)] = 0 // the NUL getCString writes
			return true
		}, func() {
			if got := GoString(ID(1)); got != want {
				t.Fatalf("GoString = %q, want %q", got, want)
			}
		})
}

func TestLoadSuccessAndError(t *testing.T) {
	od := dlopenFn
	defer func() { dlopenFn = od }()

	var opened []string
	dlopenFn = func(path string, mode int) (uintptr, error) {
		opened = append(opened, path)
		if mode != rtldNow|rtldGlobal {
			t.Errorf("Load mode = %#x, want %#x", mode, rtldNow|rtldGlobal)
		}
		return 1, nil
	}
	if err := Load(Foundation, AppKit); err != nil {
		t.Fatalf("Load success = %v", err)
	}
	if len(opened) != 2 || opened[0] != Foundation || opened[1] != AppKit {
		t.Fatalf("Load opened = %v", opened)
	}

	dlopenFn = func(string, int) (uintptr, error) { return 0, errors.New("no such framework") }
	err := Load(WebKit)
	if !errors.Is(err, ErrDlopen) {
		t.Fatalf("Load error = %v, want ErrDlopen", err)
	}
	if got := err.Error(); got == "" || !contains(got, "WebKit") {
		t.Fatalf("Load error %q should name the framework", got)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
