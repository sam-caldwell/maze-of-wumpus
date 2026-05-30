// simloop.go — decouples simulation from rendering. In the live app the
// simulation advances on its own goroutine (SimLoop) while the bubbletea
// UI renders the latest state on its own cadence, so a slow Step() can
// never freeze input or rendering. Under test there is no SimLoop and
// the Model steps synchronously in Update (deterministic, single-
// goroutine), so the existing suite is unaffected.
package tui

import (
	"sync"
	"time"

	"maze-of-wumpus/src/world"
)

// renderInterval is how often the TUI repaints in async mode. Kept
// short for input responsiveness; independent of how long a sim Step
// actually takes.
const renderInterval = 50 * time.Millisecond

// SimLoop owns the live *world.World and advances it on a background
// goroutine. All access to the world (the loop's own Step, plus the
// UI's reads and key-driven mutations) is serialized by mu, so the
// world itself stays single-threaded even though sim and UI run on
// separate goroutines.
type SimLoop struct {
	mu    sync.RWMutex
	world *world.World
	build WorldBuilder

	interval time.Duration
	stop     chan struct{}
	stopped  chan struct{}

	// stats is the listener the sim publishes rendered stat frames to;
	// statsEvery throttles publishing to roughly the render cadence so
	// the sim isn't formatting stats on every fast step.
	stats       *StatsAggregator
	statsEvery  time.Duration
	lastPublish time.Time
}

// NewSimLoop wraps a world for background stepping at the given cadence.
func NewSimLoop(w *world.World, build WorldBuilder, interval time.Duration) *SimLoop {
	return &SimLoop{
		world:      w,
		build:      build,
		interval:   interval,
		stop:       make(chan struct{}),
		stopped:    make(chan struct{}),
		stats:      NewStatsAggregator(),
		statsEvery: renderInterval,
	}
}

// Start launches the background stepping goroutine. The ticker drops
// ticks when a Step runs long, so the sim free-runs (bounded by Step
// time) when slow and paces at `interval` when fast.
func (s *SimLoop) Start() {
	s.stats.Start()
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
				s.maybePublishStats()
			}
		}
	}()
}

// Stop halts the sim and stats listener goroutines and waits for exit.
func (s *SimLoop) Stop() {
	close(s.stop)
	<-s.stopped
	s.stats.Stop()
}

// maybePublishStats renders and publishes a stats frame at most every
// statsEvery, under the read lock (it reads world stats), so the sim
// stays lean and the UI never formats stats itself.
func (s *SimLoop) maybePublishStats() {
	now := time.Now()
	if now.Sub(s.lastPublish) < s.statsEvery {
		return
	}
	s.lastPublish = now
	s.mu.RLock()
	frame := captureStatsFrame(s.world)
	s.mu.RUnlock()
	s.stats.publish(frame)
}

// step advances one tick under the write lock, auto-reseeding (with
// learning preserved) when the maze is solved — mirroring the Model's
// synchronous tick path.
func (s *SimLoop) step() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.world.Step()
	if s.world.MazeSolved() {
		_, _ = s.world.WriteStatsLog(StatsDir)
		s.world = reseedWorldPreservingLearning(s.world, s.build)
	}
}

// read runs fn under the read lock with the current world — used by the
// UI to render a consistent frame without blocking concurrent reads.
func (s *SimLoop) read(fn func(*world.World)) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	fn(s.world)
}

// write runs fn under the write lock and adopts whatever world fn
// leaves behind (so a UI-driven reseed swaps the live world atomically).
func (s *SimLoop) write(fn func(*world.World) *world.World) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if nw := fn(s.world); nw != nil {
		s.world = nw
	}
}
