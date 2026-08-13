//go:build !darwin

package objc

import "context"

// On non-darwin platforms there is no Objective-C runtime, so the public types
// are plain uintptr-width types (mirroring the width and zero-is-nil semantics
// of the purego/objc aliases used on darwin) and the runtime entry points below
// are stubs. The OS-independent core in objc.go — the selector/class caches,
// name validation, GoString and Load — stays fully functional and testable
// here through the seams, which keep their unsupported defaults.

type (
	// ID is an Objective-C object pointer; the zero value is nil.
	ID uintptr
	// SEL is an Objective-C selector.
	SEL uintptr
	// Class is an Objective-C class object.
	Class uintptr
	// IMP is an Objective-C method implementation pointer.
	IMP uintptr
)

// MethodDef binds a selector to a Go closure for [RegisterClass]. On darwin
// this is an alias of purego's objc.MethodDef; here it is the equivalent shape.
type MethodDef struct {
	Cmd SEL
	Fn  any
}

// Protocol is an Objective-C protocol. On darwin it is an alias of purego's
// objc.Protocol; here it is a placeholder so the signatures compile.
type Protocol struct{}

// Send reports the zero [ID] on non-darwin platforms (there is no runtime to
// message). On darwin the equivalent method is inherited from purego's objc.ID.
func (id ID) Send(sel SEL, args ...any) ID { return 0 }

// Send returns the zero value of T on non-darwin platforms.
func Send[T any](id ID, sel SEL, args ...any) T {
	var zero T
	return zero
}

// NSString reports the zero [ID] on non-darwin platforms.
func NSString(s string) ID { return 0 }

// RegisterClass reports [ErrUnsupported] on non-darwin platforms.
func RegisterClass(name string, super Class, methods []MethodDef) (Class, error) {
	return 0, ErrUnsupported
}

// GetProtocol reports a nil protocol on non-darwin platforms.
func GetProtocol(name string) *Protocol { return nil }

// RegisterClassWithProtocols reports [ErrUnsupported] on non-darwin platforms.
func RegisterClassWithProtocols(name string, super Class, protocols []*Protocol, methods []MethodDef) (Class, error) {
	return 0, ErrUnsupported
}

// AutoreleasePool runs fn directly on non-darwin platforms (there is no pool to
// establish), so callers can wrap code unconditionally.
func AutoreleasePool(fn func()) { fn() }

// Stringify reports "" on non-darwin platforms.
func Stringify(v ID) string { return "" }

// DictToMap reports an empty map on non-darwin platforms.
func DictToMap(dict ID) map[string]string { return map[string]string{} }

// MapToDict reports the zero [ID] on non-darwin platforms.
func MapToDict(m map[string]string) ID { return 0 }

// App reports the zero [ID] on non-darwin platforms.
func App() ID { return 0 }

// RunApp is a no-op on non-darwin platforms.
func RunApp(policy int) {}

// DispatchMain runs fn inline on the calling goroutine on non-darwin platforms
// (there is no libdispatch main queue). A nil fn is a no-op.
func DispatchMain(fn func()) {
	if fn != nil {
		fn()
	}
}

// Runner is the non-darwin stand-in for the [Run] handle. Its methods are
// no-ops (Submit runs fn directly) so consumer code that references them still
// compiles.
type Runner struct {
	obj   ID
	tasks chan func()
}

// Object reports the zero [ID] on non-darwin platforms.
func (r *Runner) Object() ID { return r.obj }

// Submit runs fn directly on non-darwin platforms.
func (r *Runner) Submit(fn func()) { fn() }

// Run reports [ErrUnsupported] on non-darwin platforms.
func Run(ctx context.Context, runnerClass Class, setup func(r *Runner)) error {
	return ErrUnsupported
}
