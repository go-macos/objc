//go:build !darwin

package objc

import "context"

// Observe answers that it cannot: there are no notification centers off darwin,
// and a consumer that cross-compiles gets one clean error.
func Observe(context.Context, Center, ...string) (<-chan string, error) {
	return nil, ErrUnsupported
}

// Post answers that it cannot, like [Observe].
func Post(Center, string) error { return ErrUnsupported }

// RunMainLoop answers that it cannot, like [Observe].
func RunMainLoop(context.Context) error { return ErrUnsupported }
