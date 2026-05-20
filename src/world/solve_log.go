// solve_log.go — per-agent per-solve append log. Whenever an agent
// reaches the goal cell, CheckGoal calls AppendSolveRecord to write
// one NDJSON line to build/solves/agent<label>.log. The file is
// APPENDED (not truncated) so a long run accumulates the agent's
// entire solve history.
//
// Schema per line (one solve = one record):
//
//	{
//	  "run":           <Stats.GoalsReached after this solve>,
//	  "distance":      <ActualDistance walked this life>,
//	  "cycles":        <TicksAlive on this solve>,
//	  "score":         <AgentStats.Score(world.Cycle) snapshot>,
//	  "world_cycle":   <World.Cycle at solve>,
//	  "world_seed":    <World.Seed>
//	}
//
// Errors are best-effort — if the directory can't be created or the
// file can't be opened, the solve still counts in memory and the
// caller proceeds; nothing about the simulation depends on the log.
package world

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// SolveLogDir is the directory under which per-agent solve logs are
// appended. Defaults to "build/solves" — `make clean` wipes it.
var SolveLogDir = "build/solves"

// SolveLogRecord is the JSON-Lines record appended per solve.
type SolveLogRecord struct {
	Run        int     `json:"run"`
	Distance   int     `json:"distance"`
	Cycles     int     `json:"cycles"`
	Score      float64 `json:"score"`
	WorldCycle int     `json:"world_cycle"`
	WorldSeed  int64   `json:"world_seed"`
}

// appendSolveRecord writes one line of JSON to
// build/solves/agent<label>.log. Best-effort — errors are silently
// dropped because the simulation must not stall on disk problems.
//
// The append is gated on `SolveLogDir` already existing: production
// callers (cmd/main.go) create it at startup; test runs that don't
// touch the dir get a free no-op so they don't litter the package
// working directory with stray `build/solves/...` files.
func (w *World) appendSolveRecord(a *Agent) {
	info, err := os.Stat(SolveLogDir)
	if err != nil || !info.IsDir() {
		return
	}
	path := filepath.Join(SolveLogDir, fmt.Sprintf("agent%c.log", a.Label))
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	rec := SolveLogRecord{
		Run:        a.Stats.GoalsReached,
		Distance:   a.Stats.ActualDistance,
		Cycles:     a.TicksAlive,
		Score:      a.Stats.Score(w.Cycle),
		WorldCycle: w.Cycle,
		WorldSeed:  w.Seed,
	}
	_ = json.NewEncoder(f).Encode(rec) // newline already added
}
