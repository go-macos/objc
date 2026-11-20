//go:build darwin

package objc

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"
)

// serveMain runs the main thread's run loop for the test, and returns the
// function that ends it and waits for it to have ended.
func serveMain(t *testing.T) (stop func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go onMainThread(func() { done <- RunMainLoop(ctx) })
	return func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("RunMainLoop: %v", err)
		}
	}
}

func recvName(t *testing.T, ch <-chan string) string {
	t.Helper()
	select {
	case n, ok := <-ch:
		if !ok {
			t.Fatal("channel closed before a notification")
		}
		return n
	case <-time.After(5 * time.Second):
		t.Fatal("no notification within 5 s")
	}
	return ""
}

// TestObserveDistributedRealRoundTrip posts a distributed notification of its
// own name, through the same Observe and RunMainLoop a lock watcher uses, and
// measures how long it takes to arrive: the proof, with no screen lock, that an
// observer registered from an ordinary goroutine is heard.
func TestObserveDistributedRealRoundTrip(t *testing.T) {
	stop := serveMain(t)
	ctx, cancel := context.WithCancel(context.Background())
	name := fmt.Sprintf("org.go-macos.objc.test.%d.%d", os.Getpid(), time.Now().UnixNano())
	ch, err := Observe(ctx, DistributedCenter, name, name+".other")
	if err != nil {
		t.Fatal(err)
	}
	// Registration with the distributed notification daemon is asynchronous, so
	// the first posts may precede it: post until one is heard.
	start := time.Now()
	var got string
	for tries := 0; got == "" && tries < 50; tries++ {
		if err := Post(DistributedCenter, name); err != nil {
			t.Fatal(err)
		}
		select {
		case got = <-ch:
		case <-time.After(100 * time.Millisecond):
		}
	}
	if got != name {
		t.Fatalf("got %q want %q", got, name)
	}
	t.Logf("distributed notification delivered after %v (including registration)", time.Since(start))
	start = time.Now()
	if err := Post(DistributedCenter, name+".other"); err != nil {
		t.Fatal(err)
	}
	if got := recvName(t, ch); got != name+".other" {
		t.Fatalf("got %q", got)
	}
	t.Logf("second distributed notification delivered in %v", time.Since(start))
	cancel()
	for range ch { // closed once the context ended
	}
	stop()
}

func TestObserveDefaultCenterDropsWhenReaderIsBehind(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	name := fmt.Sprintf("org.go-macos.objc.local.%d", os.Getpid())
	ch, err := Observe(ctx, DefaultCenter, name)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		if err := Post(DefaultCenter, name); err != nil {
			t.Fatal(err)
		}
	}
	if n := len(ch); n != 8 {
		t.Fatalf("buffered %d, want 8 (the rest dropped)", n)
	}
	cancel()
	for range ch {
	}
	// a block that arrives after the close is ignored, not a panic
	s := &sink{ch: make(chan string, 1)}
	s.close()
	s.put("late")
}

func TestObserveWorkspaceCenterRegisters(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := Observe(ctx, WorkspaceCenter, "NSWorkspaceWillSleepNotification", "NSWorkspaceSessionDidResignActiveNotification")
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	for range ch {
	}
}

func TestRunMainLoopRefusesAnotherThread(t *testing.T) {
	if err := RunMainLoop(context.Background()); !errors.Is(err, ErrNotMainThread) {
		t.Fatalf("RunMainLoop off the main thread = %v, want ErrNotMainThread", err)
	}
}

func TestObserveAndPostErrors(t *testing.T) {
	if _, err := Observe(context.Background(), DefaultCenter); !errors.Is(err, ErrNoNames) {
		t.Fatalf("no names: %v", err)
	}
	if _, err := Observe(context.Background(), Center(99), "x"); !errors.Is(err, ErrNoCenter) {
		t.Fatalf("bad center: %v", err)
	}
	if err := Post(Center(99), "x"); !errors.Is(err, ErrNoCenter) {
		t.Fatalf("post, bad center: %v", err)
	}
	boom := errors.New("boom")
	old := loadFrameworks
	loadFrameworks = func() error { return boom }
	defer func() { loadFrameworks = old }()
	if _, err := Observe(context.Background(), DefaultCenter, "x"); !errors.Is(err, boom) {
		t.Fatalf("observe, load: %v", err)
	}
	if err := Post(DefaultCenter, "x"); !errors.Is(err, boom) {
		t.Fatalf("post, load: %v", err)
	}
	if err := RunMainLoop(context.Background()); !errors.Is(err, boom) {
		t.Fatalf("RunMainLoop, load: %v", err)
	}
}
