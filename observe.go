package objc

import "errors"

// Center names the notification center [Observe] listens on.
type Center int

const (
	// DefaultCenter is +[NSNotificationCenter defaultCenter]: notifications
	// posted inside this process.
	DefaultCenter Center = iota
	// DistributedCenter is +[NSDistributedNotificationCenter defaultCenter]:
	// notifications posted by any process of the login session, such as
	// com.apple.screenIsLocked and com.apple.screenIsUnlocked.
	DistributedCenter
	// WorkspaceCenter is the center of +[NSWorkspace sharedWorkspace]:
	// NSWorkspaceWillSleepNotification, NSWorkspaceDidWakeNotification,
	// NSWorkspaceSessionDidResignActiveNotification and their kin.
	WorkspaceCenter
)

// ErrNoNames is returned by [Observe] when it is asked to watch nothing.
var ErrNoNames = errors.New("objc: Observe needs at least one notification name")

// ErrNotMainThread is returned by [RunMainLoop] when it is not called on the
// process's main OS thread.
var ErrNotMainThread = errors.New("objc: RunMainLoop must run on the main thread")

// ErrNoCenter is returned by [Observe] when the center's class is not there
// (a framework that would not load, or an unknown [Center]).
var ErrNoCenter = errors.New("objc: no such notification center")
