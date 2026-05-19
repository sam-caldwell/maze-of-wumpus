//go:build darwin

// announce_darwin.go — macOS-only audible startup announcement using
// the system `say` command. Build-tag-restricted so other platforms
// link the no-op stub in announce_other.go instead.

package main

import "os/exec"

// announce is invoked once by runProgram right before the bubbletea
// TUI starts. The `say` command runs non-blocking (Start, not Run) so
// the announcement plays in parallel with the TUI render — by the
// time the user finishes hearing it, the maze is already on screen.
// Errors are ignored: if `say` is missing or the audio subsystem is
// unavailable the game still launches normally.
//
// Declared as a `var` so tests can substitute a stub without invoking
// the real `say` binary.
var announce = func() {
	_ = exec.Command("say", "starting...the maze of wumpus.").Start()
}
