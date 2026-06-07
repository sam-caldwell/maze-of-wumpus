// testsupport.go — helpers callable from sibling packages' tests.
// Lives in a non-_test.go file so the symbols are visible across
// package boundaries. Not used by production code.
package world

// EnableHazards is retained as a no-op shim for tests written before
// the hazard teardown. Hazards no longer exist; the only remaining
// toggle is TTL, which it leaves enabled.
func (w *World) EnableHazards() {
	w.TTLDisabled = false
}

// SpawnAgentForTest plants the labeled agent at the entrance,
// regardless of which other agent currently holds that cell. Used
// by tests that need to operate on a specific agent without waiting
// for the staggered initial respawn timers.
func SpawnAgentForTest(w *World, label rune) *Agent {
	entrance := w.Maze.EntrancePos
	if existing := w.AgentAt[entrance.Y][entrance.X]; existing != nil {
		existing.Alive = false
		w.AgentAt[entrance.Y][entrance.X] = nil
	}
	a := w.AgentByLabel(label)
	a.Alive = true
	a.Disabled = false // tests want this agent active
	a.Pos = entrance
	a.RespawnIn = -1
	a.Stats.ActualDistance = 0
	w.AgentAt[entrance.Y][entrance.X] = a
	w.MarkAgentSensed(a)
	return a
}

// EnableAllAgents flips Disabled=false on every agent. Test helper
// for scenarios that drive the simulation without using
// SpawnAgentForTest (e.g., long-run "no panic" checks).
func (w *World) EnableAllAgents() {
	for _, a := range w.Agents {
		a.Disabled = false
	}
}
