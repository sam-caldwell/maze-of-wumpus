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

	"maze-of-wumpus/src/logging"
	"maze-of-wumpus/src/strategy"
	"maze-of-wumpus/src/tui"
	"maze-of-wumpus/src/world"
	"maze-of-wumpus/src/wumpus"
)

// LogDir: relative path where per-agent NDJSON logs are written.
const LogDir = "build/logs"

// buildWorld constructs a world with the full strategy configuration
// for both agents and wumpus. Used at launch and by the TUI's reseed.
func buildWorld(seed int64) *world.World {
	return world.NewWorldWithConfig(world.Config{
		Seed:              seed,
		StrategyFor:       strategy.ForLabel,
		WumpusStrategy:    wumpus.PickStrategy,
		VengeanceStrategy: wumpus.ScentStrategy,
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
	logger := logging.NewAgentLogger(LogDir)
	logger.SetStrategyNamer(strategy.Name)
	defer logger.Close()
	m := tui.NewModel(seed, buildWorld)
	m.Logger = logger
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
// stdout AND one JSON record per cycle per agent.
func runHeadless(seed int64, steps int, stdout io.Writer) {
	logger := logging.NewAgentLogger(LogDir)
	logger.SetStrategyNamer(strategy.Name)
	defer logger.Close()
	runHeadlessLoop(buildWorld(seed), steps, stdout, logger)
}

// runHeadlessLoop is the inner loop, split out so tests can poke at
// the early-exit-on-goal branch with a synthetic World. `logger` may
// be nil (the LogTick call is nil-safe).
func runHeadlessLoop(w *world.World, steps int, stdout io.Writer, logger *logging.AgentLogger) {
	writeHeadlessState(stdout, w)
	logger.LogTick(w)
	for i := 0; i < steps; i++ {
		w.Step()
		writeHeadlessState(stdout, w)
		logger.LogTick(w)
		if w.GameOver {
			return
		}
	}
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
			a.Label, a.Stats.Score(w.Stats.OptimalDistance),
		)
	}
	fmt.Fprintf(out, " game_over=%v\n", w.GameOver)
}

var exitFunc = os.Exit

func main() { exitFunc(runApp(os.Args[1:], os.Stdout, os.Stderr)) }
