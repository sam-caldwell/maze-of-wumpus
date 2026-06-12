package world

import (
	"testing"
	"time"
)

// TestParallelRunner_AdvancesAndRaceFree drives the parallel runner with
// wandering agents (set a.Strategy so every step actually moves, deposits
// scent, and senses) and checks it advances. Run with -race to validate
// the concurrency model: no data races, no concurrent-map panics across
// the worker goroutines and the barrier coordinator.
func TestParallelRunner_AdvancesAndRaceFree(t *testing.T) {
	w := NewWorld(1)
	// Plain NewWorld wires no strategy dispatch; give each agent a simple
	// wandering policy so the full move/scent/sense path runs in parallel.
	for _, a := range w.Agents {
		a.Strategy = func(w *World, a *Agent) Pos { return w.FallbackMove(a) }
	}
	pr := NewParallelRunner(w, 10*time.Millisecond)
	rep := pr.Run(300 * time.Millisecond)

	if rep.TotalSteps() == 0 {
		t.Fatal("parallel run took no steps")
	}
	for _, a := range rep.Agents {
		if a.Steps == 0 {
			t.Errorf("agent %c took no steps", a.Label)
		}
	}
	if rep.Barriers == 0 {
		t.Error("no barriers ran")
	}
	if w.parallel {
		t.Error("parallel flag should be cleared after Run")
	}
}

// TestPutScent_SerialUnchanged: in serial mode PutScent writes the
// canonical grid directly and never buffers — preserving existing
// behavior for every non-parallel caller.
func TestPutScent_SerialUnchanged(t *testing.T) {
	w := NewWorld(2)
	a := w.Agents[0]
	w.PutScent(a, 5, 6)
	if w.ScentOwner[6][5] != a.Label {
		t.Errorf("serial PutScent didn't write the grid: got %q", w.ScentOwner[6][5])
	}
	if len(a.scentBuf) != 0 {
		t.Errorf("serial PutScent should not buffer, got %d deposits", len(a.scentBuf))
	}
}

// TestPutScent_ParallelBuffers: in parallel mode PutScent buffers onto
// the group instead of touching the shared grid.
func TestPutScent_ParallelBuffers(t *testing.T) {
	w := NewWorld(2)
	w.parallel = true
	defer func() { w.parallel = false }()
	a := w.Agents[0]
	w.PutScent(a, 7, 8)
	if w.ScentOwner[8][7] != 0 {
		t.Error("parallel PutScent must not write the canonical grid")
	}
	if len(a.scentBuf) != 1 || a.scentBuf[0].x != 7 || a.scentBuf[0].y != 8 {
		t.Errorf("parallel PutScent didn't buffer correctly: %+v", a.scentBuf)
	}
}

// TestAgentRng_Fallback: AgentRng returns the shared World.Rng when the
// agent has no private source (serial), and the private one when set.
func TestAgentRng_Fallback(t *testing.T) {
	w := NewWorld(3)
	a := w.Agents[0]
	if w.AgentRng(a) != w.Rng {
		t.Error("AgentRng should fall back to World.Rng in serial mode")
	}
}

// TestParallelRunner_PausedDoesNotStep: while paused the workers take no
// steps; after resuming they do. State is read after Stop (goroutines
// joined) so it's race-free.
func TestParallelRunner_PausedDoesNotStep(t *testing.T) {
	w := NewWorld(1)
	for _, a := range w.Agents {
		a.Strategy = func(w *World, a *Agent) Pos { return w.FallbackMove(a) }
	}
	pr := NewParallelRunner(w, 10*time.Millisecond)

	pr.SetPaused(true)
	pr.Start()
	time.Sleep(150 * time.Millisecond)
	pr.Stop()
	var paused int64
	for _, a := range w.Agents {
		paused += a.ParSteps
	}
	if paused != 0 {
		t.Errorf("paused runner stepped %d times, want 0", paused)
	}

	pr.SetPaused(false)
	pr.Start()
	time.Sleep(150 * time.Millisecond)
	pr.Stop()
	var played int64
	for _, a := range w.Agents {
		played += a.ParSteps
	}
	if played == 0 {
		t.Error("resumed runner took no steps")
	}
}

// TestParallelRunner_LatchesSolvedForReseed: once enough agents reach the
// goal-count threshold (the maze-solved condition), the runner latches
// Solved so a UI driver can auto-reseed (the parallel analogue of the
// serial loop's reseed-on-solve / the 'r' key).
func TestParallelRunner_LatchesSolvedForReseed(t *testing.T) {
	w := NewWorld(1)
	for i := 0; i < MazeSolvedAgentCount && i < len(w.Agents); i++ {
		w.Agents[i].Stats.GoalsReached = MazeSolvedGoals
	}
	if !w.MazeSolved() {
		t.Fatal("setup: maze should report solved")
	}
	pr := NewParallelRunner(w, 5*time.Millisecond)
	pr.Start()
	defer pr.Stop()
	deadline := time.After(3 * time.Second)
	for !pr.Solved() {
		select {
		case <-deadline:
			t.Fatal("runner never latched Solved despite maze-solved condition")
		default:
			time.Sleep(2 * time.Millisecond)
		}
	}
}
