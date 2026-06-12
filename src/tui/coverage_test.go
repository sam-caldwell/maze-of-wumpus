package tui

import (
	"fmt"
	"strings"
	"testing"

	"maze-of-wumpus/src/world"
)

// TestFormatAgentStats_AllScoreTiers covers each lastScoreTier branch
// (0..3 → green/yellow/orange/red) through formatAgentStats.
func TestFormatAgentStats_AllScoreTiers(t *testing.T) {
	m := NewModel(101, world.NewWorld)
	a := m.World.AgentByLabel('1')
	a.RespawnIn = 0
	m.World.RespawnAgents()
	// MinScore = 0.2, AvgScore = 0.5, MaxScore = 0.9. Flex LastScore
	// through the tier boundaries (≥max, ≥avg, ≥min, <min).
	a.Stats.GoalsReached = 3
	a.Stats.MinScore = 0.2
	a.Stats.AvgScore = 0.5
	a.Stats.MaxScore = 0.9
	for _, ls := range []float64{0.95, 0.6, 0.3, 0.1} {
		a.Stats.LastScore = ls
		_ = m.formatAgentStats(a)
	}
}

// TestFormatAgentStats_DistDangerAndWarn: ActualDistance high enough
// to fire dist:danger (ratio ≥ 0.80) and dist:warn (ratio ≥ 0.75).
func TestFormatAgentStats_DistDangerAndWarn(t *testing.T) {
	m := NewModel(102, world.NewWorld)
	a := m.World.AgentByLabel('1')
	a.RespawnIn = 0
	m.World.RespawnAgents()
	if m.World.Stats.OptimalDistance == 0 {
		t.Skip("zero optimal distance")
	}
	cap := world.TTLMultiplier * m.World.Stats.OptimalDistance
	// Danger band.
	a.Stats.ActualDistance = int(float64(cap) * 0.85)
	out := m.formatAgentStats(a)
	if !strings.Contains(out, "dist:") {
		t.Errorf("missing dist field")
	}
	// Warn band.
	a.Stats.ActualDistance = int(float64(cap) * 0.77)
	_ = m.formatAgentStats(a)
}

// TestFormatAgentStats_StrategyAndFails: the str: column shows the
// current strategy letter and the f: column shows the death count.
func TestFormatAgentStats_StrategyAndFails(t *testing.T) {
	m := NewModel(103, world.NewWorld)
	a := m.World.AgentByLabel('1')
	a.RespawnIn = 0
	m.World.RespawnAgents()
	a.CurrentStrategy = 'X'
	a.Stats.Deaths = 3
	out := m.formatAgentStats(a)
	if !strings.Contains(out, " str:X ") {
		t.Errorf("missing strategy str:X: %s", out)
	}
	if !strings.Contains(out, " f:003 ") {
		t.Errorf("missing fails f:003: %s", out)
	}
}

// TestFormatAgentStats_Dead: dead agents render "dead    " instead
// of "alive   ".
func TestFormatAgentStats_Dead(t *testing.T) {
	m := NewModel(104, world.NewWorld)
	a := m.World.AgentByLabel('1')
	a.Alive = false
	out := m.formatAgentStats(a)
	if !strings.Contains(out, "dead") {
		t.Errorf("missing 'dead' for dead agent: %s", out)
	}
}

// TestReseedPreservingLearning_StopsAtShorterNextAgents covers the
// "i ≥ len(m.World.Agents)" early-break branch.
func TestReseedPreservingLearning_StopsAtShorterNextAgents(t *testing.T) {
	m := NewModel(106, world.NewWorld)
	m.World.Agents = append(m.World.Agents, m.World.Agents[0])
	m.reseedPreservingLearning() // must not panic
}

// TestTrustColorIdx_AllBranches: ≤0 → 0, ≥cap → 15, middle scales.
func TestTrustColorIdx_AllBranches(t *testing.T) {
	if got := trustColorIdx(-1); got != 0 {
		t.Errorf("negative score = %d, want 0", got)
	}
	if got := trustColorIdx(0); got != 0 {
		t.Errorf("zero score = %d, want 0", got)
	}
	if got := trustColorIdx(TrustHeatCap); got != 15 {
		t.Errorf("at-cap score = %d, want 15", got)
	}
	if got := trustColorIdx(TrustHeatCap + 100); got != 15 {
		t.Errorf("above-cap score = %d, want 15", got)
	}
	if got := trustColorIdx(TrustHeatCap / 2); got < 5 || got > 10 {
		t.Errorf("half-cap score = %d, want ~7", got)
	}
}

