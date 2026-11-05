//go:build darwin

package objc

import (
	"testing"
	"unsafe"
)

// The Msg functions answer exactly what Send answers, for an object, a count
// and a BOOL.
func TestMsgMatchesSend(t *testing.T) {
	AutoreleasePool(func() {
		s := NSString("héllo")
		if got, want := ID(Msg0(s, Sel("self"))), s.Send(Sel("self")); got != want {
			t.Fatalf("self: Msg0 %#x, Send %#x", got, want)
		}
		if got, want := Msg0(s, Sel("length")), uintptr(s.Send(Sel("length"))); got != want || got != 5 {
			t.Fatalf("length: Msg0 %d, Send %d, want 5", got, want)
		}
		isStr := Msg1(s, Sel("isKindOfClass:"), uintptr(ClassID("NSString")))
		isNum := Msg1(s, Sel("isKindOfClass:"), uintptr(ClassID("NSNumber")))
		if isStr&0xff != 1 || isNum&0xff != 0 {
			t.Fatalf("isKindOfClass: NSString %d, NSNumber %d", isStr, isNum)
		}
		// -[NSString compare:options:] → NSOrderedSame (0) for equal strings
		// compared case-insensitively (NSCaseInsensitiveSearch = 1).
		if got := Msg2(s, Sel("compare:options:"), uintptr(NSString("HÉLLO")), 1); got != 0 {
			t.Fatalf("compare:options: %d, want NSOrderedSame", got)
		}
	})
}

// A pointer argument reaches the method: getCString fills a Go buffer.
func TestMsgPointerArgument(t *testing.T) {
	AutoreleasePool(func() {
		s := NSString("abc")
		buf := make([]byte, 8)
		ok := Msg3(s, Sel("getCString:maxLength:encoding:"),
			uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), nsUTF8Encoding)
		if ok&0xff != 1 || string(buf[:3]) != "abc" || buf[3] != 0 {
			t.Fatalf("getCString: ok %d, buffer %q", ok, buf)
		}
	})
}

// The point of the Msg functions: one allocation where Send costs five or more.
func TestMsgAllocations(t *testing.T) {
	if raceEnabled {
		t.Skip("the race detector adds allocations of its own")
	}
	o := ClassID("NSObject")
	self, responds := Sel("self"), Sel("respondsToSelector:")
	cases := []struct {
		name string
		fn   func()
	}{
		{"Msg0", func() { Msg0(o, self) }},
		{"Msg1", func() { Msg1(o, responds, uintptr(self)) }},
		{"Msg2", func() { Msg2(o, self, 1, 2) }},
		{"Msg3", func() { Msg3(o, self, 1, 2, 3) }},
	}
	for _, c := range cases {
		if n := testing.AllocsPerRun(1000, c.fn); n > 1 {
			t.Errorf("%s allocates %v times per message, want at most 1", c.name, n)
		}
	}
	t.Logf("Send with one argument: %v allocations", testing.AllocsPerRun(1000, func() { o.Send(responds, self) }))
}

func BenchmarkMsg0(b *testing.B) {
	o, self := ClassID("NSObject"), Sel("self")
	b.ReportAllocs()
	for b.Loop() {
		Msg0(o, self)
	}
}

func BenchmarkSend(b *testing.B) {
	o, self := ClassID("NSObject"), Sel("self")
	b.ReportAllocs()
	for b.Loop() {
		o.Send(self)
	}
}
