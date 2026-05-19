package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"maze-of-wumpus/src/world"
)

func newTestModel(seed int64) Model {
	return NewModel(seed, world.NewWorld)
}

func TestNewModelAndInit(t *testing.T) {
	m := newTestModel(1)
	if m.World == nil {
		t.Fatal("world nil")
	}
	if m.Init() == nil {
		t.Error("Init should return a tick cmd")
	}
}

func TestNewModel_NilBuilderUsesDefault(t *testing.T) {
	m := NewModel(1, nil)
	if m.World == nil {
		t.Fatal("nil builder fallback failed")
	}
}

func TestUpdate_QuitKeys(t *testing.T) {
	keys := []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune("q")},
		{Type: tea.KeyCtrlC},
	}
	for _, k := range keys {
		m := newTestModel(1)
		_, cmd := m.Update(k)
		if cmd == nil {
			t.Errorf("key %q produced no cmd", k.String())
			continue
		}
		msg := cmd()
		if _, ok := msg.(tea.QuitMsg); !ok {
			t.Errorf("key %q expected QuitMsg, got %T", k.String(), msg)
		}
	}
}

func TestUpdate_RestartKey_PreservesLearning(t *testing.T) {
	m := newTestModel(1)
	a := m.World.AgentByLabel('1')
	d := m.World.AgentByLabel('4')
	e := m.World.AgentByLabel('5')
	a.Beliefs.SafeFromPit[world.Pos{X: 1, Y: 2}] = true
	d.QL.SetQ(world.Pos{X: 3, Y: 4}, 0, 7.7)
	e.DQN.W1[0] = 1234.5
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	newWorld := m2.(Model).World
	if newWorld == m.World {
		t.Fatal("world was not replaced on 'r'")
	}
	if !newWorld.AgentByLabel('1').Beliefs.SafeFromPit[world.Pos{X: 1, Y: 2}] {
		t.Error("A's Beliefs did not carry over")
	}
	if newWorld.AgentByLabel('4').QL.GetQ(world.Pos{X: 3, Y: 4}, 0) != 7.7 {
		t.Error("D's Q-table did not carry over")
	}
	if newWorld.AgentByLabel('5').DQN.W1[0] != 1234.5 {
		t.Error("E's DQN weights did not carry over")
	}
}

func TestUpdate_ShowPathKey(t *testing.T) {
	m := newTestModel(1)
	if m.ShowPath {
		t.Fatal("ShowPath should default to false")
	}
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	if !m2.(Model).ShowPath {
		t.Error("'s' did not flip ShowPath on")
	}
	m3, _ := m2.(Model).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	if m3.(Model).ShowPath {
		t.Error("second 's' did not flip ShowPath off")
	}
}

// TestUpdate_WumpusToggle: 'w' flips WumpusDisabled. Default is
// disabled-by-default, so the first 'w' should ENABLE wumpus.
func TestUpdate_WumpusToggle(t *testing.T) {
	m := newTestModel(1)
	if !m.World.WumpusDisabled {
		t.Fatal("WumpusDisabled should default to true")
	}
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("w")})
	if m2.(Model).World.WumpusDisabled {
		t.Error("'w' did not enable wumpus (WumpusDisabled should now be false)")
	}
	m3, _ := m2.(Model).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("w")})
	if !m3.(Model).World.WumpusDisabled {
		t.Error("second 'w' did not flip WumpusDisabled back on")
	}
}

// TestUpdate_FireToggle: 'f' flips both FirePitsDisabled and
// WaterPitsDisabled together. Defaults are disabled, so the first
// 'f' should ENABLE both.
func TestUpdate_FireToggle(t *testing.T) {
	m := newTestModel(1)
	if !m.World.FirePitsDisabled || !m.World.WaterPitsDisabled {
		t.Fatal("pits should default to disabled")
	}
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")})
	if m2.(Model).World.FirePitsDisabled {
		t.Error("'f' did not enable fire pits")
	}
	if m2.(Model).World.WaterPitsDisabled {
		t.Error("'f' did not enable water pits")
	}
	m3, _ := m2.(Model).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")})
	if !m3.(Model).World.FirePitsDisabled || !m3.(Model).World.WaterPitsDisabled {
		t.Error("second 'f' did not flip both back to disabled")
	}
}

// TestView_TogglesReflectedInStatus: the status footer should show
// wumpus, pits, and ttl states.
func TestView_TogglesReflectedInStatus(t *testing.T) {
	m := newTestModel(1)
	// Defaults: everything disabled.
	v := m.View()
	for _, want := range []string{"wumpus:OFF", "pits:OFF", "ttl:OFF"} {
		if !strings.Contains(v, want) {
			t.Errorf("status missing %q at startup defaults", want)
		}
	}
	m.World.EnableHazards()
	v = m.View()
	for _, want := range []string{"wumpus:on", "pits:on", "ttl:on"} {
		if !strings.Contains(v, want) {
			t.Errorf("status missing %q after EnableHazards", want)
		}
	}
}

