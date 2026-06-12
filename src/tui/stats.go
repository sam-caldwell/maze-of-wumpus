// stats.go — the value types the SimLoop and the UI goroutine exchange.
//
// The sim is the SOLE owner of the live *world.World; the UI goroutine
// never touches it. All coupling flows through three immutable values:
//
//   - viewState   : the UI's scroll / size / overlay intent, published to
//     the sim (atomic, latest-wins) so it renders the viewport the user
//     is actually looking at.
//   - screenFrame : a fully-composed, immutable screen the sim renders
//     after each tick and stores atomically; the UI just displays it —
//     no world access on the render path, no lock.
//   - worldCmd    : a world mutation the UI posts to the sim's command
//     channel (reseed, TTL/agent toggles). Applied on the sim goroutine
//     at a tick boundary, so the world stays single-threaded without a
//     mutex.
//
// This replaces the previous RWMutex-guarded shared-world design: there
// is now exactly one writer (the sim goroutine) and the UI is a pure,
// lock-free consumer of published frames.
package tui

import "maze-of-wumpus/src/world"

// viewState is the UI's render intent. Published by the UI on resize and
// on every keypress; loaded by the sim when it composes a frame. Scroll
// offsets, terminal size, and the shortest-path overlay all live on the
// UI side because they are display state, not world state.
type viewState struct {
	offsetX, offsetY int
	termW, termH     int
	showPath         bool
}

// screenFrame is one fully-rendered screen plus the measured right-pane
// width. The text is spliced as-is by the UI; rightW is fed back so the
// UI can size its viewport / clamp scroll offsets without reading the
// world.
type screenFrame struct {
	text   string
	rightW int
}

// worldCmd is a world mutation posted by the UI and applied on the sim
// goroutine. Returning a non-nil *World swaps the live world (used by
// reseed); returning nil mutates in place (used by the toggles).
type worldCmd func(*world.World) *world.World
