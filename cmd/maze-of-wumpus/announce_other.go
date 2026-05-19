//go:build !darwin

// announce_other.go — no-op startup announce for every non-macOS
// platform. The darwin build uses announce_darwin.go which invokes
// the `say` command.

package main

// announce is the cross-platform stub: nothing speaks, the game just
// starts. Declared as a `var` so tests can swap in their own observer.
var announce = func() {}
