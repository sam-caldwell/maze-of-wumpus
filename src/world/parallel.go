// parallel.go — experimental parallel agent execution.
//
// In the normal (serial) model, World.Step advances every agent exactly
// once per cycle on a single goroutine. This file adds an ALTERNATE
// driver where each agent (and its swarm group) runs on its OWN
// goroutine, free-running plan→move as fast as its strategy allows — so
// a cheap planner (QMDP) takes many steps in the wall-clock time a heavy
// one (POMCP) takes a few. That asymmetry is the whole point: it lets us
// observe the per-strategy throughput difference under real parallelism.
//
// Concurrency model (lock-free / partitioned, with a periodic barrier):
//
//   - Workers step under a shared READ lock; the coordinator runs the
//     globally-entangled bookkeeping (goal/death/respawn, scent merge,
//     cycle advance) under the WRITE lock. Go's RWMutex gives many
//     concurrent readers OR one exclusive writer — exactly a barrier:
//     while workers step they share RLock; when the coordinator wants in
//     it takes Lock, which waits for in-flight steps to finish and then
//     blocks new ones, pausing every worker for the bookkeeping pass.
//
//   - SCENT is the only cross-group shared state a step touches. Reads
//     hit the canonical grid directly — safe, because the only writer
//     (the coordinator) is paused whenever any worker holds RLock.
//     WRITES can't go to the shared grid (concurrent RLock holders would
//     race), so they buffer into the group's own a.scentBuf (via
//     PutScent) and the barrier flushes them. Scent is therefore
//     eventually-consistent: a deposit becomes visible to other groups
//     at the next barrier, not instantly.
//
//   - RNG is per-group (a.Rng, via AgentRng) so workers never race on
//     World.Rng. Cross-run determinism is gone in this mode — inherent
//     to asynchronous scheduling.
//
//   - AgentAt is not maintained between barriers (no worker reads it
//     there); the coordinator rebuilds it from positions before running
//     the lifecycle, which needs a consistent spatial index.
//
// The serial path (World.Step / MoveAgents) is completely unchanged;
// w.parallel is false there, so PutScent/AgentRng behave exactly as
// before. This driver is opt-in (cmd --parallel).
package world

import (
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"
)

// scentDeposit is one buffered scent stamp accumulated by a group during
// a parallel window and flushed into the canonical grid at the barrier.
type scentDeposit struct {
	x, y  int
	label rune
	cycle int
}

// PutScent records a scent stamp for agent a at (x, y). Serial mode
// writes the canonical grid directly (unchanged). Parallel mode appends
// to the group's buffer (a is the group owner / swarm leader) so
// concurrent workers never write the shared grid. Exported so the
// strategy package's clone-move path routes through it too.
func (w *World) PutScent(a *Agent, x, y int) {
	if w.parallel {
		a.scentBuf = append(a.scentBuf, scentDeposit{x: x, y: y, label: a.Label, cycle: w.Cycle})
		return
	}
	w.ScentOwner[y][x] = a.Label
	w.ScentCycle[y][x] = w.Cycle
}

// AgentRng returns the random source strategy/move code should draw from
// for agent a: the group's private Rng in parallel mode (so workers
// never race on World.Rng), else the shared World.Rng (serial —
// unchanged, preserving serial determinism).
func (w *World) AgentRng(a *Agent) *rand.Rand {
	if a.Rng != nil {
		return a.Rng
	}
	return w.Rng
}