// TestUpdate_AgentToggles: '1'..'5' flips each agent's Disabled flag.
// Agent 1 is enabled by default; agents 2..5 are disabled by default.
func TestUpdate_AgentToggles(t *testing.T) {
	m := newTestModel(1)
	defaults := map[string]bool{
		"1": false, // agent 1 starts enabled
		"2": true, "3": true, "4": true, "5": true,
	}
	for _, key := range []string{"1", "2", "3", "4", "5"} {
		a := m.World.AgentByLabel(rune(key[0]))
		if a.Disabled != defaults[key] {
			t.Fatalf("agent %s default Disabled = %v, want %v",
				key, a.Disabled, defaults[key])
		}
		startDisabled := a.Disabled
		_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
		if a.Disabled == startDisabled {
			t.Errorf("first %s did not flip agent %s", key, key)
		}
		_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
		if a.Disabled != startDisabled {
			t.Errorf("second %s did not return agent %s to default", key, key)
		}
	}
}

// TestUpdate_TTLToggle: 't' flips TTLDisabled.
func TestUpdate_TTLToggle(t *testing.T) {
	m := newTestModel(1)
	if !m.World.TTLDisabled {
		t.Fatal("TTLDisabled should default to true")
	}
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	if m2.(Model).World.TTLDisabled {
		t.Error("'t' did not enable TTL")
	}
	m3, _ := m2.(Model).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	if !m3.(Model).World.TTLDisabled {
		t.Error("second 't' did not flip TTLDisabled back on")
	}
}

// TestGlyphAt_HidesDisabledHazards: with wumpus / fire disabled the
// glyph layer renders path cells where they used to live.
func TestGlyphAt_HidesDisabledHazards(t *testing.T) {
	m := newTestModel(1)
	w := m.World
	w.EnableHazards() // start with hazards so we have entities to disable
	// Plant a wumpus and fire pit at known cells.
	w.Maze.Cells[3][3] = world.CellFirePit
	wm := w.Wumpus[0]
	w.WumpusAt[wm.Pos.Y][wm.Pos.X] = nil
	wm.Pos = world.Pos{X: 4, Y: 4}
	w.WumpusAt[4][4] = wm
	w.WumpusDisabled = true
	w.FirePitsDisabled = true
	if got := m.glyphAt(w, 4, 4); got == wumpusGlyph {
		t.Error("wumpus glyph rendered while disabled")
	}
	if got := m.glyphAt(w, 3, 3); got == firePitGlyph {
		t.Error("fire pit glyph rendered while disabled")
	}
}

func TestUpdate_RestartKey(t *testing.T) {
	m := newTestModel(1)
	orig := m.World
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	if m2.(Model).World == orig {
		t.Error("'r' did not replace the world")
	}
}

func TestUpdate_TickAdvancesWorld(t *testing.T) {
	m := newTestModel(1)
	startCycle := m.World.Cycle
	_, cmd := m.Update(tickMsg{})
	if m.World.Cycle != startCycle+1 {
		t.Errorf("Cycle = %d, want %d", m.World.Cycle, startCycle+1)
	}
	if cmd == nil {
		t.Error("tick should re-arm a new tick cmd")
	}
}

func TestUpdate_UnknownKey(t *testing.T) {
	m := newTestModel(1)
	startCycle := m.World.Cycle
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("z")})
	if m.World.Cycle != startCycle {
		t.Errorf("unknown key should not advance cycle")
	}
	if cmd != nil {
		t.Errorf("unknown key should return nil cmd, got %v", cmd)
	}
}

func TestView_NonEmpty(t *testing.T) {
	m := newTestModel(1)
	s := m.View()
	if !strings.Contains(s, "Maze of Wumpus") {
		t.Error("title missing from View output")
	}
	if !strings.Contains(s, "1 alive") && !strings.Contains(s, "1 dead") {
		t.Error("expected agent 1 status row in view")
	}
}

func TestFormatAgentStats_HitsColorBranches(t *testing.T) {
	m := newTestModel(1)
	m.World.Stats.OptimalDistance = 100
	a := m.World.AgentByLabel('2')
	a.Alive = true
	for _, dist := range []int{50, 150, 180} {
		a.Stats.ActualDistance = dist
		out := m.formatAgentStats(a)
		if !strings.Contains(out, "dist:") {
			t.Errorf("missing dist field in row for dist=%d: %q", dist, out)
		}
	}
	a.Alive = false
	a.Stats.LastDeathReason = "ttl"
	a.Stats.ActualDistance = 150
	if !strings.Contains(m.formatAgentStats(a), "dead") {
		t.Error("dead row should include 'dead'")
	}
}