// TestGlyphAt_AgentEntrancePerColor: when an agent's perimeter
// entrance is rendered, the glyph carries that agent's per-label
// background color and the agent's own identifier rune (e.g., "1")
// as the foreground character.
func TestGlyphAt_AgentEntrancePerColor(t *testing.T) {
	m := NewModel(108, world.NewWorld)
	a := m.World.AgentByLabel('1')
	if a == nil {
		t.Skip("agent 1 missing")
	}
	got := m.glyphAt(m.World, a.EntrancePos.X, a.EntrancePos.Y)
	if got == entranceGlyph {
		t.Errorf("agent 1's entrance rendered with generic glyph instead of per-agent")
	}
	// Must contain "48;5;39" (the bg color clause).
	if !strings.Contains(got, "48;5;39") {
		t.Errorf("agent 1's entrance glyph missing bg color 39: %q", got)
	}
	// Must contain the agent's own label rune ("1"), NOT "S".
	if !strings.Contains(got, "1") {
		t.Errorf("agent 1's entrance glyph missing label '1': %q", got)
	}
	if strings.Contains(got, "S") {
		t.Errorf("agent 1's entrance glyph still contains stale 'S': %q", got)
	}
}

// TestFormatAgentStats_TTLColumnIsPerAgent: two agents with very
// different OptimalDistance values produce distinct TTL columns.
func TestFormatAgentStats_TTLColumnIsPerAgent(t *testing.T) {
	m := NewModel(109, world.NewWorld)
	a := m.World.AgentByLabel('1')
	b := m.World.AgentByLabel('2')
	a.OptimalDistance = 100
	b.OptimalDistance = 250
	wantA := fmt.Sprintf("TTL:%04d", world.TTLMultiplier*100)
	wantB := fmt.Sprintf("TTL:%04d", world.TTLMultiplier*250)
	if !strings.Contains(m.formatAgentStats(a), wantA) {
		t.Errorf("agent 1 stats missing %q", wantA)
	}
	if !strings.Contains(m.formatAgentStats(b), wantB) {
		t.Errorf("agent 2 stats missing %q", wantB)
	}
}

// TestDistSeverity_PerAgentTTL: distSeverity classifies ActualDistance
// against the supplied TTL. The formatAgentStats hookup now passes
// `TTLMultiplier × a.OptimalDistance` (per-agent), so the severity
// function fires at per-agent ratios — independent of the world-wide
// Stats.OptimalDistance.
func TestDistSeverity_PerAgentTTL(t *testing.T) {
	// Pick an OptimalDistance × TTLMultiplier large enough that the
	// per-tier ratios round cleanly. Values are derived from the cap
	// so the assertions stay correct if TTLMultiplier is retuned.
	perAgentCap := world.TTLMultiplier * 100
	if got := distSeverity(perAgentCap*90/100, perAgentCap); got != 2 {
		t.Errorf("90%% of per-agent cap should be danger (severity 2), got %d", got)
	}
	if got := distSeverity(perAgentCap*76/100, perAgentCap); got != 1 {
		t.Errorf("76%% of per-agent cap should be warn (severity 1), got %d", got)
	}
	if got := distSeverity(perAgentCap*40/100, perAgentCap); got != 0 {
		t.Errorf("40%% of per-agent cap should be normal (severity 0), got %d", got)
	}
}

// TestGlyphAt_DisabledAgentInvisible: disabled agents on a cell
// do not render their glyph.
func TestGlyphAt_DisabledAgentInvisible(t *testing.T) {
	m := NewModel(107, world.NewWorld)
	a := m.World.AgentByLabel('1')
	a.Pos = world.Pos{X: 50, Y: 50}
	a.Alive = true
	a.Disabled = true
	m.World.AgentAt[50][50] = a
	got := m.glyphAt(m.World, 50, 50)
	if got == agent1Glyph {
		t.Errorf("disabled agent glyph rendered: %q", got)
	}
}

