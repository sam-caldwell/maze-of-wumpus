// parallel_loop.go — a driver that runs the world with one worker
// goroutine per agent (world.ParallelRunner) and feeds the TUI live
// frames, so the maze can be watched while agents navigate it in
// parallel at their own rates.
//
// It satisfies the same `driver` interface as SimLoop, so the Model is
// agnostic about which engine is behind it. Frames are rendered at the
// runner's barrier (where the world is stable and all workers are
// paused), throttled to the render cadence; world mutations from the UI
// (TTL / agent toggles) are posted to the runner and applied at that same
// barrier. Reseed ('r') is not supported here — the workers hold direct
// agent references, so swapping the world mid-run would orphan them — so
// that key is a no-op in parallel mode.
package tui

import (
	"sync/atomic"
	"time"

	"maze-of-wumpus/src/world"
)

// parallelBarrierEvery is how often the parallel runner pauses workers to
// run global bookkeeping (and render a frame). Short enough for a smooth
// repaint; long enough that the stop-the-world overhead stays small.
const parallelBarrierEvery = 20 * time.Millisecond

// ParallelLoop drives the live world via a world.ParallelRunner and
// publishes rendered frames for the Model to display.
type ParallelLoop struct {
	runner       *world.ParallelRunner
	view         atomic.Pointer[viewState]
	frame        atomic.Pointer[screenFrame]
	publishEvery time.Duration
	lastRender   time.Time // touched only on the barrier goroutine
}

// NewParallelLoop wraps w for parallel, TUI-driven execution.
func NewParallelLoop(w *world.World) *ParallelLoop {
	return &ParallelLoop{
		runner:       world.NewParallelRunner(w, parallelBarrierEvery),
		publishEvery: renderInterval,
	}
}

// Start wires the barrier render hook and launches the parallel runner.
func (pl *ParallelLoop) Start() {
	pl.runner.OnBarrier = pl.renderAtBarrier
	pl.runner.Start()
}

// Stop halts the runner and its workers.
func (pl *ParallelLoop) Stop() { pl.runner.Stop() }

// post forwards an in-place world mutation (TTL / agent toggles) to the
// runner, applied at the next barrier with workers paused. World swaps go
// through reseed, not post.
func (pl *ParallelLoop) post(cmd worldCmd) {
	pl.runner.Post(func(w *world.World) { _ = cmd(w) })
}

// reseed stops every worker, builds a fresh world (preserving learning),
// and restarts the workers on it — the 'r' key. Runs on the UI goroutine;
// Stop blocks until the workers and barrier have exited (≈ one barrier
// interval) before the new runner takes over, so there is no overlap. The
// pause state carries across so reseeding while paused stays paused.
func (pl *ParallelLoop) reseed(build WorldBuilder) {
	wasPaused := pl.runner.Paused()
	pl.runner.Stop() // halt all agents + the barrier, wait for exit
	nw := reseedWorldPreservingLearning(pl.runner.World(), build)
	pl.runner = world.NewParallelRunner(nw, parallelBarrierEvery)
	pl.runner.OnBarrier = pl.renderAtBarrier
	pl.runner.SetPaused(wasPaused)
	pl.runner.Start() // restart agents on the new map
}

// togglePause flips the frozen state and reports the new value.
func (pl *ParallelLoop) togglePause() bool {
	p := !pl.runner.Paused()
	pl.runner.SetPaused(p)
	return p
}

// needsReseed reports whether the maze has been solved and the loop should
// be reseeded (the runner latches this at a barrier; the Model polls it on
// tick and calls reseed, which restarts the workers on a fresh map).
func (pl *ParallelLoop) needsReseed() bool { return pl.runner.Solved() }

// publishView stores the UI's latest scroll/size/overlay intent.
func (pl *ParallelLoop) publishView(vs *viewState) { pl.view.Store(vs) }

// latestFrame returns the freshest rendered screen, or nil before the
// first barrier render.
func (pl *ParallelLoop) latestFrame() *screenFrame { return pl.frame.Load() }

// renderAtBarrier composes a frame from the stable world at a barrier,
// throttled to the render cadence. Runs on the runner's barrier goroutine
// with the world paused, so reading it here is race-free.
func (pl *ParallelLoop) renderAtBarrier(w *world.World) {
	if time.Since(pl.lastRender) < pl.publishEvery {
		return
	}
	vs := pl.view.Load()
	if vs == nil || vs.termW == 0 || vs.termH == 0 {
		return // wait for the UI to report a terminal size
	}
	pl.lastRender = time.Now()
	m := Model{
		World:    w,
		ShowPath: vs.showPath,
		termW:    vs.termW,
		termH:    vs.termH,
		offsetX:  vs.offsetX,
		offsetY:  vs.offsetY,
		paused:   pl.runner.Paused(),
	}
	text, rightW := m.composeScreen()
	pl.frame.Store(&screenFrame{text: text, rightW: rightW})
}