// TestLastSolveTier: thresholds for the last-solve-time color tier.
func TestLastSolveTier(t *testing.T) {
	tests := []struct {
		last, min, avg, max, want int
		name                      string
	}{
		{0, 0, 0, 0, -1, "never solved"},
		{80, 80, 100, 200, 0, "ties min → green"},
		{50, 80, 100, 200, 0, "below min → green"},
		{90, 80, 100, 200, 1, "between min and avg → yellow"},
		{150, 80, 100, 200, 2, "between avg and max → orange"},
		{200, 80, 100, 200, 2, "ties max → orange"},
		{250, 80, 100, 200, 3, "above max → red"},
	}
	for _, tc := range tests {
		if got := lastSolveTier(tc.last, tc.min, tc.avg, tc.max); got != tc.want {
			t.Errorf("%s: lastSolveTier(%d,%d,%d,%d) = %d, want %d",
				tc.name, tc.last, tc.min, tc.avg, tc.max, got, tc.want)
		}
	}
}

func TestDistSeverity(t *testing.T) {
	tests := []struct {
		actual, ttl, want int
		name              string
	}{
		{0, 0, 0, "zero ttl is safe"},
		{0, 200, 0, "zero dist"},
		{100, 200, 0, "50% safe"},
		{149, 200, 0, "74% safe"},
		{150, 200, 1, "75% warn"},
		{159, 200, 1, "79% warn"},
		{160, 200, 2, "80% danger"},
		{200, 200, 2, "100% danger"},
		{500, 200, 2, "over-TTL danger"},
	}
	for _, tc := range tests {
		if got := distSeverity(tc.actual, tc.ttl); got != tc.want {
			t.Errorf("%s: distSeverity(%d, %d) = %d, want %d",
				tc.name, tc.actual, tc.ttl, got, tc.want)
		}
	}
}

func TestView_ShowPathOverlay(t *testing.T) {
	m := newTestModel(1)
	m.ShowPath = true
	if !strings.Contains(m.View(), "\x1b[43m") {
		t.Error("expected yellow-bg ANSI in View with ShowPath on")
	}
}

// TestView_SeedInTitle: the top line should always include the
// current map's seed.
func TestView_SeedInTitle(t *testing.T) {
	m := newTestModel(98765)
	if !strings.Contains(m.View(), "Seed: 98765") {
		t.Error("title missing 'Seed: 98765'")
	}
}

func TestView_GoalsBanner(t *testing.T) {
	m := newTestModel(1)
	m.World.AgentByLabel('2').Stats.GoalsReached = 3
	if !strings.Contains(m.View(), "GOALS: 3") {
		t.Error("expected GOALS: 3 in title bar")
	}
}

func TestGlyphAt_AllCellTypes(t *testing.T) {
	m := newTestModel(1)
	w := m.World
	w.EnableHazards() // glyphs for wumpus / fire pits etc. only render when enabled
	w.Maze.Cells[5][5] = world.CellWall
	if m.glyphAt(w, 5, 5) != wallGlyph {
		t.Error("wall glyph mismatch")
	}
	w.Maze.Cells[5][5] = world.CellPath
	if m.glyphAt(w, 5, 5) != pathGlyph {
		t.Error("path glyph mismatch")
	}
	w.Maze.Cells[5][5] = world.CellFirePit
	if m.glyphAt(w, 5, 5) != firePitGlyph {
		t.Error("firepit glyph mismatch")
	}
	w.Maze.Cells[5][5] = world.CellGoal
	if m.glyphAt(w, 5, 5) != goalGlyph {
		t.Error("goal glyph mismatch")
	}
	w.Maze.Cells[5][5] = world.CellEntrance
	if m.glyphAt(w, 5, 5) != entranceGlyph {
		t.Error("entrance glyph mismatch")
	}
	w.Maze.Cells[5][5] = world.CellPath
	w.Heat[5][5] = true
	if m.glyphAt(w, 5, 5) != heatGlyph {
		t.Error("heat glyph mismatch")
	}
	w.Heat[5][5] = false
	w.Stench[5][5] = true
	if m.glyphAt(w, 5, 5) != stenchGlyph {
		t.Error("stench glyph mismatch")
	}
	w.Heat[5][5] = true
	w.Stench[5][5] = true
	if m.glyphAt(w, 5, 5) != stenchHeatGl {
		t.Error("heat+stench glyph mismatch")
	}
	w.Heat[5][5] = false
	w.Stench[5][5] = false
	w.Maze.Cells[5][5] = world.CellWaterPit
	if m.glyphAt(w, 5, 5) != waterPitGlyph {
		t.Error("water pit glyph mismatch")
	}
	w.Maze.Cells[5][5] = world.CellPath
	w.ScentOwner[5][5] = '1'
	if m.glyphAt(w, 5, 5) != scent1Glyph {
		t.Error("scent A glyph mismatch")
	}
	w.ScentOwner[5][5] = '2'
	if m.glyphAt(w, 5, 5) != scent2Glyph {
		t.Error("scent B glyph mismatch")
	}
	w.ScentOwner[5][5] = '3'
	if m.glyphAt(w, 5, 5) != scent3Glyph {
		t.Error("scent C glyph mismatch")
	}
	w.ScentOwner[5][5] = '4'
	if m.glyphAt(w, 5, 5) != scent4Glyph {
		t.Error("scent D glyph mismatch")
	}
	w.ScentOwner[5][5] = '5'
	if m.glyphAt(w, 5, 5) != scent5Glyph {
		t.Error("scent E glyph mismatch")
	}
	w.ScentOwner[5][5] = 'Z'
	if m.glyphAt(w, 5, 5) != pathGlyph {
		t.Error("unknown scent owner should render as path")
	}
	w.ScentOwner[5][5] = 0
	a := w.AgentByLabel('1')
	a.Alive = true
	a.Disabled = false
	a.Pos = world.Pos{X: 5, Y: 5}
	w.AgentAt[5][5] = a
	if m.glyphAt(w, 5, 5) != agent1Glyph {
		t.Error("agent A glyph mismatch")
	}
	a.Label = '2'
	if m.glyphAt(w, 5, 5) != agent2Glyph {
		t.Error("agent B glyph mismatch")
	}
	a.Label = '3'
	if m.glyphAt(w, 5, 5) != agent3Glyph {
		t.Error("agent C glyph mismatch")
	}
	a.Label = '4'
	if m.glyphAt(w, 5, 5) != agent4Glyph {
		t.Error("agent D glyph mismatch")
	}
	a.Label = '5'
	if m.glyphAt(w, 5, 5) != agent5Glyph {
		t.Error("agent E glyph mismatch")
	}
}

