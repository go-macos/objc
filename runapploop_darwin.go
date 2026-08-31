//go:build darwin

package objc

// nsEventMaskAny is NSEventMaskAny: every event type there is.
const nsEventMaskAny = ^uint64(0)

// RunAppLoop enters an AppKit event loop and LEAVES IT when stop says so.
//
// It exists because -[NSApplication run] cannot be left on demand.
// -[NSApplication stop:] sets a flag that AppKit reads only after it finishes
// processing an event, so an application with nobody touching it waits for an
// event that is never coming and never returns -- see [StopApp], which wakes
// the run loop and is still not enough, because waking a run loop is not an
// event either.
//
// This is the loop -[NSApplication run] runs, written out: take the next
// event, send it, and between the two ask the caller whether to carry on.
// Waking the main run loop makes the wait return nil, the caller is asked, and
// a caller that says stop gets its thread back at once -- with no event, no
// mouse movement and nobody at the machine.
//
// It is NOT a pump. -[NSApplication sendEvent:] is what tracks a menu, drives a
// window and drags a scrollbar; a program that sampled the run loop instead
// would show a menu-bar item whose menu never opens, which is a real defect
// this fleet has already paid for once. The only thing missing against run is
// the application lifecycle around the loop, which a menu-bar program does not
// have.
//
// It must be called on the process main OS thread. policy is an
// NSApplicationActivationPolicy value (0 = Regular, 1 = Accessory/menu-bar).
// A nil stop runs for ever, which is what run does.
func RunAppLoop(policy int, stop func() bool) {
	app := App()
	app.Send(Sel("setActivationPolicy:"), policy)
	// finishLaunching, not run: the application has to be told it has started
	// -- it posts NSApplicationDidFinishLaunching and unblocks the parts of
	// AppKit that wait for it -- and run is the very thing being replaced.
	app.Send(Sel("finishLaunching"))

	mode := NSString("kCFRunLoopDefaultMode")
	mode.Send(Sel("retain")) // for the life of the loop
	forever := ClassID("NSDate").Send(Sel("distantFuture"))
	for stop == nil || !stop() {
		// Every argument here is an integer, a pointer or a bool. That is not
		// an accident: the obvious way to break out of -[NSApplication run] is
		// to post an application-defined event, and +[NSEvent
		// otherEventWithType:location:...] takes an NSPoint and a timestamp --
		// three doubles, in floating-point registers, through a bridge that
		// passes everything in integer ones.
		ev := app.Send(Sel("nextEventMatchingMask:untilDate:inMode:dequeue:"),
			nsEventMaskAny, forever, mode, true)
		if ev == 0 {
			// The wait was interrupted rather than satisfied: somebody stopped
			// the run loop. Ask again whether to carry on -- that is the whole
			// mechanism.
			continue
		}
		app.Send(Sel("sendEvent:"), ev)
	}
}