// stepGroupParallel runs ONE step for agent a (and, for swarm leaders,
// its clones via the strategy wrapper). It mirrors the per-agent body of
// MoveAgents, with three parallel-mode adaptations: scent deposits route
// through PutScent (buffered), AgentAt is left to the barrier, and a TTL
// death / goal arrival is reported as `terminal` instead of being
// resolved inline (the coordinator handles the entangled lifecycle).
//
// Runs on the group's worker goroutine while holding the barrier RLock,
// so it only ever reads shared state (scent grid, immutable maze /
// distance fields) and mutates this group's own state.
func (w *World) stepGroupParallel(a *Agent) (terminal bool) {
	// Count every step attempt (moved or not), mirroring tickAgentClocks
	// which bumps TicksAlive once per serial tick. ParSteps is the
	// lifetime rate counter surfaced in the report.
	a.TicksAlive++
	a.ParSteps++

	var target Pos
	if a.CurrentStrategy != 0 && w.strategyForLetter != nil {
		if s := w.strategyForLetter(a.CurrentStrategy); s != nil {
			target = s(w, a)
		} else {
			target = a.Pos
		}
	} else if a.Strategy != nil {
		target = a.Strategy(w, a)
	} else {
		target = a.Pos
	}
	if !w.CanMoveTo(a, target) {
		if a.SearchAnim != nil {
			return w.groupAtGoal(a) // frozen mid-animation; no move this step
		}
		target = w.FallbackMove(a)
	}
	if target == a.Pos {
		return w.groupAtGoal(a)
	}

	oldPos := a.Pos
	w.PutScent(a, a.Pos.X, a.Pos.Y) // deposit at the cell being left (buffered)
	a.Pos = target
	w.MarkAgentSensed(a)
	a.Stats.ActualDistance++
	// (No DecisionLog here: it writes a shared sink and isn't goroutine-
	//  safe. Parallel mode runs with DecisionLogEnabled off.)
	if w.ShortestPathCells[a.Pos] {
		a.Stats.OnPathSteps++
	} else {
		a.Stats.OffPathSteps++
	}
	if a.Visited == nil {
		a.Visited = map[Pos]bool{}
	}
	if a.LifetimeVisited == nil {
		a.LifetimeVisited = map[Pos]bool{}
	}
	if !a.Visited[a.Pos] {
		a.Visited[a.Pos] = true
		if a.LifetimeVisited[a.Pos] {
			if a.KnownPathRewarded == nil {
				a.KnownPathRewarded = map[Pos]bool{}
			}
			if !a.KnownPathRewarded[a.Pos] {
				a.PendingBonus += KnownPathReward
				a.KnownPathRewarded[a.Pos] = true
			}
		} else {
			a.PendingBonus += ExplorationBonus
			a.LifetimeVisited[a.Pos] = true
		}
	}
	if a.HasLastFrom && a.Pos == a.LastFromCell {
		a.PendingBonus -= BackStepPenalty
	}
	curStartDist := w.DistFromStart[a.Pos.Y][a.Pos.X]
	if curStartDist > a.MaxStartDist {
		a.PendingBonus += float64(curStartDist-a.MaxStartDist) * RealDistanceShaping
		a.MaxStartDist = curStartDist
	}
	if curStartDist > a.Stats.MaxReach {
		a.Stats.MaxReach = curStartDist // persists across lives; funds TTL commute credit
	}
	walkables := 0
	for _, dd := range Cardinals {
		np := Pos{a.Pos.X + dd.X, a.Pos.Y + dd.Y}
		if w.Maze.IsWalkable(np) {
			walkables++
		}
	}
	if walkables == 1 {
		if a.LastDeadEndCycle != 0 && w.Cycle-a.LastDeadEndCycle > DeadEndWindow {
			a.DeadEndCount = 0
		}
		exp := a.DeadEndCount
		if exp > DeadEndExpCap {
			exp = DeadEndExpCap
		}
		a.PendingBonus -= float64(int(1) << exp)
		a.DeadEndCount++
		a.LastDeadEndCycle = w.Cycle
	}
	a.LastFromCell = oldPos
	a.HasLastFrom = true

	ceiling := w.TTLCeiling(a)
	ttlDead := !w.TTLDisabled && ceiling > 0 && a.Stats.ActualDistance > ceiling
	return ttlDead || w.groupAtGoal(a)
}

