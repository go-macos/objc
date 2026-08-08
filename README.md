# go-macos/objc

[![CI](https://github.com/go-macos/objc/actions/workflows/ci.yml/badge.svg)](https://github.com/go-macos/objc/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/go-macos/objc.svg)](https://pkg.go.dev/github.com/go-macos/objc)
[![Go 1.26.4](https://img.shields.io/badge/go-1.26.4-00ADD8)](https://go.dev/dl/)
[![License: BSD-3-Clause](https://img.shields.io/badge/license-BSD--3--Clause-blue.svg)](LICENSE)

A pure-Go (**CGO_ENABLED=0**) bridge to the macOS Objective-C runtime, reached
entirely through [ebitengine/purego](https://github.com/ebitengine/purego) —
`dlopen` + `objc_msgSend`, no cgo, no shelling out to `osascript`.

It is the single shared home for the Objective-C plumbing that the fleet's
CGO=0 macOS code kept re-implementing: selector/class lookup, NSString↔Go
bridging, framework `dlopen`, autorelease-pool handling and run loops.

## Surface

| Area | API |
| --- | --- |
| Selector / class lookup (cached) | `Sel`, `RegisterName`, `GetClass`, `ClassID` |
| Messaging | `ID.Send`, generic `Send[T]` (scalars, `CGFloat`, structs) |
| Runtime classes | `RegisterClass`, `RegisterClassWithProtocols`, `GetProtocol`, `MethodDef` |
| Strings | `NSString`, `GoString` |
| Objects / dictionaries | `Stringify`, `DictToMap`, `MapToDict` |
| Frameworks | `Load` + `Foundation` / `AppKit` / `WebKit` / `CoreFoundation` / `Security` / `LibSystem` |
| Scopes | `AutoreleasePool` |
| Run loops | `App`, `RunApp`, `Run(ctx, class, setup)` + `Runner` |
| Validation | `ValidateName` |

```go
import "github.com/go-macos/objc"

objc.Load(objc.Foundation, objc.AppKit)

s := objc.GoString(objc.NSString("héllo"))                 // "héllo"
n := objc.Send[float64](objc.NSString("3.5"), objc.Sel("doubleValue")) // 3.5

cls, _ := objc.RegisterClass("MyTarget", objc.GetClass("NSObject"),
    []objc.MethodDef{
        {Cmd: objc.Sel("fire:"), Fn: func(self objc.ID, _ objc.SEL, sender objc.ID) {
            // handle sender
        }},
    })
```

## Design

- **On darwin** the runtime types (`ID`, `SEL`, `Class`, `IMP`, `MethodDef`) are
  aliases of the corresponding `purego/objc` types, so method closures satisfy
  purego's strict `NewIMP` `(id, SEL)` receiver check and existing purego-based
  call sites can switch their import to this package and keep compiling.
- **On non-darwin** the same symbols exist as `uintptr`-width types and stubs
  that report [`ErrUnsupported`], so consumers cross-compile. The OS-independent
  core (selector/class caches, name validation, the `GoString` marshalling
  logic and `Load`) stays fully functional and is covered **100%** on every
  lane through injection seams.
- The darwin C/objc calls sit behind fake-injection seams, so their error
  branches are reachable in tests; the real `objc_msgSend` / NSString /
  `RegisterClass` / run-loop paths are verified **on-device** in CI.

## License

BSD-3-Clause © the go-macos/objc authors.
