// logger.go — per-agent JSON Lines (NDJSON) log writer.
//
// One file per agent label, written into a logs directory
// (build/logs by default):
//
//	a.log, b.log, c.log, d.log, e.log
//
// Each file gets one line per cycle per agent — a single
// AgentLogRecord, JSON-encoded with a trailing newline. The schema
// captures position, alive flag, strategy-specific state, reward-
// shaping signals, and lifetime stats.
//
// All operations are nil-safe.
package logging

import (
	"encoding/json"
	"os"
	"path/filepath"
	"unicode"

	"maze-of-wumpus/src/world"
)

// StrategyNamer maps an agent label to its strategy name. The logger
// uses this for the per-record "strategy" field. cmd/main.go wires
// in strategy.Name; tests can pass nil (records get "unknown").
type StrategyNamer func(rune) string

// AgentLogger holds one open file + json.Encoder per agent label.
type AgentLogger struct {
	files      map[rune]*os.File
	encoders   map[rune]*json.Encoder
	strategyOf StrategyNamer
}

// NewAgentLogger opens five log files in `dir` (one per agent
// a/b/c/d/e), truncating any existing file. The directory is created
// if it doesn't already exist. Each game launch starts with empty
// logs.
func NewAgentLogger(dir string) *AgentLogger {
	al := &AgentLogger{
		files:    map[rune]*os.File{},
		encoders: map[rune]*json.Encoder{},
	}
	_ = os.MkdirAll(dir, 0755)
	for _, label := range []rune{'1', '2', '3', '4', '5', '6', '7'} {
		path := filepath.Join(dir, string(label)+".log")
		f, err := os.Create(path)
		if err != nil {
			continue
		}
		al.files[label] = f
		al.encoders[label] = json.NewEncoder(f)
	}
	return al
}

// SetStrategyNamer attaches a callback used to populate the "strategy"
// field of each record. Safe to call on nil.
func (al *AgentLogger) SetStrategyNamer(fn StrategyNamer) {
	if al == nil {
		return
	}
	al.strategyOf = fn
}

// Close flushes and closes every log file. Safe to call on nil.
func (al *AgentLogger) Close() {
	if al == nil {
		return
	}
	for _, f := range al.files {
		_ = f.Close()
	}
}

// AgentLogRecord is the JSON schema written per (cycle, agent).
type AgentLogRecord struct {
	Cycle           int       `json:"cycle"`
	Label           string    `json:"label"`
	Strategy        string    `json:"strategy"`
	Alive           bool      `json:"alive"`
	Pos             [2]int    `json:"pos"`
	TicksAlive      int       `json:"ticks_alive"`
	Water           int       `json:"water"`
	PlanLen         int       `json:"plan_len"`
	DeadEndCount    int       `json:"dead_end_count"`
	LastFromCell    [2]int    `json:"last_from_cell"`
	HasLastFrom     bool      `json:"has_last_from"`
	PendingBonus    float64   `json:"pending_bonus"`
	Deaths          int       `json:"deaths"`
	WumpusKilled    int       `json:"wumpus_killed"`
	GoalsReached    int       `json:"goals_reached"`
	ActualDistance  int       `json:"actual_distance"`
	BestSolveDist   int       `json:"best_solve_dist"`
	BestSolveTime   int       `json:"best_solve_time"`
	LastDeath       string    `json:"last_death,omitempty"`
	Score           float64   `json:"score"`
	BeliefsSize     int       `json:"beliefs_size,omitempty"`
	QTableSize      int       `json:"q_table_size,omitempty"`
	DqnQValues      []float64 `json:"dqn_q,omitempty"`
	LifetimeVisited int       `json:"lifetime_visited_cells"`
	OptimalDist     int       `json:"optimal_distance"`
}

// LogTick writes one record for each agent in `w`. Nil-safe.
func (al *AgentLogger) LogTick(w *world.World) {
	if al == nil {
		return
	}
	for _, a := range w.Agents {
		label := unicode.ToLower(a.Label)
		enc, ok := al.encoders[label]
		if !ok {
			continue
		}
		_ = enc.Encode(al.buildRecord(w, a))
	}
}

// buildRecord assembles the per-agent snapshot. For agent E we also
// run the DQN's forward pass so the log captures the Q-value vector
// the agent saw this tick.
func (al *AgentLogger) buildRecord(w *world.World, a *world.Agent) AgentLogRecord {
	strategy := "unknown"
	if al.strategyOf != nil {
		strategy = al.strategyOf(a.Label)
	}
	rec := AgentLogRecord{
		Cycle:           w.Cycle,
		Label:           string(a.Label),
		Strategy:        strategy,
		Alive:           a.Alive,
		Pos:             [2]int{a.Pos.X, a.Pos.Y},
		TicksAlive:      a.TicksAlive,
		Water:           a.Water,
		PlanLen:         len(a.Plan),
		DeadEndCount:    a.DeadEndCount,
		LastFromCell:    [2]int{a.LastFromCell.X, a.LastFromCell.Y},
		HasLastFrom:     a.HasLastFrom,
		PendingBonus:    a.PendingBonus,
		Deaths:          a.Stats.Deaths,
		WumpusKilled:    a.Stats.WumpusKilled,
		GoalsReached:    a.Stats.GoalsReached,
		ActualDistance:  a.Stats.ActualDistance,
		BestSolveDist:   a.Stats.BestSolveDistance,
		BestSolveTime:   a.Stats.BestSolveTime,
		LastDeath:       a.Stats.LastDeathReason,
		Score:           a.Stats.Score(w.Stats.OptimalDistance),
		LifetimeVisited: len(a.LifetimeVisited),
		OptimalDist:     w.Stats.OptimalDistance,
	}
	if a.Beliefs != nil {
		rec.BeliefsSize = len(a.Beliefs.SafeFromPit)
	}
	if a.QL != nil {
		rec.QTableSize = len(a.QL.Q)
	}
	if a.DQN != nil {
		_, out := a.DQN.Forward(world.AgentDqnFeatures(w, a))
		rec.DqnQValues = out
	}
	return rec
}
