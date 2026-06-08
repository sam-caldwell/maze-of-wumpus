// announce.go — startup announcement hook.
//
// Previously this spoke a greeting via the macOS `say` command; that
// audible startup announcement has been removed, so the hook is now a
// cross-platform no-op. It is retained (and declared as a `var`) so
// runProgram keeps a single, testable startup seam — tests swap in
// their own observer to assert it fires before the TUI runner starts.

package main

// announce is invoked once by runProgram right before the bubbletea
// TUI starts. It does nothing by default.
var announce = func() {}
