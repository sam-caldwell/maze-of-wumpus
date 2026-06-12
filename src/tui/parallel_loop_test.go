package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"maze-of-wumpus/src/world"
)

// TestParallelLoop_RendersFrames: the parallel loop, once the UI reports a
// terminal size, renders frames from the world at its barriers while
// worker goroutines drive the agents. Run with -race to validate the
// barrier render against the concurrent workers.
func TestParallelLoop_RendersFrames(t *testing.T) {
	w := world.NewWorld(1)
	for _, a := range w.Agents { // wandering policy so agents actually move
		a.Strategy = func(w *world.World, a *world.Agent) world.Pos { return w.FallbackMove(a) }
	}
	pl := NewParallelLoop(w)
	pl.publishEvery = time.Millisecond
	pl.publishView(&viewState{termW: 80, termH: 24})
	pl.Start()
	defer pl.Stop()

	deadline := time.After(3 * time.Second)
	for pl.latestFrame() == nil {
		select {
		case <-deadline:
			t.Fatal("parallel loop never published a frame")
		default:
			time.Sleep(2 * time.Millisecond)
		}
	}
	if !strings.Contains(pl.latestFrame().text, "Maze of Wumpus") {
		t.Errorf("rendered frame missing title: %q", pl.latestFrame().text)
	}
}

// TestParallelModel_ViewAndKeys: a parallel-backed Model renders a non-
// empty placeholder before the first frame and handles a UI-local key.
func TestParallelModel_ViewAndKeys(t *testing.T) {
	m := NewParallelModel(3, world.NewWorld)
	if _, ok := m.sim.(*ParallelLoop); !ok {
		t.Fatal("parallel model should be backed by a ParallelLoop")
	}
	if m.View() == "" {
		t.Error("parallel View produced empty output")
	}
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	if !m2.(Model).ShowPath {
		t.Error("'s' did not toggle ShowPath in parallel mode")
	}
}
