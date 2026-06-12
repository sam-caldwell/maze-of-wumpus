// simloop.go — decouples simulation from rendering. In the live app the
// simulation advances on its own goroutine (SimLoop) while the bubbletea
// UI renders the latest published frame on its own cadence, so a slow
// Step() can never freeze input or rendering. Under test there is no
// SimLoop and the Model steps synchronously in Update (deterministic,
// single-goroutine), so the existing suite is unaffected.
//
// Ownership model: the SimLoop goroutine is the ONLY thing that touches
// the live *world.World — it steps it, applies UI-posted commands to it,
// and renders frames from it, all on that one goroutine. The UI never
// reads or writes the world; it publishes its view intent (s.view),
// posts mutations (s.cmds), and consumes rendered frames (s.frame). That
// single-writer discipline is why there is no lock here anymore.
package tui

import (
	"sync/atomic"
	"time"

	"maze-of-wumpus/src/world"
)

// renderInterval is how often the TUI repaints in async mode. Kept
// short for input responsiveness; independent of how long a sim Step
// actually takes.
const renderInterval = 50 * time.Millisecond

// cmdBuffer is the depth of the UI→sim command channel. Keypresses
// arrive at human speed and the sim drains the whole channel every tick,
// so this is never realistically filled; an over-full channel simply
// drops the excess (see post) rather than blocking the UI goroutine.
const cmdBuffer = 64

// SimLoop owns the live *world.World and advances it on a background
// goroutine. The world is touched only by that goroutine; the UI
// interacts through lock-free atomics (view, frame, lastCycle) and the
// buffered command channel (cmds).
type SimLoop struct {
	world *world.World
	build WorldBuilder

	interval time.Duration
	stop     chan struct{}
	stopped  chan struct{}

	// cmds carries world mutations posted by the UI (reseed / toggles),
	// drained and applied on the sim goroutine at each tick boundary.
	cmds chan worldCmd
	// view is the UI's latest render intent (scroll/size/overlay); the
	// sim loads it when composing a frame.
	view atomic.Pointer[viewState]
	// frame is the most recently composed screen; the UI loads it lock-
	// free on its render path.
	frame atomic.Pointer[screenFrame]
	// lastCycle mirrors world.Cycle after each step so callers (and
	// tests) can observe progress without touching the world.
	lastCycle atomic.Int64

	// publishEvery throttles frame rendering to roughly the render
	// cadence so the sim isn't composing a screen on every fast step.
	publishEvery time.Duration
	lastPublish  time.Time

	// paused, when true, freezes stepping: the loop only spawns agents
	// (so the map renders) and re-publishes frames, advancing nothing.
	// Toggled by the UI ('space'); read on the sim goroutine.
	paused atomic.Bool
}

// NewSimLoop wraps a world for background stepping at the given cadence.
func NewSimLoop(w *world.World, build WorldBuilder, interval time.Duration) *SimLoop {
	return &SimLoop{
		world:        w,
		build:        build,
		interval:     interval,
		stop:         make(chan struct{}),
		stopped:      make(chan struct{}),
		cmds:         make(chan worldCmd, cmdBuffer),
		publishEvery: renderInterval,
	}
}

// Start launches the background stepping goroutine. The ticker drops
// ticks when a Step runs long, so the sim free-runs (bounded by Step
// time) when slow and paces at `interval` when fast.
func (s *SimLoop) Start() {
	go func() {
		defer close(s.stopped)
		t := time.NewTicker(s.interval)
		defer t.Stop()
		for {
			select {
			case <-s.stop:
				return
			case <-t.C:
				s.step()
				s.maybePublishFrame()
			}
		}
	}()
}

// Stop halts the sim goroutine and waits for it to exit.
func (s *SimLoop) Stop() {
	close(s.stop)
	<-s.stopped
}

// post hands a world mutation to the sim without blocking the UI
// goroutine. If the buffer is full (the UI is posting faster than the
// sim drains — not reachable at human keypress rates) the command is
// dropped rather than stalling rendering.
func (s *SimLoop) post(fn worldCmd) {
	select {
	case s.cmds <- fn:
	default:
	}
}

