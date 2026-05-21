package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"maze-of-wumpus/src/world"
)

// TestFormatAgentStats_AllSolveTiers covers each lastSolveTier branch
// (0..3 → green/yellow/orange/red).
func TestFormatAgentStats_AllSolveTiers(t *testing.T) {
	m := NewModel(101, world.NewWorld)
	a := m.World.AgentByLabel('1')
	a.RespawnIn = 0
	m.World.RespawnAgents()
	// MinSolveTime = 10, MaxSolveTime = 100, AvgSolveTime = 55. We
	// flex LastSolveTime through the tier boundaries.
	a.Stats.MinSolveTime = 10
	a.Stats.MaxSolveTime = 100
	a.Stats.AvgSolveTime = 55.0
	for _, ls := range []int{10, 50, 70, 100} {
		a.Stats.LastSolveTime = ls
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

// TestFormatAgentStats_TrusteeAndStrategy: rendered with non-zero
// trustee and CurrentStrategy.
func TestFormatAgentStats_TrusteeAndStrategy(t *testing.T) {
	m := NewModel(103, world.NewWorld)
	a := m.World.AgentByLabel('1')
	a.RespawnIn = 0
	m.World.RespawnAgents()
	a.CurrentTrustee = '5'
	a.CurrentStrategy = 'X'
	a.LearnedTTL = 42
	out := m.formatAgentStats(a)
	if !strings.Contains(out, " f:5 ") {
		t.Errorf("missing trustee f:5: %s", out)
	}
	if !strings.Contains(out, " str:X ") {
		t.Errorf("missing strategy str:X: %s", out)
	}
	if !strings.Contains(out, "ttl:0042") {
		t.Errorf("missing learned ttl: %s", out)
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

// TestUpdate_FarSightAgentToggleKey covers the a/A/b/B/c/C branch.
func TestUpdate_FarSightAgentToggleKey(t *testing.T) {
	m := NewModel(105, world.NewWorld)
	a := m.World.AgentByLabel('A')
	before := a.Disabled
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	m = next.(Model)
	if a.Disabled == before {
		t.Errorf("'a' key did not toggle agent A")
	}
	// Upper-case form should work too.
	beforeB := m.World.AgentByLabel('B').Disabled
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("B")})
	m = next.(Model)
	if m.World.AgentByLabel('B').Disabled == beforeB {
		t.Errorf("'B' key did not toggle agent B")
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

