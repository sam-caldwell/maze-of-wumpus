package tui

import (
	"strings"
	"testing"
	"time"

	"maze-of-wumpus/src/world"
)

// TestParallelLoop_Reseed: the 'r' path stops the workers, swaps in a
// fresh world, and restarts — and the restarted runner publishes frames
// again. Run with -race to validate the stop/restart handoff.
func TestParallelLoop_Reseed(t *testing.T) {
	w := world.NewWorld(1)
	for _, a := range w.Agents { // wandering policy so workers actually step
		a.Strategy = func(w *world.World, a *world.Agent) world.Pos { return w.FallbackMove(a) }
	}
	pl := NewParallelLoop(w)
	pl.publishEvery = time.Millisecond
	pl.publishView(&viewState{termW: 80, termH: 24})
	pl.Start()
	defer pl.Stop()

	waitFrame := func() {
		deadline := time.After(3 * time.Second)
		for pl.latestFrame() == nil {
			select {
			case <-deadline:
				t.Fatal("no frame published")
			default:
				time.Sleep(2 * time.Millisecond)
			}
		}
	}
	waitFrame()
	old := pl.runner.World()

	pl.frame.Store(nil)         // so we can confirm the NEW runner publishes
	pl.reseed(world.NewWorld)   // stop → fresh world → restart
	if pl.runner.World() == old {
		t.Fatal("reseed did not swap the world")
	}
	waitFrame() // restarted runner must render the new map
}

// TestParallelLoop_PausedFrameShowsIndicator: a paused loop renders the
// map with the "PAUSED" header so the user knows to press space.
func TestParallelLoop_PausedFrameShowsIndicator(t *testing.T) {
	w := world.NewWorld(1)
	pl := NewParallelLoop(w)
	pl.togglePause() // start paused, as NewParallelModel does
	pl.publishEvery = time.Millisecond
	pl.publishView(&viewState{termW: 80, termH: 24})
	pl.Start()
	defer pl.Stop()

	deadline := time.After(3 * time.Second)
	for pl.latestFrame() == nil {
		select {
		case <-deadline:
			t.Fatal("no frame published while paused")
		default:
			time.Sleep(2 * time.Millisecond)
		}
	}
	if !strings.Contains(pl.latestFrame().text, "PAUSED") {
		t.Error("paused frame missing the PAUSED indicator")
	}
}
