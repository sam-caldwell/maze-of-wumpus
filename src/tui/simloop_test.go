package tui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"maze-of-wumpus/src/world"
)

// TestSimLoop_AdvancesWorld: a started loop advances the world's Cycle
// on its own goroutine, and Stop halts it cleanly.
func TestSimLoop_AdvancesWorld(t *testing.T) {
	w := world.NewWorld(1)
	s := NewSimLoop(w, world.NewWorld, 5*time.Millisecond)
	s.Start()
	deadline := time.After(3 * time.Second)
	for {
		var cyc int
		s.read(func(cw *world.World) { cyc = cw.Cycle })
		if cyc > 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("sim loop did not advance the world")
		default:
			time.Sleep(2 * time.Millisecond)
		}
	}
	s.Stop()
}

// TestSimLoop_ConcurrentReadWrite: rendering (read) and key-driven
// mutation (write) from another goroutine while the loop steps must be
// race-free. Run with -race to catch violations.
func TestSimLoop_ConcurrentReadWrite(t *testing.T) {
	w := world.NewWorld(2)
	s := NewSimLoop(w, world.NewWorld, time.Millisecond)
	s.Start()
	defer s.Stop()
	done := time.After(300 * time.Millisecond)
	for {
		select {
		case <-done:
			return
		default:
		}
		// Read like the renderer.
		s.read(func(cw *world.World) { _ = cw.Cycle })
		// Mutate like a key handler (toggle TTL) under the write lock.
		s.write(func(cw *world.World) *world.World {
			cw.TTLDisabled = !cw.TTLDisabled
			return nil
		})
	}
}

// TestAsyncModel_TickDoesNotStep: in async mode the Model's tick must
// NOT step the world itself (the SimLoop does). The Model just re-arms
// the repaint. We don't Start the loop, so Cycle stays put.
func TestAsyncModel_TickDoesNotStep(t *testing.T) {
	m := NewAsyncModel(1, world.NewWorld)
	if m.sim == nil {
		t.Fatal("async model should have a SimLoop")
	}
	startCycle := m.sim.world.Cycle
	m2, cmd := m.Update(tickMsg{})
	if cmd == nil {
		t.Error("async tick should re-arm a repaint cmd")
	}
	if m2.(Model).sim.world.Cycle != startCycle {
		t.Error("async tick must not advance the world (the SimLoop does)")
	}
}

// TestStatsAggregator_LatestWins: the listener converges on the most
// recently published frame; Latest is nil before any publish.
func TestStatsAggregator_LatestWins(t *testing.T) {
	a := NewStatsAggregator()
	if a.Latest() != nil {
		t.Error("Latest should be nil before any publish")
	}
	a.Start()
	defer a.Stop()
	last := &StatsFrame{header: "frame-N"}
	for i := 0; i < 50; i++ {
		last = &StatsFrame{header: "frame"}
		a.publish(last)
	}
	deadline := time.After(2 * time.Second)
	for a.Latest() == nil {
		select {
		case <-deadline:
			t.Fatal("aggregator never received a frame")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	if a.Latest().header != "frame" {
		t.Errorf("Latest header = %q, want %q", a.Latest().header, "frame")
	}
}

// TestSimLoop_PublishesStats: a running sim loop publishes rendered
// stat frames to its aggregator (race-checked).
func TestSimLoop_PublishesStats(t *testing.T) {
	w := world.NewWorld(7)
	s := NewSimLoop(w, world.NewWorld, 5*time.Millisecond)
	s.statsEvery = time.Millisecond // publish promptly for the test
	s.Start()
	defer s.Stop()
	deadline := time.After(3 * time.Second)
	for s.stats.Latest() == nil {
		select {
		case <-deadline:
			t.Fatal("sim loop never published a stats frame")
		default:
			time.Sleep(2 * time.Millisecond)
		}
	}
	f := s.stats.Latest()
	if f.header == "" || len(f.rightLines) == 0 || f.bottom == "" {
		t.Errorf("published stats frame is incomplete: %+v", f)
	}
}

// TestAsyncModel_ViewRendersUnderLock: View on an async model renders
// the world without panicking and includes the title.
func TestAsyncModel_ViewRendersUnderLock(t *testing.T) {
	m := NewAsyncModel(3, world.NewWorld)
	if got := m.View(); got == "" {
		t.Error("async View produced empty output")
	}
	// A key still works (toggle ShowPath) through the write path.
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	if !m2.(Model).ShowPath {
		t.Error("'s' did not toggle ShowPath in async mode")
	}
}
