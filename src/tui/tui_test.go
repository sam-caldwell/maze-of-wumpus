package tui

import (
	"fmt"
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
	bayes := m.World.AgentByLabel('2') // Bayesian
	qmdp := m.World.AgentByLabel('4')  // QMDP
	bayes.Beliefs.Observed[world.Pos{X: 1, Y: 2}] = true
	qmdp.LearnedTTL = 555
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	newWorld := m2.(Model).World
	if newWorld == m.World {
		t.Fatal("world was not replaced on 'r'")
	}
	if !newWorld.AgentByLabel('2').Beliefs.Observed[world.Pos{X: 1, Y: 2}] {
		t.Error("Bayesian beliefs did not carry over")
	}
	if newWorld.AgentByLabel('4').LearnedTTL != 555 {
		t.Errorf("LearnedTTL did not carry over: %d, want 555",
			newWorld.AgentByLabel('4').LearnedTTL)
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

// TestView_PerAgentTTLColumn: TTL is now surfaced per-agent in the
// stats line (right of dist) rather than in the global status bar.
// Verify the per-agent TTL columns render and the status bar no
// longer carries a global TTL/multiplier value.
func TestView_PerAgentTTLColumn(t *testing.T) {
	m := newTestModel(1)
	v := m.View()
	// Every agent line should contain a "TTL:NNNN" column.
	for _, a := range m.World.Agents {
		want := fmt.Sprintf("TTL:%04d", world.TTLMultiplier*a.OptimalDistance)
		if !strings.Contains(v, want) {
			t.Errorf("missing per-agent TTL column for %c (%q)", a.Label, want)
		}
	}
	// The status bar should no longer contain a "×<multiplier>" token.
	if strings.Contains(v, fmt.Sprintf("TTL %d ×%d", world.TTLMultiplier, world.TTLMultiplier)) {
		t.Errorf("status bar should no longer contain global TTL/multiplier display")
	}
}

// TestUpdate_AgentToggles: '1'..'4' flips each agent's Disabled flag.
// All four agents start enabled by default.
func TestUpdate_AgentToggles(t *testing.T) {
	m := newTestModel(1)
	for _, key := range []string{"1", "2", "3", "4"} {
		a := m.World.AgentByLabel(rune(key[0]))
		if a.Disabled {
			t.Fatalf("agent %s should default to enabled (Disabled=false)", key)
		}
		_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
		if !a.Disabled {
			t.Errorf("first %s did not disable agent %s", key, key)
		}
		_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
		if a.Disabled {
			t.Errorf("second %s did not re-enable agent %s", key, key)
		}
	}
}

// TestUpdate_TTLToggle: 't' flips TTLDisabled. TTL defaults to ON
// (TTLDisabled=false) so the first press disables it.
func TestUpdate_TTLToggle(t *testing.T) {
	m := newTestModel(1)
	if m.World.TTLDisabled {
		t.Fatal("TTLDisabled should default to false (TTL on)")
	}
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	if !m2.(Model).World.TTLDisabled {
		t.Error("'t' did not disable TTL")
	}
	m3, _ := m2.(Model).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	if m3.(Model).World.TTLDisabled {
		t.Error("second 't' did not re-enable TTL")
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

// TestFormatAgentStats_HasStartsColumn confirms the new s:NNN
// column shows up and the old last_death column is gone.
func TestFormatAgentStats_HasStartsColumn(t *testing.T) {
	m := newTestModel(1)
	a := m.World.AgentByLabel('1')
	a.Alive = true
	a.Stats.Starts = 7
	a.Stats.LastDeathReason = "wumpus" // still tracked but not rendered
	out := m.formatAgentStats(a)
	if !strings.Contains(out, "s:007") {
		t.Errorf("status row missing 's:007': %q", out)
	}
	if strings.Contains(out, "last_death") {
		t.Errorf("status row still has last_death column: %q", out)
	}
}

// TestFormatAgentStats_FailsColumn: the f: column is the agent's fail
// (death) count — 0 on a fresh agent, and reflecting Stats.Deaths
// otherwise. (It is NOT the trustee: scent following was removed.)
func TestFormatAgentStats_FailsColumn(t *testing.T) {
	m := newTestModel(1)
	a := m.World.AgentByLabel('4')
	a.Alive = true
	a.Stats.Deaths = 0
	if out := m.formatAgentStats(a); !strings.Contains(out, "f:000") {
		t.Errorf("fresh agent should show f:000, got %q", out)
	}
	a.Stats.Deaths = 7
	if out := m.formatAgentStats(a); !strings.Contains(out, "f:007") {
		t.Errorf("agent with 7 deaths should show f:007, got %q", out)
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
	w.Maze.Cells[5][5] = world.CellWall
	if m.glyphAt(w, 5, 5) != wallGlyph {
		t.Error("wall glyph mismatch")
	}
	w.Maze.Cells[5][5] = world.CellPath
	if m.glyphAt(w, 5, 5) != pathGlyph {
		t.Error("path glyph mismatch")
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
	// Stamp a fresh deposit so the trail is within its lifetime (a stale
	// deposit now renders as plain path).
	w.Cycle = 1
	w.ScentCycle[5][5] = 1
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

// TestUpdate_ArrowKeysScrollMazeViewport: arrow keys bump offsetX /
// offsetY by one cell, clamped to [0, BoardWidth/Height − viewport].
// With no WindowSizeMsg the viewport equals the full board, so the
// max offsets are 0 in both dims — every arrow press is a no-op.
func TestUpdate_ArrowKeysScrollMazeViewport(t *testing.T) {
	m := newTestModel(1)
	// Resize so the viewport is smaller than the board, otherwise
	// clamp would pin offsets to zero.
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 60, Height: 40})
	m = m2.(Model)
	if m.offsetX != 0 || m.offsetY != 0 {
		t.Fatalf("initial offsets should be (0,0), got (%d,%d)", m.offsetX, m.offsetY)
	}
	m3, _ := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = m3.(Model)
	if m.offsetX != 1 {
		t.Errorf("right arrow → offsetX = %d, want 1", m.offsetX)
	}
	m4, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = m4.(Model)
	if m.offsetY != 1 {
		t.Errorf("down arrow → offsetY = %d, want 1", m.offsetY)
	}
	// Left at offsetX=1 → 0. A second left should clamp at 0.
	m5, _ := m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	m = m5.(Model)
	if m.offsetX != 0 {
		t.Errorf("left arrow → offsetX = %d, want 0", m.offsetX)
	}
	m6, _ := m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	m = m6.(Model)
	if m.offsetX != 0 {
		t.Errorf("left arrow at 0 should clamp, got offsetX = %d", m.offsetX)
	}
}

// TestUpdate_ShiftArrowKeysPageScroll: shift+arrow jumps the maze
// viewport by a full page (the viewport dimension), while a plain
// arrow moves one cell. Verifies the page jump equals viewW/viewH.
func TestUpdate_ShiftArrowKeysPageScroll(t *testing.T) {
	m := newTestModel(1)
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 60, Height: 40})
	m = m2.(Model)
	viewW, viewH := m.currentViewSize()
	if viewW <= 1 || viewH <= 1 {
		t.Fatalf("viewport too small to test paging: (%d,%d)", viewW, viewH)
	}
	// shift+down jumps one page vertically.
	m3, _ := m.Update(tea.KeyMsg{Type: tea.KeyShiftDown})
	m = m3.(Model)
	if m.offsetY != viewH {
		t.Errorf("shift+down → offsetY = %d, want %d (one page)", m.offsetY, viewH)
	}
	// shift+right jumps one page horizontally.
	m4, _ := m.Update(tea.KeyMsg{Type: tea.KeyShiftRight})
	m = m4.(Model)
	if m.offsetX != viewW {
		t.Errorf("shift+right → offsetX = %d, want %d (one page)", m.offsetX, viewW)
	}
	// shift+up from one page down returns to the top (clamped at 0).
	m5, _ := m.Update(tea.KeyMsg{Type: tea.KeyShiftUp})
	m = m5.(Model)
	if m.offsetY != 0 {
		t.Errorf("shift+up → offsetY = %d, want 0", m.offsetY)
	}
	// shift+left from one page right returns to the left edge.
	m6, _ := m.Update(tea.KeyMsg{Type: tea.KeyShiftLeft})
	m = m6.(Model)
	if m.offsetX != 0 {
		t.Errorf("shift+left → offsetX = %d, want 0", m.offsetX)
	}
}

// TestUpdate_PageKeysScrollVertically: PgUp / PgDn jump the maze
// viewport a full page vertically. These are the reliable vertical-
// page keys for terminals that intercept shift+↑/↓ and forward a
// bare arrow instead.
func TestUpdate_PageKeysScrollVertically(t *testing.T) {
	m := newTestModel(1)
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 60, Height: 40})
	m = m2.(Model)
	_, viewH := m.currentViewSize()
	if viewH <= 1 {
		t.Fatalf("viewport too short to test paging: %d", viewH)
	}
	m3, _ := m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	m = m3.(Model)
	if m.offsetY != viewH {
		t.Errorf("pgdown → offsetY = %d, want %d (one page)", m.offsetY, viewH)
	}
	m4, _ := m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	m = m4.(Model)
	if m.offsetY != 0 {
		t.Errorf("pgup → offsetY = %d, want 0", m.offsetY)
	}
}

// TestUpdate_ShiftArrowClampsAtBoardEdge: a single page jump never
// pushes the viewport past the board edge.
func TestUpdate_ShiftArrowClampsAtBoardEdge(t *testing.T) {
	m := newTestModel(1)
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 60, Height: 40})
	m = m2.(Model)
	viewW, viewH := m.currentViewSize()
	maxX := world.BoardWidth - viewW
	maxY := world.BoardHeight - viewH
	// Spam shift+down / shift+right well past the edge.
	for i := 0; i < world.BoardHeight; i++ {
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyShiftDown})
		m = next.(Model)
		next, _ = m.Update(tea.KeyMsg{Type: tea.KeyShiftRight})
		m = next.(Model)
	}
	if m.offsetY != maxY {
		t.Errorf("offsetY after paging to edge = %d, want %d", m.offsetY, maxY)
	}
	if m.offsetX != maxX {
		t.Errorf("offsetX after paging to edge = %d, want %d", m.offsetX, maxX)
	}
}

