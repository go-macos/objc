//go:build darwin

package objc

import (
	"context"
	"sync"
)

// observeSeconds is how long one turn of [RunMainLoop] lasts, and so the
// longest a cancelled context goes unnoticed. A notification does NOT wait for
// it: the loop is woken the moment one arrives.
const observeSeconds = 1.0

// loadFrameworks is a seam: the tests make the load fail.
var loadFrameworks = func() error { return Load(Foundation, AppKit) }

// isMainThread is a seam: the tests answer for a thread they do not own.
var isMainThread = func() bool {
	return Send[bool](ClassID("NSThread"), Sel("isMainThread"))
}

// centerFor returns the notification center, or 0 when there is none.
func centerFor(c Center) ID {
	switch c {
	case DefaultCenter:
		return ClassID("NSNotificationCenter").Send(Sel("defaultCenter"))
	case DistributedCenter:
		return ClassID("NSDistributedNotificationCenter").Send(Sel("defaultCenter"))
	case WorkspaceCenter:
		return ClassID("NSWorkspace").Send(Sel("sharedWorkspace")).Send(Sel("notificationCenter"))
	}
	return 0
}

// sink hands names to a channel that is closed exactly once, however late a
// block arrives from another thread.
type sink struct {
	mu     sync.Mutex
	ch     chan string
	closed bool
}

// put delivers name, or drops it when the reader is eight behind.
func (s *sink) put(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	select {
	case s.ch <- name:
	default:
	}
}

func (s *sink) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	close(s.ch)
}

// Post posts a notification called name, with no object and no payload, on
// center c. On [DistributedCenter] it reaches every process of the login
// session, this one included, which makes it the way to prove that [Observe]
// works without waiting for the system to produce a real event.
func Post(c Center, name string) error {
	if err := loadFrameworks(); err != nil {
		return err
	}
	center := centerFor(c)
	if center == 0 {
		return ErrNoCenter
	}
	if c == DistributedCenter {
		center.Send(Sel("postNotificationName:object:userInfo:deliverImmediately:"), NSString(name), ID(0), ID(0), true)
	} else {
		center.Send(Sel("postNotificationName:object:"), NSString(name), ID(0))
	}
	return nil
}

// Observe delivers the name of each notification in names, posted on center c,
// until ctx ends, and then closes the channel. It returns an error, and
// registers nothing, when the observers cannot be registered.
//
// ⛔ DISTRIBUTED AND WORKSPACE NOTIFICATIONS ARE DELIVERED ON THE MAIN THREAD'S
// RUN LOOP, and on no other: measured here, an observer registered and a loop
// pumped on a goroutine's own thread heard nothing, while the same code pumped
// on the main thread heard the notification. Observe therefore does not run a
// loop of its own; the program must run [RunMainLoop] on the main thread (which
// a Go program has only if its main goroutine was locked to it in an init
// function) for as long as it wants to hear. Nothing in Observe can tell that
// nobody is listening: a caller that must know posts a notification of its own
// name with [Post] and waits for it.
//
// A reader more than eight names behind loses the newest ones.
func Observe(ctx context.Context, c Center, names ...string) (<-chan string, error) {
	if len(names) == 0 {
		return nil, ErrNoNames
	}
	if err := loadFrameworks(); err != nil {
		return nil, err
	}
	center := centerFor(c)
	if center == 0 {
		return nil, ErrNoCenter
	}
	s := &sink{ch: make(chan string, 8)}
	var (
		tokens []ID
		blocks []Block
	)
	for _, n := range names {
		blk := NewBlock(func(_ Block, note ID) {
			s.put(GoString(note.Send(Sel("name"))))
		})
		tok := center.Send(Sel("addObserverForName:object:queue:usingBlock:"), NSString(n), ID(0), ID(0), blk)
		tok.Send(Sel("retain"))
		tokens = append(tokens, tok)
		blocks = append(blocks, blk)
	}
	go func() {
		<-ctx.Done()
		for _, tok := range tokens {
			center.Send(Sel("removeObserver:"), tok)
			tok.Send(Sel("release"))
		}
		for _, b := range blocks {
			b.Release()
		}
		s.close()
	}()
	return s.ch, nil
}

// RunMainLoop runs the main thread's run loop until ctx ends, so that the
// notifications [Observe] asked for are delivered. It must be called on the
// process's main OS thread, and returns [ErrNotMainThread] otherwise; a Go
// program gets that thread by calling runtime.LockOSThread in an init function
// and running the rest of its work on other goroutines.
//
// The loop sleeps in the kernel between notifications; once a second it looks at
// ctx.
func RunMainLoop(ctx context.Context) error {
	if err := loadFrameworks(); err != nil {
		return err
	}
	if !isMainThread() {
		return ErrNotMainThread
	}
	AutoreleasePool(func() {
		// A run loop with nothing attached returns at once; a repeating timer is
		// what makes it wait for the next turn.
		tick := NewBlock(func(Block, ID) {})
		timer := ClassID("NSTimer").Send(Sel("scheduledTimerWithTimeInterval:repeats:block:"),
			observeSeconds, true, tick)
		rl := ClassID("NSRunLoop").Send(Sel("currentRunLoop"))
		for ctx.Err() == nil {
			until := ClassID("NSDate").Send(Sel("dateWithTimeIntervalSinceNow:"), observeSeconds)
			rl.Send(Sel("runUntilDate:"), until)
		}
		timer.Send(Sel("invalidate"))
		tick.Release()
	})
	return nil
}