// groupAtGoal reports whether the group's leader or any alive clone is
// standing on the goal cell — the signal to park the group so the
// position is still there when the barrier's CheckGoal records the win.
func (w *World) groupAtGoal(a *Agent) bool {
	if a.Pos == w.Maze.GoalPos {
		return true
	}
	for _, c := range a.SwarmClones {
		if c != nil && c.Alive && c.Pos == w.Maze.GoalPos {
			return true
		}
	}
	return false
}

// ParallelRunner drives a world with one worker goroutine per agent plus
// a coordinator that runs the global lifecycle at a fixed barrier
// cadence. Construct with NewParallelRunner and drive with Run.
type ParallelRunner struct {
	w            *World
	barrier      sync.RWMutex // RLock = step, Lock = global bookkeeping
	barrierEvery time.Duration
	stop         chan struct{}
	barriersDone chan struct{}
	wg           sync.WaitGroup
	occupied     []Pos // cells set in AgentAt last rebuild, to clear next
	barriers     int64
	start        time.Time

	// OnBarrier, if set, is invoked at the END of every barrier with the
	// world stable (write lock held, all workers paused). The TUI uses it
	// to render a frame from a consistent snapshot. Must not spawn work
	// that re-enters the runner.
	OnBarrier func(*World)

	// cmds carries world mutations posted by a UI driver, drained and
	// applied at the start of each barrier (under the write lock) so the
	// world is only ever mutated with workers paused.
	cmds chan func(*World)

	// paused, when true, freezes the simulation: workers don't step and
	// the barrier advances nothing — it only spawns agents (so the map
	// renders) and re-renders. Toggled by the UI ('space'). Read by
	// worker goroutines, written by the UI goroutine → atomic.
	paused atomic.Bool

	// solved latches once the maze-solved condition is met at a barrier
	// (enough agents at the goal-count threshold). A UI driver polls it
	// (Solved) to trigger an auto-reseed, since the reseed must stop and
	// restart the workers — which can't be done from inside the barrier.
	solved atomic.Bool
}

// SetPaused freezes (true) or resumes (false) the simulation.
func (pr *ParallelRunner) SetPaused(p bool) { pr.paused.Store(p) }

// Paused reports whether the simulation is currently frozen.
func (pr *ParallelRunner) Paused() bool { return pr.paused.Load() }

// Solved reports whether the maze-solved condition has been reached since
// the runner started — the cue for a UI driver to auto-reseed.
func (pr *ParallelRunner) Solved() bool { return pr.solved.Load() }

// NewParallelRunner wraps w for parallel execution with the given barrier
// cadence (how often the global lifecycle/scent-merge pass runs).
func NewParallelRunner(w *World, barrierEvery time.Duration) *ParallelRunner {
	return &ParallelRunner{
		w:            w,
		barrierEvery: barrierEvery,
		cmds:         make(chan func(*World), 64),
	}
}

// World returns the runner's world. Safe to call from OnBarrier (the
// world is stable there); otherwise the caller must not touch mutable
// world state concurrently with the workers.
func (pr *ParallelRunner) World() *World { return pr.w }

// Post queues a world mutation to run at the next barrier (workers
// paused). Non-blocking; drops if the buffer is full (not reachable at
// human keypress rates).
func (pr *ParallelRunner) Post(fn func(*World)) {
	select {
	case pr.cmds <- fn:
	default:
	}
}

// AgentRate is the per-agent throughput result of a parallel run.
type AgentRate struct {
	Label    rune
	Strategy rune
	Steps    int64 // ParSteps taken during the run
	Goals    int
	Deaths   int
}

// ParallelReport summarizes a Run.
type ParallelReport struct {
	Wall     time.Duration
	Barriers int64
	Agents   []AgentRate
}

// TotalSteps is the aggregate step count across all agents.
func (r ParallelReport) TotalSteps() int64 {
	var t int64
	for _, a := range r.Agents {
		t += a.Steps
	}
	return t
}