// TestGlyphAt_GhostsOverlay: an active SearchAnim renders red ghosts
// at every Origin + k*dir for k in [1..Depth].
func TestGlyphAt_GhostsOverlay(t *testing.T) {
	m := newTestModel(1)
	w := m.World
	a := w.AgentByLabel('2')
	a.Alive = true
	a.Disabled = false
	a.Pos = world.Pos{X: 40, Y: 40}
	w.AgentAt[40][40] = a
	a.SearchAnim = &world.SearchAnim{
		Origin:     a.Pos,
		BranchDirs: []world.Pos{{X: 1, Y: 0}, {X: 0, Y: 1}}, // east + south
		ChosenStep: world.Pos{X: 41, Y: 40},
		Phase:      1,
		Depth:      2,
		MaxDepth:   3,
	}
	// East branch at depths 1 and 2:
	if got := m.glyphAt(w, 41, 40); got != ghostGlyph {
		t.Errorf("(41,40) east depth 1 = %q, want ghost", got)
	}
	if got := m.glyphAt(w, 42, 40); got != ghostGlyph {
		t.Errorf("(42,40) east depth 2 = %q, want ghost", got)
	}
	// South branch at depth 1:
	if got := m.glyphAt(w, 40, 41); got != ghostGlyph {
		t.Errorf("(40,41) south depth 1 = %q, want ghost", got)
	}
	// Agent's own cell still renders as agent (ghosts overlay only
	// cells that don't already have an agent / wumpus).
	if got := m.glyphAt(w, 40, 40); got != agent2Glyph {
		t.Errorf("(40,40) origin = %q, want agent2", got)
	}
	// A cell outside the branch fan is NOT a ghost.
	if got := m.glyphAt(w, 39, 40); got == ghostGlyph {
		t.Errorf("(39,40) should not be a ghost")
	}
}

// TestGlyphAt_GhostsHiddenForDisabledAgent: ghost overlay respects
// the per-agent Disabled flag.
func TestGlyphAt_GhostsHiddenForDisabledAgent(t *testing.T) {
	m := newTestModel(1)
	w := m.World
	a := w.AgentByLabel('2')
	a.Disabled = true
	a.SearchAnim = &world.SearchAnim{
		Origin:     world.Pos{X: 5, Y: 5},
		BranchDirs: []world.Pos{{X: 1, Y: 0}},
		Phase:      1,
		Depth:      1,
		MaxDepth:   3,
	}
	if got := m.glyphAt(w, 6, 5); got == ghostGlyph {
		t.Error("ghost rendered for disabled agent")
	}
}

func TestTickEvery(t *testing.T) {
	cmd := tickEvery(1 * time.Millisecond)
	if cmd == nil {
		t.Fatal("tickEvery returned nil")
	}
	if _, ok := cmd().(tickMsg); !ok {
		t.Error("tickEvery cmd did not produce tickMsg")
	}
}