// TestUpdate_ArrowKeysClampToBoardEdge: spamming arrows past the
// board edge should stop at the maximum offset (board dim minus
// viewport dim).
func TestUpdate_ArrowKeysClampToBoardEdge(t *testing.T) {
	m := newTestModel(1)
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 60, Height: 40})
	m = m2.(Model)
	for i := 0; i < world.BoardWidth+10; i++ {
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRight})
		m = next.(Model)
	}
	if m.offsetX <= 0 || m.offsetX > world.BoardWidth {
		t.Errorf("offsetX after spam = %d, expected clamped > 0 and ≤ BoardWidth", m.offsetX)
	}
	for i := 0; i < world.BoardHeight+10; i++ {
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = next.(Model)
	}
	if m.offsetY <= 0 || m.offsetY > world.BoardHeight {
		t.Errorf("offsetY after spam = %d, expected clamped > 0 and ≤ BoardHeight", m.offsetY)
	}
}

// TestUpdate_ReseedResetsOffsets: reseeding via 'r' should snap the
// maze viewport back to the top-left corner.
func TestUpdate_ReseedResetsOffsets(t *testing.T) {
	m := newTestModel(1)
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 60, Height: 40})
	m = m2.(Model)
	m3, _ := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = m3.(Model)
	if m.offsetX == 0 {
		t.Fatal("offsetX should be > 0 after a right-arrow press")
	}
	m4, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	m = m4.(Model)
	if m.offsetX != 0 || m.offsetY != 0 {
		t.Errorf("reseed should reset offsets to (0,0), got (%d,%d)",
			m.offsetX, m.offsetY)
	}
}