// String renders a human-readable per-agent rate table.
func (r ParallelReport) String() string {
	secs := r.Wall.Seconds()
	out := fmt.Sprintf("parallel: %.2fs wall, %d barriers, %d total steps (%.0f steps/s)\n",
		secs, r.Barriers, r.TotalSteps(), float64(r.TotalSteps())/secs)
	for _, a := range r.Agents {
		out += fmt.Sprintf("  agent %c [%c]: %8d steps  %7.0f steps/s  goals:%d deaths:%d\n",
			a.Label, a.Strategy, a.Steps, float64(a.Steps)/secs, a.Goals, a.Deaths)
	}
	return out
}

// Start flips the world into parallel mode, seeds a private RNG per
// group, and launches a worker per agent plus the barrier coordinator —
// returning immediately. Use for an open-ended (e.g. TUI-driven) run;
// pair with Stop. For a fixed-duration measurement use Run.
func (pr *ParallelRunner) Start() {
	w := pr.w
	w.parallel = true
	pr.solved.Store(false)
	// Per-group private RNGs, seeded from the world seed so the SEEDING
	// is reproducible even though the interleaving is not.
	for i, a := range w.Agents {
		a.Rng = rand.New(rand.NewSource(w.Seed + int64(i+1)*0x9E3779B1))
		a.ParSteps = 0
		a.parked = false
		a.scentBuf = a.scentBuf[:0]
	}
	pr.stop = make(chan struct{})
	pr.barriersDone = make(chan struct{})
	pr.start = time.Now()
	for _, a := range w.Agents {
		pr.wg.Add(1)
		go pr.worker(a)
	}
	go func() { defer close(pr.barriersDone); pr.runBarriers() }()
}

// Stop halts all workers and the coordinator, waits for them to exit, and
// restores serial mode.
func (pr *ParallelRunner) Stop() {
	close(pr.stop)
	pr.wg.Wait()
	<-pr.barriersDone
	pr.w.parallel = false
}

// Run executes the world in parallel for duration d and returns the
// per-agent throughput — the headless measurement path. Safe to call on
// a freshly-built world.
func (pr *ParallelRunner) Run(d time.Duration) ParallelReport {
	w := pr.w
	startGoals := make(map[rune]int, len(w.Agents))
	startDeaths := make(map[rune]int, len(w.Agents))
	for _, a := range w.Agents {
		startGoals[a.Label] = a.Stats.GoalsReached
		startDeaths[a.Label] = a.Stats.Deaths
	}

	pr.Start()
	time.Sleep(d)
	wall := time.Since(pr.start)
	pr.Stop()

	rates := make([]AgentRate, 0, len(w.Agents))
	for _, a := range w.Agents {
		rates = append(rates, AgentRate{
			Label:    a.Label,
			Strategy: a.CurrentStrategy,
			Steps:    a.ParSteps,
			Goals:    a.Stats.GoalsReached - startGoals[a.Label],
			Deaths:   a.Stats.Deaths - startDeaths[a.Label],
		})
	}
	return ParallelReport{Wall: wall, Barriers: pr.barriers, Agents: rates}
}

// worker free-runs steps for one group until stopped, pausing whenever
// the coordinator holds the barrier write lock, and idling briefly when
// the group is dead/parked so it doesn't busy-spin.
func (pr *ParallelRunner) worker(a *Agent) {
	defer pr.wg.Done()
	for {
		select {
		case <-pr.stop:
			return
		default:
		}
		stepped := false
		pr.barrier.RLock()
		if !pr.paused.Load() && a.Alive && !a.Disabled && !a.parked {
			if pr.w.stepGroupParallel(a) {
				a.parked = true
			}
			stepped = true
		}
		pr.barrier.RUnlock()
		if !stepped {
			time.Sleep(time.Millisecond) // paused/dead/parked: wait for the barrier
		}
	}
}

// runBarriers fires the global bookkeeping pass at the barrier cadence
// until stopped.
func (pr *ParallelRunner) runBarriers() {
	t := time.NewTicker(pr.barrierEvery)
	defer t.Stop()
	for {
		select {
		case <-pr.stop:
			return
		case <-t.C:
			pr.barrier.Lock()
			pr.commitBarrier()
			pr.barrier.Unlock()
		}
	}
}

