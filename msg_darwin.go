//go:build darwin

package objc

import (
	"sync"

	"github.com/ebitengine/purego"
)

// msgSendAddr is objc_msgSend's address, resolved the first time a Msg
// function runs.
var (
	msgSendOnce sync.Once
	msgSendAddr uintptr
)

func msgSend() uintptr {
	msgSendOnce.Do(func() {
		h, err := purego.Dlopen(LibObjC, purego.RTLD_LAZY|purego.RTLD_GLOBAL)
		if err != nil {
			panic("objc: " + err.Error())
		}
		a, err := purego.Dlsym(h, "objc_msgSend")
		if err != nil {
			panic("objc: " + err.Error())
		}
		msgSendAddr = a
	})
	return msgSendAddr
}

// Msg0 sends sel to id with no argument and returns the result as an integer:
// an object, a BOOL, an NSUInteger, a pointer. Msg1, Msg2 and Msg3 take one,
// two and three arguments.
//
// They exist for the hot path. [ID.Send] takes its arguments as `any` and calls
// objc_msgSend through reflection, which costs five allocations with no
// argument and seven with one (measured 2026-11-05, purego v0.11.3). These go
// straight to purego.SyscallN and cost one, its own argument slice. They are
// fixed-arity on purpose: a variadic version must be //go:uintptrescapes to
// keep pointer arguments alive, and that sends the caller's slice to the heap
// too, doubling the cost.
//
// Every argument must fit an integer register. A pointer is passed as
// uintptr(unsafe.Pointer(p)) written IN the call, which keeps p alive until the
// call returns. Floating-point arguments, by-value structs and struct returns
// are not supported: use [Send] for those.
func Msg0(id ID, sel SEL) uintptr {
	r, _, _ := purego.SyscallN(msgSend(), uintptr(id), uintptr(sel))
	return r
}

// Msg1 is [Msg0] with one argument.
//
//go:uintptrescapes
func Msg1(id ID, sel SEL, a uintptr) uintptr {
	r, _, _ := purego.SyscallN(msgSend(), uintptr(id), uintptr(sel), a)
	return r
}

// Msg2 is [Msg0] with two arguments.
//
//go:uintptrescapes
func Msg2(id ID, sel SEL, a, b uintptr) uintptr {
	r, _, _ := purego.SyscallN(msgSend(), uintptr(id), uintptr(sel), a, b)
	return r
}

// Msg3 is [Msg0] with three arguments.
//
//go:uintptrescapes
func Msg3(id ID, sel SEL, a, b, c uintptr) uintptr {
	r, _, _ := purego.SyscallN(msgSend(), uintptr(id), uintptr(sel), a, b, c)
	return r
}
