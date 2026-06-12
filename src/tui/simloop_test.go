package tui

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"maze-of-wumpus/src/world"
)

// TestSimLoop_AdvancesWorld: a started loop advances the world on its own
// goroutine — observed via the race-safe lastCycle counter — and Stop
// halts it cleanly.
func TestSimLoop_AdvancesWorld(t *testing.T) {
	w := world.NewWorld(1)
	s := NewSimLoop(w, world.NewWorld, 5*time.Millisecond)
	s.Start()
	deadline := time.After(3 * time.Second)
	for s.lastCycle.Load() == 0 {
		select {
		case <-deadline:
			t.Fatal("sim loop did not advance the world")
		default:
			time.Sleep(2 * time.Millisecond)
		}
	}
	s.Stop()
}

// TestSimLoop_ConcurrentCommandsAndView: the UI surface — posting world
// commands and publishing view intent from another goroutine while the
// loop steps and renders — must be race-free. The world is only ever
// touched on the sim goroutine, so there is no lock; run with -race to
// prove there is also no race.
func TestSimLoop_ConcurrentCommandsAndView(t *testing.T) {
	w := world.NewWorld(2)
	s := NewSimLoop(w, world.NewWorld, time.Millisecond)
	s.publishEvery = time.Millisecond
	s.Start()
	defer s.Stop()
	done := time.After(300 * time.Millisecond)
	for {
		select {
		case <-done:
			return
		default:
		}
		// Post a world mutation like a key handler (toggle TTL); applied
		// on the sim goroutine.
		s.post(func(cw *world.World) *world.World {
			cw.TTLDisabled = !cw.TTLDisabled
			return nil
		})
		// Publish view intent like the resize/render path; the sim loads
		// it to compose frames.
		s.publishView(&viewState{termW: 80, termH: 24})
	}
}

// TestAsyncModel_TickDoesNotStep: in async mode the Model's tick must NOT
// step the world itself (the SimLoop does). We don't Start the loop, so
// no goroutine runs and Cycle stays put.
func TestAsyncModel_TickDoesNotStep(t *testing.T) {
	m := NewAsyncModel(1, world.NewWorld)
	if m.sim == nil {
		t.Fatal("async model should have a SimLoop")
	}
	sim := m.sim.(*SimLoop)
	startCycle := sim.world.Cycle
	m2, cmd := m.Update(tickMsg{})
	if cmd == nil {
		t.Error("async tick should re-arm a repaint cmd")
	}
	if m2.(Model).sim.(*SimLoop).world.Cycle != startCycle {
		t.Error("async tick must not advance the world (the SimLoop does)")
	}
}

// TestSimLoop_PublishesFrame: once the UI has reported a terminal size, a
// running loop renders and stores a composed screen frame (race-checked).
func TestSimLoop_PublishesFrame(t *testing.T) {
	w := world.NewWorld(7)
	s := NewSimLoop(w, world.NewWorld, 5*time.Millisecond)
	s.publishEvery = time.Millisecond
	s.publishView(&viewState{termW: 80, termH: 24})
	s.Start()
	defer s.Stop()
	deadline := time.After(3 * time.Second)
	for s.frame.Load() == nil {
		select {
		case <-deadline:
			t.Fatal("sim loop never published a frame")
		default:
			time.Sleep(2 * time.Millisecond)
		}
	}
	f := s.frame.Load()
	if f.text == "" || !strings.Contains(f.text, "Maze of Wumpus") {
		t.Errorf("published frame is incomplete: %q", f.text)
	}
	if f.rightW <= 0 {
		t.Errorf("published frame rightW = %d, want > 0", f.rightW)
	}
}

// TestSimLoop_NoFrameBeforeSize: with no view intent yet (the UI hasn't
// reported a terminal size), the loop must NOT publish a frame — this is
// what keeps the sim from ever rendering the full 1024×1024 board.
func TestSimLoop_NoFrameBeforeSize(t *testing.T) {
	w := world.NewWorld(8)
	s := NewSimLoop(w, world.NewWorld, time.Millisecond)
	s.publishEvery = time.Millisecond
	s.Start()
	defer s.Stop()
	time.Sleep(50 * time.Millisecond)
	if s.frame.Load() != nil {
		t.Error("loop published a frame before the UI reported a terminal size")
	}
}

// TestSimLoop_AppliesCommand: a posted command is drained and run on the
// sim goroutine. Observed via an atomic the command sets, so the test
// never reads the world off the sim goroutine (race-free under -race).
func TestSimLoop_AppliesCommand(t *testing.T) {
	w := world.NewWorld(5)
	s := NewSimLoop(w, world.NewWorld, 2*time.Millisecond)
	s.Start()
	defer s.Stop()
	var applied atomic.Bool
	s.post(func(cw *world.World) *world.World {
		applied.Store(true)
		return nil
	})
	deadline := time.After(3 * time.Second)
	for !applied.Load() {
		select {
		case <-deadline:
			t.Fatal("posted command was never applied")
		default:
			time.Sleep(2 * time.Millisecond)
		}
	}
}

// TestAsyncModel_ViewAndKeys: View on an async model renders a non-empty
// placeholder before the first frame, and a UI-local key still updates
// Model state (ShowPath) without a running loop.
func TestAsyncModel_ViewAndKeys(t *testing.T) {
	m := NewAsyncModel(3, world.NewWorld)
	if got := m.View(); got == "" {
		t.Error("async View produced empty output")
	}
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	if !m2.(Model).ShowPath {
		t.Error("'s' did not toggle ShowPath in async mode")
	}
}