// commitBarrier runs the globally-entangled bookkeeping with all workers
// paused (write lock held): flush buffered scent, rebuild the spatial
// index, apply TTL kills, record goal wins, and respawn — reusing the
// existing serial lifecycle functions verbatim, since here they are once
// again the sole mutator of the world.
func (pr *ParallelRunner) commitBarrier() {
	w := pr.w
	pr.barriers++

	// 0. Apply any UI-posted world mutations (toggles, etc.) now that we
	//    hold the write lock and every worker is paused.
	pr.drainCommands()

	// Frozen (pre-<space>): spawn agents so the map shows them at their
	// entrances, refresh the spatial index, and render — but advance
	// nothing (no cycle, movement, scent, or lifecycle progression).
	if pr.paused.Load() {
		w.RespawnAgents()
		pr.rebuildAgentAt()
		if pr.OnBarrier != nil {
			pr.OnBarrier(w)
		}
		return
	}

	w.Cycle++

	// 1. Flush each group's buffered scent into the canonical grid.
	for _, a := range w.Agents {
		for _, d := range a.scentBuf {
			w.ScentOwner[d.y][d.x] = d.label
			w.ScentCycle[d.y][d.x] = d.cycle
		}
		a.scentBuf = a.scentBuf[:0]
	}

	// 2. Make AgentAt match worker-moved positions so the lifecycle
	//    functions (which read/maintain it) see correct occupancy.
	pr.rebuildAgentAt()

	// 3. TTL kills — the inline serial check, applied once per barrier.
	if !w.TTLDisabled {
		for _, a := range w.Agents {
			if !a.Alive || a.Disabled {
				continue
			}
			if ceiling := w.TTLCeiling(a); ceiling > 0 && a.Stats.ActualDistance > ceiling {
				w.KillAgent(a, "ttl")
			}
		}
	}

	// 4. Goal wins + respawns (the entangled global passes, unchanged).
	w.CheckGoal()
	w.RespawnAgents()

	// 5. Re-sync the spatial index after the lifecycle moved/placed
	//    agents (goal snaps, clone promotions, respawn placements) and
	//    record the final occupied set for the next barrier's clear.
	pr.rebuildAgentAt()

	// 6. Unpark every alive group so its worker resumes next window.
	for _, a := range w.Agents {
		if a.Alive && !a.Disabled {
			a.parked = false
		}
	}

	// 7. Refresh each group's throughput (steps/sec, averaged since the
	//    run started) for the stats-board "rt:" column.
	if el := time.Since(pr.start).Seconds(); el > 0 {
		for _, a := range w.Agents {
			a.StepsPerSec = float64(a.ParSteps) / el
		}
	}

	// 8. Latch the maze-solved condition so a UI driver can auto-reseed
	//    (it can't reseed from in here — that stops this very goroutine).
	if w.MazeSolved() {
		pr.solved.Store(true)
	}

	// 9. Let a UI driver render a frame from the now-stable world.
	if pr.OnBarrier != nil {
		pr.OnBarrier(w)
	}
}

// drainCommands applies every queued UI mutation under the barrier write
// lock (workers paused), so the world stays single-writer.
func (pr *ParallelRunner) drainCommands() {
	for {
		select {
		case fn := <-pr.cmds:
			fn(pr.w)
		default:
			return
		}
	}
}

// rebuildAgentAt clears the cells it set last time and repopulates the
// spatial index from the agents' current positions, recording the new
// occupied set. Clones are not indexed in AgentAt (the renderer/lifecycle
// read them straight from SwarmClones), matching serial behavior.
func (pr *ParallelRunner) rebuildAgentAt() {
	w := pr.w
	for _, p := range pr.occupied {
		w.AgentAt[p.Y][p.X] = nil
	}
	pr.occupied = pr.occupied[:0]
	for _, a := range w.Agents {
		if a.Alive && !a.Disabled {
			w.AgentAt[a.Pos.Y][a.Pos.X] = a
			pr.occupied = append(pr.occupied, a.Pos)
		}
	}
}