// TestUpdate_WindowSizeMsgReclampsOffsets: shrinking the terminal so
// the previously-valid offset would push the viewport off the board
// must re-clamp offsets back into range.
func TestUpdate_WindowSizeMsgReclampsOffsets(t *testing.T) {
	m := newTestModel(1)
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 60, Height: 40})
	m = m2.(Model)
	for i := 0; i < 30; i++ {
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRight})
		m = next.(Model)
	}
	preMax := m.offsetX
	// Re-resize to the full-board fallback: viewport = board, so the
	// only valid offset is 0.
	m3, _ := m.Update(tea.WindowSizeMsg{Width: 0, Height: 0})
	m = m3.(Model)
	if m.offsetX != 0 {
		t.Errorf("offset should re-clamp to 0 when viewport grows to whole board, got %d (was %d)",
			m.offsetX, preMax)
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

// TestGlyphAt_ScentExpiresAfterLifetime: a scent trail renders while fresh
// but is removed (renders as plain path) once older than ScentMaxAge
// cycles; re-walking the cell resets its lifetime.
func TestGlyphAt_ScentExpiresAfterLifetime(t *testing.T) {
	m := newTestModel(1)
	w := m.World
	x, y := 5, 5
	w.Maze.Cells[y][x] = world.CellPath
	w.ScentOwner[y][x] = '1'

	w.Cycle = 10
	w.ScentCycle[y][x] = 10 // fresh deposit this cycle
	if m.glyphAt(w, x, y) != scent1Glyph {
		t.Error("fresh scent should render the scent glyph")
	}

	w.Cycle = 10 + world.ScentMaxAge // aged out
	if m.glyphAt(w, x, y) != pathGlyph {
		t.Error("expired scent should render as plain path (removed)")
	}

	w.ScentCycle[y][x] = w.Cycle // re-walked → lifetime reset
	if m.glyphAt(w, x, y) != scent1Glyph {
		t.Error("refreshed scent should render again")
	}
}
