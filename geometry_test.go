// Copyright (c) 2026, the go-macos authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file.

package objc

import (
	"testing"
	"unsafe"
)

// The point of these is the layout, not the arithmetic. A struct passed by
// value to an Objective-C message has to match what the runtime expects field
// for field; if somebody "tidies" a float64 to a float32, nothing here stops
// compiling and every geometry call starts handing AppKit nonsense. So assert
// the shape, which is the part that is actually a contract.
func TestGeometryLayout(t *testing.T) {
	const cgfloat = unsafe.Sizeof(float64(0))

	if got, want := unsafe.Sizeof(NSPoint{}), 2*cgfloat; got != want {
		t.Errorf("NSPoint is %d bytes, want %d", got, want)
	}
	if got, want := unsafe.Sizeof(NSSize{}), 2*cgfloat; got != want {
		t.Errorf("NSSize is %d bytes, want %d", got, want)
	}
	if got, want := unsafe.Sizeof(NSRect{}), 4*cgfloat; got != want {
		t.Errorf("NSRect is %d bytes, want %d", got, want)
	}
	if got := unsafe.Offsetof(NSRect{}.Size); got != 2*cgfloat {
		t.Errorf("NSRect.Size starts at %d, want %d", got, 2*cgfloat)
	}
	if got := unsafe.Offsetof(NSSize{}.Height); got != cgfloat {
		t.Errorf("NSSize.Height starts at %d, want %d", got, cgfloat)
	}
	if got := unsafe.Offsetof(NSPoint{}.Y); got != cgfloat {
		t.Errorf("NSPoint.Y starts at %d, want %d", got, cgfloat)
	}
}

// A rect is built from the other two rather than from four loose numbers, so
// check they actually compose.
func TestNSRectComposes(t *testing.T) {
	r := NSRect{Origin: NSPoint{X: 1, Y: 2}, Size: NSSize{Width: 3, Height: 4}}
	if r.Origin.X != 1 || r.Origin.Y != 2 || r.Size.Width != 3 || r.Size.Height != 4 {
		t.Errorf("NSRect did not compose: %+v", r)
	}
}
