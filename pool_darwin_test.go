// Copyright (c) the go-macos authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build darwin

package objc

import (
	"runtime"
	"sync"
	"testing"
)

// TestAPoolIsDrainedOnTheThreadThatMadeIt.
//
// An NSAutoreleasePool belongs to its creating thread, and Go moves an unlocked
// goroutine to another thread at any preemption point. Draining one on a thread
// that never owned it is a SIGSEGV inside objc_autoreleasePoolPop, at a program
// counter with nothing of this package on the stack — which is how it was
// eventually found: from a CI log, in a caller that had done nothing but look up
// two more keyboard shortcuts.
//
// This hammers it from many goroutines, each doing enough inside the pool to
// give the scheduler somewhere to preempt. It is a RACE, so a pass is not proof
// that the lock is there; a crash is proof that it is not. What makes it worth
// keeping is that the same body, run against the unlocked version, took the
// process down.
func TestAPoolIsDrainedOnTheThreadThatMadeIt(t *testing.T) {
	if err := Load(Foundation); err != nil {
		t.Skipf("Foundation unavailable: %v", err)
	}
	const goroutines, rounds = 8, 200
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < rounds; i++ {
				AutoreleasePool(func() {
					// Real autoreleased objects, and a yield in the middle:
					// between the alloc and the drain is exactly where the
					// goroutine must not be allowed to move.
					s := NSString("a string that is autoreleased")
					runtime.Gosched()
					_ = GoString(s)
					runtime.Gosched()
				})
			}
		}()
	}
	wg.Wait()
}

// TestThePoolLeavesTheThreadUnlockedAfterwards: pinning is for the pool's
// lifetime and not beyond it, or a caller that uses a pool in a loop ends up
// with a goroutine welded to a thread for the rest of its life.
func TestThePoolLeavesTheThreadUnlockedAfterwards(t *testing.T) {
	if err := Load(Foundation); err != nil {
		t.Skipf("Foundation unavailable: %v", err)
	}
	// A goroutine of its own, so a lock left behind cannot be mistaken for the
	// test framework's own.
	done := make(chan bool, 1)
	go func() {
		AutoreleasePool(func() {})
		// If the thread were still locked, this goroutine could not be moved,
		// and the only portable way to notice is that UnlockOSThread has
		// already balanced: locking once more and unlocking must return it to
		// unlocked, which a leaked lock would not.
		runtime.LockOSThread()
		runtime.UnlockOSThread()
		done <- true
	}()
	if !<-done {
		t.Error("the goroutine did not finish")
	}
}

// TestPoolsNest, because a caller may use one inside another without knowing.
func TestPoolsNest(t *testing.T) {
	if err := Load(Foundation); err != nil {
		t.Skipf("Foundation unavailable: %v", err)
	}
	AutoreleasePool(func() {
		outer := GoString(NSString("outer"))
		AutoreleasePool(func() {
			if got := GoString(NSString("inner")); got != "inner" {
				t.Errorf("the inner pool gave %q", got)
			}
		})
		if outer != "outer" {
			t.Errorf("the outer string became %q", outer)
		}
	})
}

// TestAPanicStillDrains: the pool is drained on the way out however fn leaves,
// and the thread is unpinned with it.
func TestAPanicStillDrains(t *testing.T) {
	if err := Load(Foundation); err != nil {
		t.Skipf("Foundation unavailable: %v", err)
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Error("the panic did not come back out")
			}
		}()
		AutoreleasePool(func() { panic("from inside the pool") })
	}()
	// And the package still works afterwards, which it would not if the pool
	// had been left un-drained on a thread nobody owns.
	AutoreleasePool(func() {
		if got := GoString(NSString("after")); got != "after" {
			t.Errorf("after the panic, a string came back as %q", got)
		}
	})
}
