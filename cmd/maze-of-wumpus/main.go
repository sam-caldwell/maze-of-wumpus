// Package main: Maze of Wumpus — terminal-UI maze game.
//
// On launch a 120x80 maze is procedurally generated. Five agents
// (A..E) each run a distinct decision strategy and race for the goal.
//
// Two execution modes:
//
//   - Default (no flags) — bubbletea TUI.
//   - --headless          — text-only stdout-per-cycle output for
//     scripted tests. With --seed=N the run is bit-for-bit
//     reproducible.
//
// Controls (TUI only):
//
//	q          — quit
//	ctrl+c     — quit
//	r          — reseed and restart
//	s          — toggle shortest-path overlay
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"maze-of-wumpus/src/strategy"
	"maze-of-wumpus/src/tui"
	"maze-of-wumpus/src/world"
	"maze-of-wumpus/src/wumpus"
)

// buildWorld constructs a world with the full strategy configuration
// for both agents and wumpus. Used at launch and by the TUI's reseed.
func buildWorld(seed int64) *world.World {
	return world.NewWorldWithConfig(world.Config{
		Seed:                         seed,
		StrategyFor:                  strategy.ForLabel,
		StrategyForLetter:            strategy.ForLetter,
		StrategyLetters:              strategy.StrategyLetters,
		StrategyDescriptionForLetter: strategy.DescriptionByLetter,
		WumpusStrategy:               wumpus.PickStrategy,
		VengeanceStrategy:            wumpus.ScentStrategy,
	})
}

// teaRunner runs a tea.Program. Indirected so tests can swap a stub
// (running tea.NewProgram requires a TTY which test binaries lack).
var teaRunner = func(m tui.Model) error {
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}

// runProgram is the indirection that hides the bubbletea call from
// tests. Tests swap in a stub.
var runProgram = func(seed int64) error {
	ensureLogDirs()
	m := tui.NewModel(seed, buildWorld)
	// On macOS, kick off a non-blocking 'say' announcement that
	// overlaps with the first TUI render. No-op on every other OS.
	announce()
	return teaRunner(m)
}

// runApp parses CLI args and dispatches to either the TUI or the
// headless writer. Returns the process exit code.
func runApp(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("maze-of-wumpus", flag.ContinueOnError)
	fs.SetOutput(stderr)
	seedFlag := fs.Int64("seed", 0, "rng seed (0 = use current time)")
	headless := fs.Bool("headless", false, "run without TUI, write one state line per cycle to stdout")
	steps := fs.Int("steps", 200, "headless: number of ticks to run")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	seed := *seedFlag
	if seed == 0 {
		seed = time.Now().UnixNano()
	}
	if *headless {
		runHeadless(seed, *steps, stdout)
		return 0
	}
	if err := runProgram(seed); err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	return 0
}

// runHeadless drives the simulation without UI: one line per tick on
// stdout.
func runHeadless(seed int64, steps int, stdout io.Writer) {
	ensureLogDirs()
	runHeadlessLoop(buildWorld(seed), steps, stdout)
}

// ensureLogDirs creates the directories World.WriteStatsLog and
// World.appendSolveRecord write to. Production callers (TUI and
// headless) invoke it once at startup; test runs skip it so they
// don't litter the package working directory with stray logs.
func ensureLogDirs() {
	_ = os.MkdirAll(tui.StatsDir, 0755)
	_ = os.MkdirAll(world.SolveLogDir, 0755)
}

// runHeadlessLoop is the inner loop, split out so tests can poke at
// the early-exit-on-goal branch with a synthetic World. When the
// maze is solved mid-loop, the loop snapshots stats and auto-
// reseeds preserving each agent's learning state — same behavior
// as the TUI's tick handler.
func runHeadlessLoop(w *world.World, steps int, stdout io.Writer) {
	writeHeadlessState(stdout, w)
	for i := 0; i < steps; i++ {
		w.Step()
		writeHeadlessState(stdout, w)
		if w.GameOver {
			return
		}
		if w.MazeSolved() {
			_, _ = w.WriteStatsLog(tui.StatsDir)
			w = reseedHeadless(w)
			writeHeadlessState(stdout, w)
		}
	}
}

// reseedHeadless builds a fresh world for headless mode preserving
// each agent's Beliefs / QL / DQN / TrustScores. Mirrors the TUI
// Model helper — trust updates fire per-journey from KillAgent /
// CheckGoal, not at reseed.
func reseedHeadless(prevWorld *world.World) *world.World {
	prev := prevWorld.Agents
	w := buildWorld(time.Now().UnixNano())
	for i, oldA := range prev {
		if i >= len(w.Agents) {
			break
		}
		newA := w.Agents[i]
		if oldA.Beliefs != nil {
			newA.Beliefs = oldA.Beliefs
		}
		if oldA.DQN != nil {
			newA.DQN = oldA.DQN
			newA.DQN.HasPending = false
		}
		if oldA.TrustScores != nil {
			newA.TrustScores = oldA.TrustScores
		}
		// Carry the prior map's LearnedTTL forward as a prior
		// belief — invalidation in MoveAgents drops it if stale.
		newA.LearnedTTL = oldA.LearnedTTL
	}
	return w
}

// writeHeadlessState emits one space-separated key=value record per
// cycle, with per-agent fields for A..E.
func writeHeadlessState(out io.Writer, w *world.World) {
	aliveWumpus := 0
	for _, wm := range w.Wumpus {
		if wm.Alive {
			aliveWumpus++
		}
	}
	fmt.Fprintf(out, "cycle=%d wumpus_died=%d wumpus_alive=%d optimal=%d paths=%d",
		w.Cycle, w.Stats.WumpusDied, aliveWumpus,
		w.Stats.OptimalDistance, w.Stats.ShortestPaths)
	for _, a := range w.Agents {
		fmt.Fprintf(out,
			" %c_alive=%v %c_deaths=%d %c_kills=%d %c_goals=%d %c_dist=%d %c_score=%.3f",
			a.Label, a.Alive,
			a.Label, a.Stats.Deaths,
			a.Label, a.Stats.WumpusKilled,
			a.Label, a.Stats.GoalsReached,
			a.Label, a.Stats.ActualDistance,
			a.Label, a.Stats.Score(w.Cycle),
		)
	}
	fmt.Fprintf(out, " game_over=%v\n", w.GameOver)
}

var exitFunc = os.Exit

func main() { exitFunc(runApp(os.Args[1:], os.Stdout, os.Stderr)) }
