// stats.go — decouples stats collection from stats rendering. The
// simulation publishes a rendered stats frame (header, trust/info
// panel lines, per-agent + status footer) over a channel; a listener
// goroutine aggregates the stream into the latest frame; the UI reads
// that frame and composes it with the live maze pane. Stat formatting
// and the deep stat reads thus leave the UI goroutine entirely, and
// the UI only briefly locks the world for the maze viewport.
package tui

import (
	"strings"
	"sync/atomic"

	"maze-of-wumpus/src/world"
)

// StatsFrame is one published snapshot of the rendered stat panes.
// Strings are pre-rendered so the UI just splices them — no world
// access for stats on the render path.
type StatsFrame struct {
	header     string
	rightLines []string
	bottom     string
}

// captureStatsFrame renders the stat panes from `w`. Called by the sim
// goroutine under the read lock (it reads world stats); never on the
// UI goroutine. Reuses the existing renderers via a throwaway Model so
// there's a single source of truth for stat formatting.
func captureStatsFrame(w *world.World) *StatsFrame {
	m := Model{World: w}
	return &StatsFrame{
		header:     m.renderHeader(),
		rightLines: renderTrustMatrixLines(w),
		bottom:     m.renderBottomPane(),
	}
}

func (f *StatsFrame) right() string { return strings.Join(f.rightLines, "\n") }

// StatsAggregator is the listener: it receives published frames on a
// channel and keeps the most recent in an atomic pointer the UI loads
// lock-free. (Aggregation here is latest-wins; the world already keeps
// the cumulative counters/averages the frames render.)
type StatsAggregator struct {
	ch      chan *StatsFrame
	latest  atomic.Pointer[StatsFrame]
	stop    chan struct{}
	stopped chan struct{}
}

// NewStatsAggregator creates an aggregator with a single-slot inbox
// (publishers drop-and-replace, so the listener always converges on
// the freshest frame without blocking the sim).
func NewStatsAggregator() *StatsAggregator {
	return &StatsAggregator{
		ch:      make(chan *StatsFrame, 1),
		stop:    make(chan struct{}),
		stopped: make(chan struct{}),
	}
}

// Start launches the listener goroutine.
func (a *StatsAggregator) Start() {
	go func() {
		defer close(a.stopped)
		for {
			select {
			case <-a.stop:
				return
			case f := <-a.ch:
				a.latest.Store(f)
			}
		}
	}()
}

// publish hands a frame to the listener without blocking: if the inbox
// is full, the stale frame is dropped and replaced with the newer one.
func (a *StatsAggregator) publish(f *StatsFrame) {
	for {
		select {
		case a.ch <- f:
			return
		default:
			select {
			case <-a.ch: // drop a stale frame, retry
			default:
			}
		}
	}
}

// Latest returns the freshest aggregated frame, or nil before the first
// publish.
func (a *StatsAggregator) Latest() *StatsFrame { return a.latest.Load() }

// Stop halts the listener and waits for it to exit.
func (a *StatsAggregator) Stop() {
	close(a.stop)
	<-a.stopped
}