// publishView stores the UI's latest render intent for the sim to read.
func (s *SimLoop) publishView(vs *viewState) { s.view.Store(vs) }

// latestFrame returns the most recently published screen, or nil before
// the first publish. Part of the driver interface.
func (s *SimLoop) latestFrame() *screenFrame { return s.frame.Load() }

// reseed swaps the live world for a fresh one (preserving learning),
// applied on the sim goroutine at the next tick boundary.
func (s *SimLoop) reseed(build WorldBuilder) {
	s.post(func(w *world.World) *world.World {
		return reseedWorldPreservingLearning(w, build)
	})
}

// togglePause flips the frozen state and reports the new value.
func (s *SimLoop) togglePause() bool {
	p := !s.paused.Load()
	s.paused.Store(p)
	return p
}

// isPaused reports whether the loop is frozen (used to mark frames).
func (s *SimLoop) isPaused() bool { return s.paused.Load() }

// needsReseed is always false for SimLoop: it auto-reseeds itself inside
// step() when the maze is solved, so the UI never has to drive it.
func (s *SimLoop) needsReseed() bool { return false }

// driver is the execution backend the Model talks to in async/live mode:
// either the serial SimLoop or the ParallelLoop. The Model publishes its
// view intent, posts world mutations, requests reseeds, toggles pause,
// and consumes published frames — all without touching the live world.
type driver interface {
	Start()
	Stop()
	post(worldCmd)
	publishView(*viewState)
	latestFrame() *screenFrame
	reseed(build WorldBuilder)
	togglePause() bool
	needsReseed() bool
}

// step drains any queued UI commands, advances one tick, and auto-
// reseeds (preserving learning) on solve — all on the sim goroutine, so
// the world is never touched concurrently.
func (s *SimLoop) step() {
	s.drainCommands()
	if s.paused.Load() {
		// Frozen (pre-<space>): spawn agents so the map shows them at
		// their entrances; advance nothing else.
		s.world.RespawnAgents()
		return
	}
	s.world.Step()
	if s.world.MazeSolved() {
		_, _ = s.world.WriteStatsLog(StatsDir)
		s.world = reseedWorldPreservingLearning(s.world, s.build)
	}
	s.lastCycle.Store(int64(s.world.Cycle))
}

// drainCommands applies every queued UI mutation. A command returning a
// non-nil world swaps the live world (reseed); nil mutates in place.
func (s *SimLoop) drainCommands() {
	for {
		select {
		case fn := <-s.cmds:
			if nw := fn(s.world); nw != nil {
				s.world = nw
			}
		default:
			return
		}
	}
}

// maybePublishFrame renders and stores a composed screen at most every
// publishEvery. It waits until the UI has reported a real terminal size
// (a non-nil viewState with non-zero dims) so the sim never renders the
// full 1024×1024 board — only the on-screen viewport.
func (s *SimLoop) maybePublishFrame() {
	if time.Since(s.lastPublish) < s.publishEvery {
		return
	}
	vs := s.view.Load()
	if vs == nil || vs.termW == 0 || vs.termH == 0 {
		return
	}
	s.lastPublish = time.Now()
	s.frame.Store(s.renderFrame(vs))
}

// renderFrame composes the full screen for the UI's current view intent.
// Runs on the sim goroutine (sole world owner), so reading the world
// here is race-free. Reuses the Model renderers via a throwaway Model —
// one source of truth for layout, shared with sync (test) mode.
func (s *SimLoop) renderFrame(vs *viewState) *screenFrame {
	m := Model{
		World:    s.world,
		ShowPath: vs.showPath,
		termW:    vs.termW,
		termH:    vs.termH,
		offsetX:  vs.offsetX,
		offsetY:  vs.offsetY,
		paused:   s.paused.Load(),
	}
	text, rightW := m.composeScreen()
	return &screenFrame{text: text, rightW: rightW}
}
