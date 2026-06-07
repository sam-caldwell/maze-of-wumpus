// stats_log.go — write a one-shot JSON snapshot of the world's
// statistics whenever the maze is considered solved (see MazeSolved).
// The auto-reseed callers (TUI, headless loop) invoke WriteStatsLog
// just before constructing the next maze so a researcher can diff
// the per-maze performance of each agent over a long run.
//
// File naming: build/stats/<unix-nanoseconds>.log, one file per
// solved maze. Contents are pretty-printed JSON for grep / jq use.
package world

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// MazeStatsLog is the JSON record written per solved maze.
type MazeStatsLog struct {
	WrittenAt       time.Time        `json:"written_at"`
	Seed            int64            `json:"seed"`
	Cycle           int              `json:"cycle"`
	OptimalDistance int              `json:"optimal_distance"`
	ShortestPaths   int              `json:"shortest_paths"`
	Agents          []AgentStatsLogR `json:"agents"`
}

// AgentStatsLogR is one row of agent stats in the maze log.
type AgentStatsLogR struct {
	Label    string     `json:"label"`
	Disabled bool       `json:"disabled"`
	Stats    AgentStats `json:"stats"`
}

// WriteStatsLog marshals a snapshot of the world's per-agent and
// maze-level stats to `<dir>/<unix-ns>.log`. Creates `dir` on
// demand. Returns the file path written. Errors propagate so the
// caller can choose to log/print them, but in normal flow they
// shouldn't happen.
func (w *World) WriteStatsLog(dir string) (string, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("mkdir %s: %w", dir, err)
	}
	now := time.Now()
	rec := MazeStatsLog{
		WrittenAt:       now,
		Seed:            w.Seed,
		Cycle:           w.Cycle,
		OptimalDistance: w.Stats.OptimalDistance,
		ShortestPaths:   w.Stats.ShortestPaths,
	}
	for _, a := range w.Agents {
		rec.Agents = append(rec.Agents, AgentStatsLogR{
			Label:    string(a.Label),
			Disabled: a.Disabled,
			Stats:    a.Stats,
		})
	}
	buf, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal: %w", err)
	}
	path := filepath.Join(dir, fmt.Sprintf("%d.log", now.UnixNano()))
	if err := os.WriteFile(path, buf, 0644); err != nil {
		return "", fmt.Errorf("write %s: %w", path, err)
	}
	return path, nil
}
