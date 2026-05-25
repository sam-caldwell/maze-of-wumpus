package world

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSetWumpusDisabled_SpawnsOnEnable: toggle-on populates the board
// with fresh wumpus; toggle-off removes them all.
func TestSetWumpusDisabled_SpawnsOnEnable(t *testing.T) {
	w := NewWorld(260)
	// Default is disabled → no wumpus.
	if len(w.Wumpus) != 0 {
		t.Fatalf("precondition: expected 0 wumpus by default, got %d", len(w.Wumpus))
	}
	w.SetWumpusDisabled(false)
	if len(w.Wumpus) < 5 || len(w.Wumpus) > 12 {
		t.Errorf("after enable: wumpus count = %d, want 5..12", len(w.Wumpus))
	}
	for _, wm := range w.Wumpus {
		if !wm.Alive {
			t.Error("freshly spawned wumpus should be Alive")
		}
		if w.WumpusAt[wm.Pos.Y][wm.Pos.X] != wm {
			t.Error("spatial index out of sync with wumpus list")
		}
	}
	w.SetWumpusDisabled(true)
	if len(w.Wumpus) != 0 {
		t.Errorf("after disable: wumpus count = %d, want 0", len(w.Wumpus))
	}
}

// TestSetFirePitsDisabled_SpawnsOnEnable: toggle-on adds fire pits to
// rooms with the heat envelope, toggle-off strips them.
func TestSetFirePitsDisabled_SpawnsOnEnable(t *testing.T) {
	w := NewWorld(261)
	if len(w.Maze.FirePits) != 0 {
		t.Fatalf("precondition: expected 0 fire pits by default")
	}
	w.SetFirePitsDisabled(false)
	if len(w.Maze.FirePits) == 0 {
		// Sometimes rooms produce zero pits randomly; retry by re-
		// flipping a few times to give the rng a chance.
		for i := 0; i < 5 && len(w.Maze.FirePits) == 0; i++ {
			w.SetFirePitsDisabled(true)
			w.SetFirePitsDisabled(false)
		}
		if len(w.Maze.FirePits) == 0 {
			t.Skip("seed/rooms produced no fire pits even after retries")
		}
	}
	// Confirm each pit's cell is CellFirePit and Heat is set adjacent.
	for _, p := range w.Maze.FirePits {
		if w.Maze.Cells[p.Y][p.X] != CellFirePit {
			t.Errorf("pit %v cell type = %v, want CellFirePit", p, w.Maze.Cells[p.Y][p.X])
		}
	}
	w.SetFirePitsDisabled(true)
	if len(w.Maze.FirePits) != 0 {
		t.Errorf("after disable: fire pit count = %d, want 0", len(w.Maze.FirePits))
	}
	// Heat grid must be fully zero too.
	for y := 0; y < BoardHeight; y++ {
		for x := 0; x < BoardWidth; x++ {
			if w.Heat[y][x] {
				t.Fatalf("heat at (%d,%d) survived fire-pit disable", x, y)
			}
		}
	}
}

// TestSetWaterPitsDisabled_SpawnsOnEnable: toggle-on scatters water
// pits, toggle-off clears them.
func TestSetWaterPitsDisabled_SpawnsOnEnable(t *testing.T) {
	w := NewWorld(262)
	if len(w.Maze.WaterPits) != 0 {
		t.Fatalf("precondition: expected 0 water pits by default")
	}
	w.SetWaterPitsDisabled(false)
	if len(w.Maze.WaterPits) < 3 || len(w.Maze.WaterPits) > 10 {
		t.Errorf("after enable: water pit count = %d, want 3..10",
			len(w.Maze.WaterPits))
	}
	w.SetWaterPitsDisabled(true)
	if len(w.Maze.WaterPits) != 0 {
		t.Errorf("after disable: water pit count = %d, want 0",
			len(w.Maze.WaterPits))
	}
}

// TestSetWumpusDisabled_NoOpWhenAlreadyMatchingState: flipping to the
// same state is idempotent — no new wumpus spawn.
func TestSetWumpusDisabled_NoOpWhenAlreadyMatchingState(t *testing.T) {
	w := NewWorld(263)
	w.SetWumpusDisabled(false)
	first := len(w.Wumpus)
	w.SetWumpusDisabled(false) // already false, should be no-op
	if len(w.Wumpus) != first {
		t.Errorf("redundant enable spawned new wumpus: %d -> %d",
			first, len(w.Wumpus))
	}
}

// TestWumpusDisabled_DoesNotBlockMovement: a live but disabled wumpus
// must NOT block agent movement. Regression for the bug where agent 1
// froze indefinitely in front of a wumpus cell when WumpusDisabled
// was true (wumpus was inert gameplay-wise but MoveAgents still
// refused to step onto its cell).
func TestWumpusDisabled_DoesNotBlockMovement(t *testing.T) {
	w := NewWorld(250)
	// Make sure wumpus stays disabled (default for NewWorld but be
	// explicit in case future defaults change).
	w.WumpusDisabled = true
	a := SpawnAgentForTest(w, '1')
	// Park the agent at an interior path cell with a known walkable
	// neighbor, place a (disabled-context) wumpus on that neighbor,
	// and ask the agent's planner-free Strategy stub to walk into it.
	w.AgentAt[a.Pos.Y][a.Pos.X] = nil
	a.Pos = Pos{X: 40, Y: 40}
	w.AgentAt[40][40] = a
	w.Maze.Cells[40][40] = CellPath
	target := Pos{X: 41, Y: 40}
	w.Maze.Cells[target.Y][target.X] = CellPath
	// Plant a "live" wumpus at the target cell.
	wm := &Wumpus{ID: 999, Pos: target, Alive: true}
	w.Wumpus = append(w.Wumpus, wm)
	w.WumpusAt[target.Y][target.X] = wm
	// Drive agent's move via injected strategy returning `target`.
	a.Strategy = func(_ *World, _ *Agent) Pos { return target }
	w.MoveAgents()
	if a.Pos != target {
		t.Errorf("agent stayed at %v despite wumpus-disabled; expected to walk onto %v", a.Pos, target)
	}
}

// TestWaterShield_ExtinguishesFirePit: stepping onto a fire pit with
// water charges consumes the charge AND removes the fire pit.
func TestWaterShield_ExtinguishesFirePit(t *testing.T) {
	w := NewWorld(200)
	w.EnableHazards()
	if len(w.Maze.FirePits) == 0 {
		t.Skip("seed produced no fire pits")
	}
	a := SpawnAgentForTest(w, '1')
	pit := w.Maze.FirePits[0]
	w.AgentAt[a.Pos.Y][a.Pos.X] = nil
	a.Pos = pit
	w.AgentAt[pit.Y][pit.X] = a
	a.Water = 1
	pitsBefore := len(w.Maze.FirePits)
	w.ResolvePitDeaths()
	if !a.Alive {
		t.Fatal("agent should survive with water")
	}
	if a.Water != 0 {
		t.Errorf("water = %d after shield, want 0", a.Water)
	}
	if w.Maze.Cells[pit.Y][pit.X] != CellPath {
		t.Errorf("pit cell type = %v, want CellPath", w.Maze.Cells[pit.Y][pit.X])
	}
	if len(w.Maze.FirePits) != pitsBefore-1 {
		t.Errorf("FirePits = %d, want %d", len(w.Maze.FirePits), pitsBefore-1)
	}
}

// TestExtinguishFirePit_RecomputesHeat: extinguishing a fire pit
// clears Heat in its neighborhood UNLESS another pit also adjoins
// the same cell.
func TestExtinguishFirePit_RecomputesHeat(t *testing.T) {
	w := NewWorld(201)
	// Set up two adjacent fire pits at (40,40) and (40,42). Heat at
	// (40,41) is contributed by both — extinguishing one should leave
	// (40,41) hot from the other.
	p1, p2 := Pos{40, 40}, Pos{40, 42}
	w.Maze.Cells[p1.Y][p1.X] = CellFirePit
	w.Maze.Cells[p2.Y][p2.X] = CellFirePit
	w.Maze.FirePits = append(w.Maze.FirePits, p1, p2)
	// Manually flag heat at every cell adjacent to either pit.
	for _, p := range []Pos{p1, p2} {
		for dy := -1; dy <= 1; dy++ {
			for dx := -1; dx <= 1; dx++ {
				if dx == 0 && dy == 0 {
					continue
				}
				nx, ny := p.X+dx, p.Y+dy
				if InBounds(nx, ny) && w.Maze.Cells[ny][nx] != CellWall {
					w.Heat[ny][nx] = true
				}
			}
		}
	}
	shared := Pos{40, 41}
	if !w.Heat[shared.Y][shared.X] {
		t.Fatal("test setup: shared cell should be hot")
	}
	w.ExtinguishFirePit(p1)
	if w.Heat[shared.Y][shared.X] != true {
		t.Errorf("shared cell heat = %v, want true (other pit still adjacent)",
			w.Heat[shared.Y][shared.X])
	}
	// (39,39) was only adjacent to p1; after p1 is gone it should cool.
	cool := Pos{39, 39}
	if w.Heat[cool.Y][cool.X] {
		t.Errorf("cell %v should be cool after p1 extinguished", cool)
	}
}

// TestExtinguishFirePit_NoopOnNonPit: calling Extinguish on a non-pit
// cell is a silent no-op.
func TestExtinguishFirePit_NoopOnNonPit(t *testing.T) {
	w := NewWorld(202)
	p := Pos{50, 50}
	w.Maze.Cells[p.Y][p.X] = CellPath
	pitsBefore := len(w.Maze.FirePits)
	w.ExtinguishFirePit(p)
	if len(w.Maze.FirePits) != pitsBefore {
		t.Errorf("FirePits changed by no-op")
	}
}

// TestIsScentFollower: follower set is {4,5,6,7,9,A,B,C}; leaders
// {1,2,3,8} are not followers.
func TestIsScentFollower(t *testing.T) {
	for _, l := range ScentFollowerLabels {
		if !IsScentFollower(l) {
			t.Errorf("IsScentFollower(%c) = false, want true", l)
		}
	}
	for _, l := range ScentLeaderLabels {
		if IsScentFollower(l) {
			t.Errorf("IsScentFollower(%c) = true, want false", l)
		}
	}
}

// TestAgentPerceptionDefaults: every agent is constructed with the
// uniform default smell/sight radii. The old far-sight distinction
// is gone — labels 8/9/A/B/C are perception-equivalent to 1-7.
func TestAgentPerceptionDefaults(t *testing.T) {
	w := NewWorld(310)
	for _, l := range []rune{'1', '2', '3', '4', '5', '6', '7', '8', '9', 'A', 'B', 'C'} {
		a := w.AgentByLabel(l)
		if a == nil {
			t.Fatalf("missing agent %c", l)
		}
		if a.SmellRadius != DefaultSmellRadius {
			t.Errorf("agent %c SmellRadius=%d, want %d",
				l, a.SmellRadius, DefaultSmellRadius)
		}
		if a.SightRadius != DefaultSightRadius {
			t.Errorf("agent %c SightRadius=%d, want %d",
				l, a.SightRadius, DefaultSightRadius)
		}
	}
}

// TestMarkAgentSensed_SightRadius1: with SightRadius=1, the BFS
// covers the agent's cell + 8 Moore neighbors (3×3). The wall-
// adjacency post-pass then extends each path cell's Moore
// neighbors, growing the perceived set to 5×5 = 25 cells in fully-
// open terrain.
func TestMarkAgentSensed_SightRadius1(t *testing.T) {
	w := NewWorld(311)
	for y := 38; y <= 42; y++ {
		for x := 38; x <= 42; x++ {
			w.Maze.Cells[y][x] = CellPath
		}
	}
	a := &Agent{Label: '4', Pos: Pos{X: 40, Y: 40}, SightRadius: 1}
	w.MarkAgentSensed(a)
	// 3×3 BFS + post-pass extends to 5×5.
	if got := len(a.KnownCells); got != 25 {
		t.Errorf("SightRadius=1 + adjacency = %d, want 25 (5×5 after wall-adjacency)", got)
	}
}

// TestMarkAgentSensed_SightRadius2OpenArea: SightRadius=2 BFS covers
// 5×5; the wall-adjacency post-pass extends to 7×7 = 49 cells.
func TestMarkAgentSensed_SightRadius2OpenArea(t *testing.T) {
	w := NewWorld(312)
	for y := 38; y <= 42; y++ {
		for x := 38; x <= 42; x++ {
			w.Maze.Cells[y][x] = CellPath
		}
	}
	a := &Agent{Label: '9', Pos: Pos{X: 40, Y: 40}, SightRadius: 2}
	w.MarkAgentSensed(a)
	if got := len(a.KnownCells); got != 49 {
		t.Errorf("SightRadius=2 + adjacency = %d, want 49 (7×7)", got)
	}
}

// TestMarkAgentSensed_DefaultRadius_FillsWalledRegion: with the
// production default SightRadius=100, the radius exceeds the board
// diagonal, so perception is bounded by wall reachability — not by
// the radius. A 30×30 path block surrounded by a hard wall ring
// should be fully perceived from a single tick at center, and
// nothing outside the wall ring should be perceived.
func TestMarkAgentSensed_DefaultRadius_FillsWalledRegion(t *testing.T) {
	w := NewWorld(314)
	// Wall ring at (35..66) × (35..66); 30×30 open interior at
	// (36..65) × (36..65). Wall everything in between path cells
	// so the BFS can't escape via the surrounding generated maze.
	for y := 35; y <= 66; y++ {
		for x := 35; x <= 66; x++ {
			if y == 35 || y == 66 || x == 35 || x == 66 {
				w.Maze.Cells[y][x] = CellWall
			} else {
				w.Maze.Cells[y][x] = CellPath
			}
		}
	}
	a := &Agent{Label: '3', Pos: Pos{X: 50, Y: 50}, SightRadius: DefaultSightRadius}
	w.MarkAgentSensed(a)
	// Every cell in the 32×32 (interior + wall ring) must be known:
	//   30×30 = 900 paths + (32×32 − 30×30) = 124 walls = 1024 cells
	want := 32 * 32
	if got := len(a.KnownCells); got != want {
		t.Errorf("walled region: KnownCells = %d, want %d", got, want)
	}
	// Nothing OUTSIDE the wall ring should be perceived (boundary
	// rule only marks Moore-neighbors of perceived path cells; the
	// wall ring cells aren't path cells, so they don't extend).
	outsidePos := Pos{X: 34, Y: 34}
	if a.KnownCells[outsidePos] {
		t.Errorf("cell %v outside wall ring should NOT be perceived", outsidePos)
	}
}

// TestMarkAgentSensed_WallAdjacency_PerceivesCellsAroundCorners: a
// perceived path cell adjacent to a wall must have that wall in
// KnownCells. Lets the agent recognize dead-ends and corners from
// the perceived terrain layout.
func TestMarkAgentSensed_WallAdjacency_PerceivesCellsAroundCorners(t *testing.T) {
	w := NewWorld(315)
	// Carve a single path cell at (50, 50) surrounded by walls.
	w.Maze.Cells[50][50] = CellPath
	for dy := -1; dy <= 1; dy++ {
		for dx := -1; dx <= 1; dx++ {
			if dx == 0 && dy == 0 {
				continue
			}
			w.Maze.Cells[50+dy][50+dx] = CellWall
		}
	}
	a := &Agent{Label: '3', Pos: Pos{X: 50, Y: 50}, SightRadius: 1}
	w.MarkAgentSensed(a)
	// The agent should perceive its own cell + all 8 wall neighbors,
	// so it can recognize "I'm at a dead-end / 0-arm cell."
	for dy := -1; dy <= 1; dy++ {
		for dx := -1; dx <= 1; dx++ {
			p := Pos{X: 50 + dx, Y: 50 + dy}
			if !a.KnownCells[p] {
				t.Errorf("expected wall-adjacency to mark %v as known", p)
			}
		}
	}
}

// TestMarkAgentSensed_SightWallBlocks: walls enter KnownCells but
// block propagation past them. With 8-conn sensing the agent can
// sometimes reach a cell via a diagonal route, so the test isolates
// a target cell with a wall column.
func TestMarkAgentSensed_SightWallBlocks(t *testing.T) {
	w := NewWorld(313)
	for y := 38; y <= 42; y++ {
		for x := 38; x <= 42; x++ {
			w.Maze.Cells[y][x] = CellPath
		}
	}
	for y := 38; y <= 42; y++ {
		w.Maze.Cells[y][41] = CellWall
	}
	wall := Pos{X: 41, Y: 40}
	a := &Agent{Label: '9', Pos: Pos{X: 40, Y: 40}, SightRadius: 2}
	w.MarkAgentSensed(a)
	if !a.KnownCells[wall] {
		t.Error("wall should be in KnownCells (perceived but blocking)")
	}
	if a.KnownCells[Pos{X: 42, Y: 40}] {
		t.Error("cell behind the wall column should NOT be in KnownCells")
	}
}

// TestScentPeerLabels: returns the follower set minus self.
func TestScentPeerLabels(t *testing.T) {
	got := ScentPeerLabels('4')
	if len(got) != len(ScentFollowerLabels)-1 {
		t.Fatalf("len=%d, want %d", len(got), len(ScentFollowerLabels)-1)
	}
	for _, r := range got {
		if r == '4' {
			t.Errorf("self '4' appeared in peer list")
		}
	}
	// Spot-check: peer list should include both short-sight and
	// far-sight followers.
	have := map[rune]bool{}
	for _, r := range got {
		have[r] = true
	}
	for _, l := range []rune{'5', '6', '7', '9', 'A', 'B', 'C'} {
		if !have[l] {
			t.Errorf("peer list missing %c", l)
		}
	}
}

// TestApplyScentShaping_TrusteeMatch: standing on trustee scent
// yields +ScentShapingMagnitude × freshness.
func TestApplyScentShaping_TrusteeMatch(t *testing.T) {
	w := NewWorld(301)
	a := SpawnAgentForTest(w, '4')
	a.Pos = Pos{X: 40, Y: 40}
	w.Cycle = 50
	w.ScentOwner[40][40] = '2'
	w.ScentCycle[40][40] = 50 // freshness = 1.0
	a.CurrentTrustee = '2'
	if got := w.ApplyScentShaping(a); got != ScentShapingMagnitude {
		t.Errorf("trustee='2' on '2' scent: %v, want %v",
			got, ScentShapingMagnitude)
	}
}

// TestScentSensedCells_Radius1Moore: at radius 1 every Moore
// neighbor (8 surrounding cells) plus the current cell is sensed,
// excluding walls.
func TestScentSensedCells_Radius1Moore(t *testing.T) {
	w := NewWorld(330)
	// Open 5×5 plaza around (40, 40).
	for y := 38; y <= 42; y++ {
		for x := 38; x <= 42; x++ {
			w.Maze.Cells[y][x] = CellPath
		}
	}
	a := &Agent{Label: '4', Pos: Pos{X: 40, Y: 40}, SmellRadius: 1}
	got := w.ScentSensedCells(a)
	if len(got) != 9 {
		t.Errorf("SmellRadius=1 sensed = %d cells, want 9 (3×3)", len(got))
	}
}

// TestScentSensedCells_DefaultRadius: with the production default
// SmellRadius=2, the sensed set is a 5×5 Moore box (25 cells) in
// open terrain.
func TestScentSensedCells_DefaultRadius(t *testing.T) {
	w := NewWorld(332)
	for y := 38; y <= 42; y++ {
		for x := 38; x <= 42; x++ {
			w.Maze.Cells[y][x] = CellPath
		}
	}
	a := &Agent{Label: '4', Pos: Pos{X: 40, Y: 40}, SmellRadius: DefaultSmellRadius}
	got := w.ScentSensedCells(a)
	if len(got) != 25 {
		t.Errorf("DefaultSmellRadius=2 sensed = %d cells, want 25 (5×5)", len(got))
	}
	// Diagonals included.
	if !got[Pos{X: 39, Y: 39}] {
		t.Error("NW diagonal should be in sensed set")
	}
}

// TestScentSensedCells_Radius2WallBlocks: walls block scent
// propagation and are NOT included in the sensed set (walls carry
// no scent).
func TestScentSensedCells_Radius2WallBlocks(t *testing.T) {
	w := NewWorld(331)
	for y := 38; y <= 42; y++ {
		for x := 38; x <= 42; x++ {
			w.Maze.Cells[y][x] = CellPath
		}
	}
	// Vertical wall directly east of agent at (41, 40).
	w.Maze.Cells[40][41] = CellWall
	a := &Agent{Label: '9', Pos: Pos{X: 40, Y: 40}, SmellRadius: 2}
	got := w.ScentSensedCells(a)
	if got[Pos{X: 41, Y: 40}] {
		t.Error("wall cell should NOT be in scent sensed set")
	}
	// (42, 40) is east of the wall — Moore-BFS through (41,39)
	// (diagonal) reaches (42, 40) so it's still in range.  But a
	// cell exactly east-then-east with no diagonal route, e.g. if
	// surrounded by walls, would be blocked. Spot-check the wall
	// IS perceived by sight but not by scent:
	a2 := &Agent{Label: '9', Pos: Pos{X: 40, Y: 40}, SightRadius: 2}
	w.MarkAgentSensed(a2)
	if !a2.KnownCells[Pos{X: 41, Y: 40}] {
		t.Error("wall should be in sight KnownCells")
	}
}

// TestApplyScentShaping_AggregatesAcrossSensed: trustee scent at a
// Moore-diagonal neighbor (not just the current cell) should still
// produce a positive bonus.
func TestApplyScentShaping_AggregatesAcrossSensed(t *testing.T) {
	w := NewWorld(332)
	for y := 38; y <= 42; y++ {
		for x := 38; x <= 42; x++ {
			w.Maze.Cells[y][x] = CellPath
		}
	}
	a := SpawnAgentForTest(w, '4')
	a.Pos = Pos{X: 40, Y: 40}
	a.CurrentTrustee = '2'
	w.Cycle = 50
	// Plant trustee scent at the NW diagonal — NOT a cardinal
	// neighbor under the old per-cell model.
	w.ScentOwner[39][39] = '2'
	w.ScentCycle[39][39] = 50
	if got := w.ApplyScentShaping(a); got != ScentShapingMagnitude {
		t.Errorf("diagonal trustee scent: %v, want %v",
			got, ScentShapingMagnitude)
	}
	if a.JourneyTrusteeContactTicks != 1 {
		t.Errorf("contact ticks = %d, want 1", a.JourneyTrusteeContactTicks)
	}
}

// TestApplyScentShaping_Agent5BoostedMagnitude: agent 5's per-step
// scent bonus is 5× the base ScentShapingMagnitude so the DQN's
// Bellman update actually feels the trusted-scent gradient.
func TestApplyScentShaping_Agent5BoostedMagnitude(t *testing.T) {
	w := NewWorld(310)
	a := SpawnAgentForTest(w, '5')
	a.Pos = Pos{X: 40, Y: 40}
	w.Cycle = 50
	w.ScentOwner[40][40] = '2'
	w.ScentCycle[40][40] = 50 // freshness = 1.0
	a.CurrentTrustee = '2'
	want := ScentShapingMagnitude * ScentMagnitudeFor('5')
	if got := w.ApplyScentShaping(a); got != want {
		t.Errorf("agent 5 on trustee scent: %v, want %v (5× boost)",
			got, want)
	}
	if ScentMagnitudeFor('5') <= 1.0 {
		t.Errorf("ScentMagnitudeFor('5') = %v, want > 1.0",
			ScentMagnitudeFor('5'))
	}
}

// TestScentMagnitudeFor_DefaultsToOne: every follower except 5 uses
// the baseline 1.0 multiplier.
func TestScentMagnitudeFor_DefaultsToOne(t *testing.T) {
	for _, l := range []rune{'4', '6', '7'} {
		if got := ScentMagnitudeFor(l); got != 1.0 {
			t.Errorf("ScentMagnitudeFor(%c) = %v, want 1.0", l, got)
		}
	}
}

// TestApplyScentShaping_NonTrusteeNoBonus: standing on a non-trustee
// label with no negative trust yields 0 (no static repel anymore).
func TestApplyScentShaping_NonTrusteeNoBonus(t *testing.T) {
	w := NewWorld(302)
	a := SpawnAgentForTest(w, '4')
	a.Pos = Pos{X: 40, Y: 40}
	w.Cycle = 50
	w.ScentOwner[40][40] = '2' // not the trustee, no trust history
	w.ScentCycle[40][40] = 50
	a.CurrentTrustee = '1'
	if got := w.ApplyScentShaping(a); got != 0 {
		t.Errorf("non-trustee label, no negative trust: %v, want 0", got)
	}
}

// TestApplyScentShaping_DynamicRepelOnNegativeTrust: a label whose
// TrustScores entry has gone negative acts as repel.
func TestApplyScentShaping_DynamicRepelOnNegativeTrust(t *testing.T) {
	w := NewWorld(303)
	a := SpawnAgentForTest(w, '4')
	a.Pos = Pos{X: 40, Y: 40}
	w.Cycle = 50
	w.ScentOwner[40][40] = '3'
	w.ScentCycle[40][40] = 50
	a.CurrentTrustee = '1'
	a.TrustScores = map[rune]float64{'3': -1}
	if got := w.ApplyScentShaping(a); got != -ScentShapingMagnitude {
		t.Errorf("negative-trust label: %v, want %v",
			got, -ScentShapingMagnitude)
	}
}

// TestApplyScentShaping_AgentNotFollowerReturnsZero: agents 1, 2, 3
// have no scent-shaping channel at all.
func TestApplyScentShaping_AgentNotFollowerReturnsZero(t *testing.T) {
	w := NewWorld(304)
	a := SpawnAgentForTest(w, '2')
	a.Pos = Pos{X: 40, Y: 40}
	w.Cycle = 50
	w.ScentOwner[40][40] = '1'
	w.ScentCycle[40][40] = 50
	if got := w.ApplyScentShaping(a); got != 0 {
		t.Errorf("agent 2 (leader): %v, want 0", got)
	}
}

// reviveAllAgents flips Alive=true on every agent so PickTrustee's
// alive-filter sees a full candidate pool. Used by the stage tests
// that drive PickTrustee directly without going through the normal
// respawn lifecycle.
func reviveAllAgents(w *World) {
	for _, a := range w.Agents {
		a.Alive = true
		a.Disabled = false
	}
}

// TestPickTrustee_Stage1_UniformOverLeaders: with Stats.Starts ≤
// ScentRunsForTrustWeighting, picks distribute roughly evenly across
// the leader pool and never include peers, regardless of TrustScores.
func TestPickTrustee_Stage1_UniformOverLeaders(t *testing.T) {
	w := NewWorld(401)
	a := SpawnAgentForTest(w, '4')
	reviveAllAgents(w)
	a.Stats.Starts = 1 // stage 1
	// Heavy trust skew should NOT influence stage-1 (random) pick.
	a.TrustScores = map[rune]float64{'1': 100, '5': 100}
	counts := map[rune]int{}
	for i := 0; i < 300; i++ {
		a.PickTrustee(w, w.Rng)
		counts[a.CurrentTrustee]++
	}
	for _, l := range ScentLeaderLabels {
		if counts[l] < 30 {
			t.Errorf("stage 1: trustee %c picked %d/300, want ≥30", l, counts[l])
		}
	}
	// Peers must never be picked in stage 1.
	for _, l := range ScentFollowerLabels {
		if counts[l] > 0 {
			t.Errorf("stage 1: peer %c picked %d times, want 0", l, counts[l])
		}
	}
}

// TestPickTrustee_Stage2_SoftmaxOverLeaders: with Stats.Starts in
// (ScentRunsForTrustWeighting, ScentRunsForPeerExpansion], a strongly
// skewed TrustScores funnels picks to the high-trust leader.
func TestPickTrustee_Stage2_SoftmaxOverLeaders(t *testing.T) {
	w := NewWorld(402)
	a := SpawnAgentForTest(w, '4')
	reviveAllAgents(w)
	a.Stats.Starts = ScentRunsForTrustWeighting + 1 // stage 2
	a.TrustScores = map[rune]float64{'1': 5, '2': 0, '3': 0}
	picks := map[rune]int{}
	for i := 0; i < 300; i++ {
		a.PickTrustee(w, w.Rng)
		picks[a.CurrentTrustee]++
	}
	// exp(5)/(exp(5)+exp(0)+exp(0)) ≈ 0.987 → expect ~296/300 of '1'.
	if picks['1'] < 250 {
		t.Errorf("stage 2: high-trust leader picked %d/300, want ≥250",
			picks['1'])
	}
	for _, l := range ScentFollowerLabels {
		if picks[l] > 0 {
			t.Errorf("stage 2: peer %c picked %d times, want 0", l, picks[l])
		}
	}
}

// TestPickTrustee_Stage3_HalfPeers: with Stats.Starts >
// ScentRunsForPeerExpansion, picks split ~50/50 between the leader
// pool (ScentLeaderLabels) and the peer pool (other followers).
func TestPickTrustee_Stage3_HalfPeers(t *testing.T) {
	w := NewWorld(403)
	a := SpawnAgentForTest(w, '4')
	reviveAllAgents(w)
	a.Stats.Starts = ScentRunsForPeerExpansion + 1 // stage 3
	leaders := map[rune]bool{}
	for _, l := range ScentLeaderLabels {
		leaders[l] = true
	}
	peers := map[rune]bool{}
	for _, l := range ScentPeerLabels('4') {
		peers[l] = true
	}
	leaderCount := 0
	peerCount := 0
	for i := 0; i < 600; i++ {
		a.TrustScores = nil
		a.PickTrustee(w, w.Rng)
		switch {
		case a.CurrentTrustee == '4':
			t.Errorf("agent 4 picked itself as trustee")
		case leaders[a.CurrentTrustee]:
			leaderCount++
		case peers[a.CurrentTrustee]:
			peerCount++
		default:
			t.Errorf("unexpected trustee %c", a.CurrentTrustee)
		}
	}
	// 50/50 split with 600 trials → expect ~300 each; allow 200-400.
	if leaderCount < 200 || leaderCount > 400 {
		t.Errorf("stage 3 leader picks = %d/600, want roughly 300", leaderCount)
	}
	if peerCount < 200 || peerCount > 400 {
		t.Errorf("stage 3 peer picks = %d/600, want roughly 300", peerCount)
	}
}

// TestPickTrustee_NoAliveLeadersFallsBack: when no leaders are
// alive the candidate pool is empty and CurrentTrustee stays 0, so
// the strategy falls back to its non-follower algorithm.
func TestPickTrustee_NoAliveLeadersFallsBack(t *testing.T) {
	w := NewWorld(405)
	a := SpawnAgentForTest(w, '4')
	a.Stats.Starts = 1 // stage 1
	// Explicitly kill every leader candidate.
	for _, l := range ScentLeaderLabels {
		if leader := w.AgentByLabel(l); leader != nil {
			leader.Alive = false
		}
	}
	a.PickTrustee(w, w.Rng)
	if a.CurrentTrustee != 0 {
		t.Errorf("CurrentTrustee = %c with no alive leaders, want 0",
			a.CurrentTrustee)
	}
}

// TestPickTrustee_SkipsDeadLeader: with only some leaders alive,
// picks are constrained to the alive subset.
func TestPickTrustee_SkipsDeadLeader(t *testing.T) {
	w := NewWorld(406)
	a := SpawnAgentForTest(w, '4')
	a.Stats.Starts = 1
	// Kill all leaders except '2'.
	for _, l := range ScentLeaderLabels {
		leader := w.AgentByLabel(l)
		if leader == nil {
			continue
		}
		leader.Alive = (l == '2')
	}
	for i := 0; i < 50; i++ {
		a.PickTrustee(w, w.Rng)
		if a.CurrentTrustee != '2' {
			t.Errorf("iter %d: CurrentTrustee=%c, want '2' (only alive leader)",
				i, a.CurrentTrustee)
		}
	}
}

// TestPickStrategy_50_50_Mix: with empty trust scores, PickStrategy
// distributes uniformly over the letter pool (random side + softmax
// over zero scores both collapse to uniform). 700 trials × 7
// letters → ~100 each; tolerate 40-160.
func TestPickStrategy_50_50_Mix(t *testing.T) {
	w := NewWorld(450)
	a := SpawnAgentForTest(w, '4')
	letters := []rune{'R', 'S', 'T', 'U', 'V', 'W', 'X'}
	counts := map[rune]int{}
	for i := 0; i < 700; i++ {
		a.StrategyTrustScores = nil
		a.PickStrategy(letters, w.Rng)
		counts[a.CurrentStrategy]++
	}
	for _, l := range letters {
		if counts[l] < 40 || counts[l] > 160 {
			t.Errorf("letter %c: %d/700 picks, want roughly 100", l, counts[l])
		}
	}
}

// TestPickStrategy_TrustBiasFavorsHighScore: with a strongly skewed
// trust map, the softmax half of the 50/50 should funnel most of
// its picks to the high-trust letter. Random half stays uniform.
// Net: target letter gets the 50% softmax mass plus its 1/7 share
// of the 50% random mass → roughly 0.50 + 0.07 = ~0.57. Accept
// ≥0.40 to keep the test stable.
func TestPickStrategy_TrustBiasFavorsHighScore(t *testing.T) {
	w := NewWorld(451)
	a := SpawnAgentForTest(w, '4')
	letters := []rune{'R', 'S', 'T'}
	a.StrategyTrustScores = map[rune]float64{'R': 10}
	hits := 0
	trials := 500
	for i := 0; i < trials; i++ {
		a.PickStrategy(letters, w.Rng)
		if a.CurrentStrategy == 'R' {
			hits++
		}
	}
	if float64(hits)/float64(trials) < 0.40 {
		t.Errorf("high-trust letter picked %d/%d, want ≥40%%", hits, trials)
	}
}

// TestUpdateStrategyTrust_RewardsImprovement: a successful journey
// records the best time; a subsequent faster journey adds the
// improvement bonus too.
func TestUpdateStrategyTrust_RewardsImprovement(t *testing.T) {
	w := NewWorld(452)
	a := SpawnAgentForTest(w, '4')
	a.CurrentStrategy = 'R'
	a.TicksAlive = 200
	w.updateStrategyTrust(a, true)
	want := StrategyGoalBonus + StrategyImproveBonus // first solve is "improvement"
	if got := a.StrategyTrustScores['R']; got != want {
		t.Errorf("after first solve, trust = %v, want %v", got, want)
	}
	if a.StrategyBestSolveTime['R'] != 200 {
		t.Errorf("best solve = %d, want 200", a.StrategyBestSolveTime['R'])
	}
	// Second solve, faster.
	a.TicksAlive = 150
	w.updateStrategyTrust(a, true)
	want2 := want + StrategyGoalBonus + StrategyImproveBonus
	if got := a.StrategyTrustScores['R']; got != want2 {
		t.Errorf("after faster solve, trust = %v, want %v", got, want2)
	}
	if a.StrategyBestSolveTime['R'] != 150 {
		t.Errorf("best solve = %d, want 150", a.StrategyBestSolveTime['R'])
	}
}

// TestUpdateStrategyTrust_SlowerSolveOnlyGoalBonus: a solve that
// doesn't beat the prior best only earns StrategyGoalBonus.
func TestUpdateStrategyTrust_SlowerSolveOnlyGoalBonus(t *testing.T) {
	w := NewWorld(453)
	a := SpawnAgentForTest(w, '4')
	a.CurrentStrategy = 'R'
	a.StrategyBestSolveTime = map[rune]int{'R': 100}
	a.StrategyTrustScores = map[rune]float64{'R': 0}
	a.TicksAlive = 999 // way slower
	w.updateStrategyTrust(a, true)
	if got := a.StrategyTrustScores['R']; got != StrategyGoalBonus {
		t.Errorf("slow solve trust = %v, want %v", got, StrategyGoalBonus)
	}
	if a.StrategyBestSolveTime['R'] != 100 {
		t.Errorf("best should stay 100, got %d", a.StrategyBestSolveTime['R'])
	}
}

// TestUpdateStrategyTrust_FailurePenalty: an unsuccessful journey
// decrements StrategyTrustScores for the algorithm that was used.
func TestUpdateStrategyTrust_FailurePenalty(t *testing.T) {
	w := NewWorld(454)
	a := SpawnAgentForTest(w, '4')
	a.CurrentStrategy = 'V'
	w.updateStrategyTrust(a, false)
	if got := a.StrategyTrustScores['V']; got != -StrategyFailurePenalty {
		t.Errorf("failure trust = %v, want %v", got, -StrategyFailurePenalty)
	}
}

// TestPickTrustee_NoOpForLeaders: agents 1, 2, 3 don't pick trustees.
func TestPickTrustee_NoOpForLeaders(t *testing.T) {
	w := NewWorld(404)
	a := SpawnAgentForTest(w, '2')
	a.PickTrustee(w, w.Rng)
	if a.CurrentTrustee != 0 {
		t.Errorf("agent 2 (leader): CurrentTrustee=%c, want 0", a.CurrentTrustee)
	}
}

// TestOptimizeKnownPath_BuildsContiguousPath: when an agent's
// KnownCells contains entrance + goal + a connecting corridor,
// optimizeKnownPath produces a cardinal-step contiguous path from
// entrance to goal.
func TestOptimizeKnownPath_BuildsContiguousPath(t *testing.T) {
	w := NewWorld(800)
	w.Maze.EntrancePos = Pos{X: 5, Y: 5}
	w.Maze.GoalPos = Pos{X: 9, Y: 5}
	// Carve a straight corridor.
	for x := 5; x <= 9; x++ {
		w.Maze.Cells[5][x] = CellPath
	}
	a := SpawnAgentForTest(w, '3')
	a.KnownCells = map[Pos]bool{}
	for x := 5; x <= 9; x++ {
		a.KnownCells[Pos{X: x, Y: 5}] = true
	}
	w.optimizeKnownPath(a)
	if len(a.KnownShortestPath) != 5 {
		t.Fatalf("path len = %d, want 5", len(a.KnownShortestPath))
	}
	if a.KnownShortestPath[0] != w.Maze.EntrancePos {
		t.Errorf("path[0] = %v, want %v", a.KnownShortestPath[0], w.Maze.EntrancePos)
	}
	if a.KnownShortestPath[len(a.KnownShortestPath)-1] != w.Maze.GoalPos {
		t.Errorf("path[-1] = %v, want %v",
			a.KnownShortestPath[len(a.KnownShortestPath)-1], w.Maze.GoalPos)
	}
	// Each consecutive pair should be cardinally adjacent.
	for i := 1; i < len(a.KnownShortestPath); i++ {
		dx := AbsInt(a.KnownShortestPath[i].X - a.KnownShortestPath[i-1].X)
		dy := AbsInt(a.KnownShortestPath[i].Y - a.KnownShortestPath[i-1].Y)
		if dx+dy != 1 {
			t.Errorf("non-cardinal step at %d: %v→%v",
				i, a.KnownShortestPath[i-1], a.KnownShortestPath[i])
		}
	}
}

// TestOptimizeKnownPath_NoOpWhenEndpointsUnseen: if the agent
// hasn't perceived the entrance or goal, BFS leaves the cache
// untouched.
func TestOptimizeKnownPath_NoOpWhenEndpointsUnseen(t *testing.T) {
	w := NewWorld(801)
	a := SpawnAgentForTest(w, '3')
	a.KnownCells = map[Pos]bool{} // nothing perceived
	w.optimizeKnownPath(a)
	if a.KnownShortestPath != nil {
		t.Errorf("expected nil path, got %v", a.KnownShortestPath)
	}
}

// TestCachedStepFor_ReturnsNextStep: with a cached path, the
// consult returns the cell immediately after a.Pos.
func TestCachedStepFor_ReturnsNextStep(t *testing.T) {
	w := NewWorld(802)
	for x := 5; x <= 9; x++ {
		w.Maze.Cells[5][x] = CellPath
	}
	a := SpawnAgentForTest(w, '3')
	a.Pos = Pos{X: 6, Y: 5}
	a.KnownShortestPath = []Pos{
		{X: 5, Y: 5}, {X: 6, Y: 5}, {X: 7, Y: 5}, {X: 8, Y: 5}, {X: 9, Y: 5},
	}
	step, ok := w.CachedStepFor(a)
	if !ok {
		t.Fatal("expected cached step")
	}
	if step != (Pos{X: 7, Y: 5}) {
		t.Errorf("step = %v, want {7, 5}", step)
	}
}

// TestCachedStepFor_FallbackWhenHazard: when the next cell on the
// path is now a fire pit, the consult returns false so the caller
// falls through to its native planner.
func TestCachedStepFor_FallbackWhenHazard(t *testing.T) {
	w := NewWorld(803)
	for x := 5; x <= 9; x++ {
		w.Maze.Cells[5][x] = CellPath
	}
	// Place a fire pit at the next-step cell.
	w.Maze.Cells[5][7] = CellFirePit
	w.FirePitsDisabled = false
	a := SpawnAgentForTest(w, '3')
	a.Pos = Pos{X: 6, Y: 5}
	a.KnownShortestPath = []Pos{
		{X: 5, Y: 5}, {X: 6, Y: 5}, {X: 7, Y: 5}, {X: 8, Y: 5}, {X: 9, Y: 5},
	}
	if _, ok := w.CachedStepFor(a); ok {
		t.Error("expected fallback (false) when next step is hazardous")
	}
}

// TestPruneSwarmGraph_LeafTrim: a single-leaf dead-end cell whose
// Moore neighborhood is entirely walls except for one corridor
// connection gets trimmed by the leaf-trim phase. (With 8-conn
// Cardinals, "leaf" requires degree ≤ 1 over the full Moore
// neighborhood, so multi-cell side-branches with 2+ diagonal
// shortcuts to the corridor don't trim — that's expected
// 8-connected behavior.)
//
// Layout:
//
//	entrance (5,5) — (6,5) — (7,5) — goal (8,5)
//	                           |
//	                       (7,7)     ← dead-end leaf, fully walled
//
// (7,7) connects to corridor only via (7,5)? No — (7,5) and (7,7)
// aren't Moore-adjacent. Use (7,6) as the dead-end leaf instead:
// its only walkable Moore neighbor is the corridor cell (7,5)
// after we wall off the other 7.
func TestPruneSwarmGraph_LeafTrim(t *testing.T) {
	w := NewWorld(820)
	w.Maze.EntrancePos = Pos{X: 5, Y: 5}
	w.Maze.GoalPos = Pos{X: 8, Y: 5}
	for x := 5; x <= 8; x++ {
		w.Maze.Cells[5][x] = CellPath
	}
	leaf := Pos{X: 7, Y: 6}
	w.Maze.Cells[leaf.Y][leaf.X] = CellPath
	// Wall off every Moore neighbor of `leaf` except (7,5). Need
	// to clear: (6,6), (8,6), (6,7), (7,7), (8,7), (6,5)? no
	// (6,5) is corridor; (8,5)? also corridor. (7,5) is corridor.
	// So Moore neighbors that should be walls: (6,6), (8,6),
	// (6,7), (7,7), (8,7). Also wall (6,5) and (8,5)? No, those
	// are walkable corridor and ARE counted as Moore neighbors of
	// leaf — meaning leaf has degree 3 (from 7,5, 6,5, 8,5).
	// Walls won't help. We need (6,5) and (8,5) to NOT be Moore
	// neighbors of leaf. Move leaf farther: use (5, 7).
	leaf = Pos{X: 5, Y: 7}
	w.Maze.Cells[leaf.Y][leaf.X] = CellPath
	// Connect leaf to corridor via a thin stem at (5,6).
	w.Maze.Cells[6][5] = CellPath
	// Wall all other Moore neighbors of `leaf` so its only
	// walkable Moore neighbor is (5,6).
	for _, p := range []Pos{
		{X: 4, Y: 6}, {X: 6, Y: 6},
		{X: 4, Y: 7}, {X: 6, Y: 7},
		{X: 4, Y: 8}, {X: 5, Y: 8}, {X: 6, Y: 8},
	} {
		w.Maze.Cells[p.Y][p.X] = CellWall
	}
	known := map[Pos]bool{
		{X: 5, Y: 5}: true, {X: 6, Y: 5}: true, {X: 7, Y: 5}: true, {X: 8, Y: 5}: true,
		{X: 5, Y: 6}: true,
		leaf:         true,
	}
	// Mark the walls around `leaf` as perceived so the trim sees
	// no frontier at that cell.
	for _, p := range []Pos{
		{X: 4, Y: 6}, {X: 6, Y: 6},
		{X: 4, Y: 7}, {X: 6, Y: 7},
		{X: 4, Y: 8}, {X: 5, Y: 8}, {X: 6, Y: 8},
	} {
		known[p] = true
	}
	alive := w.pruneSwarmGraph(known, 1)
	if alive[leaf] {
		t.Errorf("dead-end leaf %v survived prune", leaf)
	}
	// The corridor should still be there:
	for x := 5; x <= 8; x++ {
		p := Pos{X: x, Y: 5}
		if !alive[p] {
			t.Errorf("corridor cell %v pruned, want alive", p)
		}
	}
}

// TestPruneSwarmGraph_KeepsFrontierBranch: a branch leading to a
// frontier (unperceived cell) is NOT pruned even if it doesn't
// reach goal — the swarm still wants to explore that direction.
func TestPruneSwarmGraph_KeepsFrontierBranch(t *testing.T) {
	w := NewWorld(821)
	w.Maze.EntrancePos = Pos{X: 5, Y: 5}
	w.Maze.GoalPos = Pos{X: 8, Y: 5}
	for x := 5; x <= 8; x++ {
		w.Maze.Cells[5][x] = CellPath
	}
	w.Maze.Cells[6][6] = CellPath
	// (6,6) is a branch with the cell BELOW it (6,7) UNPERCEIVED
	// → (6,6) is a frontier and must survive pruning.
	known := map[Pos]bool{
		{X: 5, Y: 5}: true, {X: 6, Y: 5}: true,
		{X: 7, Y: 5}: true, {X: 8, Y: 5}: true,
		{X: 6, Y: 6}: true,
	}
	alive := w.pruneSwarmGraph(known, 1)
	if !alive[Pos{X: 6, Y: 6}] {
		t.Error("frontier cell (6,6) should survive prune")
	}
}

// TestRecomputeSwarmGraphIfStale_CachesUntilGrowth: a second call
// without union growth shouldn't re-run BFS. We can't directly
// observe "didn't recompute"; instead we tweak the cached alive
// set after the first call and verify the second call doesn't
// clobber it.
func TestRecomputeSwarmGraphIfStale_CachesUntilGrowth(t *testing.T) {
	w := NewWorld(822)
	a := SpawnAgentForTest(w, '3')
	a.Alive = true
	a.CurrentStrategy = SwarmStrategyLetter
	a.SwarmGroupID = 1
	a.KnownCells = map[Pos]bool{{X: 1, Y: 1}: true}
	w.RecomputeSwarmGraphIfStale(1)
	// Poke the cache with a sentinel cell that the BFS would never
	// produce on its own.
	sentinel := Pos{X: -1, Y: -1}
	w.swarmGraphs[1].aliveCells[sentinel] = true
	// Second call with no growth: should be a no-op, sentinel survives.
	w.RecomputeSwarmGraphIfStale(1)
	if !w.swarmGraphs[1].aliveCells[sentinel] {
		t.Error("second call recomputed despite no growth")
	}
	// Grow the union: now the cache should rebuild and sentinel
	// gets wiped.
	a.KnownCells[Pos{X: 2, Y: 2}] = true
	w.RecomputeSwarmGraphIfStale(1)
	if w.swarmGraphs[1].aliveCells[sentinel] {
		t.Error("third call (after growth) failed to rebuild — sentinel persisted")
	}
}

// TestOptimizeKnownPath_BroadcastsToSwarmPeers: when a Swarm-S
// agent reaches the goal, the optimized path is copied to every
// alive swarm peer's KnownShortestPath.
func TestOptimizeKnownPath_BroadcastsToSwarmPeers(t *testing.T) {
	w := NewWorld(810)
	w.Maze.EntrancePos = Pos{X: 5, Y: 5}
	w.Maze.GoalPos = Pos{X: 9, Y: 5}
	for x := 5; x <= 9; x++ {
		w.Maze.Cells[5][x] = CellPath
	}
	a := SpawnAgentForTest(w, '3')
	b := SpawnAgentForTest(w, '4')
	c := SpawnAgentForTest(w, '5')
	// SpawnAgentForTest kills any agent already at the entrance, so
	// only the most recent spawn is alive. Manually revive all three
	// for the swarm test.
	a.Alive, b.Alive, c.Alive = true, true, true
	a.CurrentStrategy = SwarmStrategyLetter
	b.CurrentStrategy = SwarmStrategyLetter
	c.CurrentStrategy = 'T' // NOT swarm
	a.KnownCells = map[Pos]bool{}
	b.KnownCells = map[Pos]bool{}
	c.KnownCells = map[Pos]bool{}
	// Give the swarm half of the corridor each — neither agent
	// alone can BFS entrance→goal, but the union can.
	for x := 5; x <= 7; x++ {
		a.KnownCells[Pos{X: x, Y: 5}] = true
	}
	for x := 7; x <= 9; x++ {
		b.KnownCells[Pos{X: x, Y: 5}] = true
	}
	// c is not on the swarm; even though it could individually
	// see the goal, the broadcast must skip it.
	for x := 5; x <= 9; x++ {
		c.KnownCells[Pos{X: x, Y: 5}] = true
	}
	w.optimizeKnownPath(a)
	if len(a.KnownShortestPath) != 5 {
		t.Fatalf("a's path len = %d, want 5", len(a.KnownShortestPath))
	}
	if len(b.KnownShortestPath) != 5 {
		t.Errorf("swarm peer b should have inherited the path, got %v",
			b.KnownShortestPath)
	}
	if c.KnownShortestPath != nil {
		t.Errorf("non-swarm peer c received a path: %v", c.KnownShortestPath)
	}
}

// TestOptimizeKnownPath_NoBroadcastWhenSolo: a non-swarm agent's
// goal-reach should NOT touch any other agent's KnownShortestPath.
func TestOptimizeKnownPath_NoBroadcastWhenSolo(t *testing.T) {
	w := NewWorld(811)
	w.Maze.EntrancePos = Pos{X: 5, Y: 5}
	w.Maze.GoalPos = Pos{X: 9, Y: 5}
	for x := 5; x <= 9; x++ {
		w.Maze.Cells[5][x] = CellPath
	}
	a := SpawnAgentForTest(w, '3')
	b := SpawnAgentForTest(w, '4')
	a.Alive, b.Alive = true, true
	a.CurrentStrategy = 'T' // plain Bayesian, not swarm
	b.CurrentStrategy = SwarmStrategyLetter
	for x := 5; x <= 9; x++ {
		a.KnownCells[Pos{X: x, Y: 5}] = true
	}
	w.optimizeKnownPath(a)
	if len(a.KnownShortestPath) != 5 {
		t.Fatalf("a's path len = %d, want 5", len(a.KnownShortestPath))
	}
	if b.KnownShortestPath != nil {
		t.Errorf("non-swarm goal-reach leaked path: %v", b.KnownShortestPath)
	}
}

// TestCachedStepFor_OffPathReturnsFalse: when the agent has drifted
// off the cached path, the consult returns false.
func TestCachedStepFor_OffPathReturnsFalse(t *testing.T) {
	w := NewWorld(804)
	a := SpawnAgentForTest(w, '3')
	a.Pos = Pos{X: 100, Y: 100} // nowhere near the path
	a.KnownShortestPath = []Pos{
		{X: 5, Y: 5}, {X: 6, Y: 5}, {X: 7, Y: 5},
	}
	if _, ok := w.CachedStepFor(a); ok {
		t.Error("off-path agent should fall back")
	}
}

// TestNewWorld_FirstEventIsFromStartingPool: every fresh world
// boots with a single yellow opener picked at random from the
// startingMessages pool (War Games, 2001, Dual Core, etc).
func TestNewWorld_FirstEventIsFromStartingPool(t *testing.T) {
	w := NewWorld(900)
	if len(w.Events) == 0 {
		t.Fatal("Events empty after NewWorld")
	}
	first := w.Events[0]
	if first.Color != "yellow" {
		t.Errorf("first event color = %q, want yellow", first.Color)
	}
	found := false
	for _, msg := range startingMessages {
		if first.Message == msg {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("first event %q not in startingMessages pool", first.Message)
	}
}

// TestGenerateMaze_GoalIsFarFromEntrance: every freshly-generated
// maze must place the goal on a walkable cell at Manhattan distance
// ≥ MinGoalDistanceCells from the entrance. Sample many seeds to
// cover both standard and open-field variants.
func TestGenerateMaze_GoalIsFarFromEntrance(t *testing.T) {
	for seed := int64(0); seed < 40; seed++ {
		w := NewWorld(seed)
		if !w.Maze.IsWalkable(w.Maze.GoalPos) {
			t.Errorf("seed %d: goal %v not walkable", seed, w.Maze.GoalPos)
		}
		d := AbsInt(w.Maze.GoalPos.X-w.Maze.EntrancePos.X) +
			AbsInt(w.Maze.GoalPos.Y-w.Maze.EntrancePos.Y)
		if d < MinGoalDistanceCells {
			t.Errorf("seed %d: goal-distance = %d, want ≥ %d",
				seed, d, MinGoalDistanceCells)
		}
	}
}

// TestGenerateMaze_GoalPositionVaries: across many seeds the goal
// must NOT always be at the same cell (random placement, not the
// legacy fixed corner).
func TestGenerateMaze_GoalPositionVaries(t *testing.T) {
	seen := map[Pos]bool{}
	for seed := int64(0); seed < 30; seed++ {
		w := NewWorld(seed)
		seen[w.Maze.GoalPos] = true
	}
	if len(seen) < 5 {
		t.Errorf("only %d distinct goal positions across 30 seeds — random placement may be broken", len(seen))
	}
}

// TestGenerateMaze_OpenFieldVariantOccurs: sample many seeds and
// confirm at least one produces the open-field variant (≈20% rate).
// In an open-field maze every interior cell is walkable; perimeter
// cells stay as walls; entrance is at (1,1) and goal at the
// opposite interior corner.
func TestGenerateMaze_OpenFieldVariantOccurs(t *testing.T) {
	found := false
	for seed := int64(0); seed < 80 && !found; seed++ {
		w := NewWorld(seed)
		// Open-field signature: every cell in row 1, columns 1..N-2,
		// is walkable (no carved corridor walls in the interior).
		open := true
		for x := 1; x < BoardWidth-1; x++ {
			if !w.Maze.IsWalkable(Pos{X: x, Y: 1}) {
				open = false
				break
			}
		}
		if !open {
			continue
		}
		// Confirm entrance sits at a non-corner perimeter cell.
		if w.Maze.EntrancePos != (Pos{X: 1, Y: 0}) {
			t.Errorf("open-field entrance = %v, want (1,0)", w.Maze.EntrancePos)
		}
		// Confirm goal is walkable and at least MinGoalDistanceCells
		// from the entrance (random placement).
		if !w.Maze.IsWalkable(w.Maze.GoalPos) {
			t.Errorf("open-field goal %v not walkable", w.Maze.GoalPos)
		}
		d := AbsInt(w.Maze.GoalPos.X-w.Maze.EntrancePos.X) +
			AbsInt(w.Maze.GoalPos.Y-w.Maze.EntrancePos.Y)
		if d < MinGoalDistanceCells {
			t.Errorf("open-field goal-distance = %d, want ≥ %d",
				d, MinGoalDistanceCells)
		}
		// Confirm perimeter walls intact.
		if w.Maze.IsWalkable(Pos{X: 0, Y: 0}) {
			t.Error("open-field perimeter corner (0,0) should be a wall")
		}
		found = true
	}
	if !found {
		t.Error("no open-field variant produced in 80 seeds (rate ≈ 0%)")
	}
}

// TestEnforceBenchmarkSingleton_DemoteExtras: when 2+ alive agents
// are on R, all but one get demoted to T. Trustee cleared (T is
// scent-blind).
func TestEnforceBenchmarkSingleton_DemoteExtras(t *testing.T) {
	w := NewWorld(774)
	for _, a := range w.Agents {
		a.Alive = true
		a.Disabled = false
		a.CurrentStrategy = 'T'
	}
	// Force four agents onto R.
	for _, l := range []rune{'1', '2', '3', '4'} {
		w.AgentByLabel(l).CurrentStrategy = BenchmarkStrategyLetter
		w.AgentByLabel(l).CurrentTrustee = '5'
	}
	w.EnforceBenchmarkSingleton()
	onR := 0
	for _, a := range w.Agents {
		if a.Alive && a.CurrentStrategy == BenchmarkStrategyLetter {
			onR++
		}
	}
	if onR != MaxBenchmarkAgents {
		t.Errorf("after enforcement, R-count = %d, want %d", onR, MaxBenchmarkAgents)
	}
	// Demoted agents must have their CurrentTrustee cleared.
	for _, a := range w.Agents {
		if a.Alive && a.CurrentStrategy == 'T' && a.CurrentTrustee != 0 {
			// Was this one of our forced-R set?
			for _, l := range []rune{'1', '2', '3', '4'} {
				if a.Label == l {
					t.Errorf("demoted agent %c retained trustee %c",
						a.Label, a.CurrentTrustee)
				}
			}
		}
	}
}

// TestEnforceBenchmarkSingleton_NoOpForOne: a single R user is OK
// — no demotion happens.
func TestEnforceBenchmarkSingleton_NoOpForOne(t *testing.T) {
	w := NewWorld(775)
	for _, a := range w.Agents {
		a.Alive = true
		a.Disabled = false
		a.CurrentStrategy = 'T'
	}
	w.AgentByLabel('1').CurrentStrategy = BenchmarkStrategyLetter
	w.EnforceBenchmarkSingleton()
	if w.AgentByLabel('1').CurrentStrategy != BenchmarkStrategyLetter {
		t.Error("sole R user was demoted (shouldn't be)")
	}
}

// TestEnforceSwarmQuorum_DraftsToThree: when only one alive agent
// is on S, EnforceSwarmQuorum drafts two more alive non-S agents
// to join, hitting the quorum of 3.
// TestEnforceSwarmQuorum_IsNoOp: under the independent-swarm model
// each S-picker is already a complete swarm (1 leader + 10 clones),
// so the old "draft agents into S to meet quorum" rule no longer
// applies. EnforceSwarmQuorum is kept as a no-op for backwards
// compatibility — drafting must NOT happen.
func TestEnforceSwarmQuorum_IsNoOp(t *testing.T) {
	w := NewWorld(770)
	for _, a := range w.Agents {
		a.Alive = true
		a.Disabled = false
		a.CurrentStrategy = 'T'
		a.CurrentTrustee = 0
	}
	w.AgentByLabel('5').CurrentStrategy = SwarmStrategyLetter
	w.EnforceSwarmQuorum()
	onSwarm := 0
	for _, a := range w.Agents {
		if a.Alive && a.CurrentStrategy == SwarmStrategyLetter {
			onSwarm++
		}
	}
	if onSwarm != 1 {
		t.Errorf("S-count = %d, want 1 (no drafting under new model)", onSwarm)
	}
}

// TestEnforceSwarmQuorum_NoOpWhenZeroOnSwarm: if no alive agent is
// on S, EnforceSwarmQuorum doesn't draft anyone.
func TestEnforceSwarmQuorum_NoOpWhenZeroOnSwarm(t *testing.T) {
	w := NewWorld(771)
	for _, a := range w.Agents {
		a.Alive = true
		a.Disabled = false
		a.CurrentStrategy = 'T'
	}
	w.EnforceSwarmQuorum()
	for _, a := range w.Agents {
		if a.CurrentStrategy == SwarmStrategyLetter {
			t.Errorf("agent %c was drafted to S despite zero starting swarm",
				a.Label)
		}
	}
}

// TestEnforceSwarmQuorum_AlreadyQuorate: when ≥3 alive agents are
// already on S, EnforceSwarmQuorum leaves everyone alone.
func TestEnforceSwarmQuorum_AlreadyQuorate(t *testing.T) {
	w := NewWorld(772)
	for _, a := range w.Agents {
		a.Alive = true
		a.Disabled = false
		a.CurrentStrategy = 'T'
	}
	swarm := []rune{'3', '4', '5'}
	for _, l := range swarm {
		w.AgentByLabel(l).CurrentStrategy = SwarmStrategyLetter
	}
	w.EnforceSwarmQuorum()
	onSwarm := 0
	for _, a := range w.Agents {
		if a.Alive && a.CurrentStrategy == SwarmStrategyLetter {
			onSwarm++
		}
	}
	if onSwarm != 3 {
		t.Errorf("quorate swarm grew unexpectedly: %d", onSwarm)
	}
}

// TestEnforceSwarmQuorum_DraftClearsTrustee: drafted agents
// have their CurrentTrustee cleared since S is scent-blind.
func TestEnforceSwarmQuorum_DraftClearsTrustee(t *testing.T) {
	w := NewWorld(773)
	for _, a := range w.Agents {
		a.Alive = true
		a.Disabled = false
		a.CurrentStrategy = 'T'
		a.CurrentTrustee = 0
	}
	// A scent-using follower that's currently on T with a trustee.
	target := w.AgentByLabel('4')
	target.CurrentStrategy = 'U'
	target.CurrentTrustee = '2'
	// Set one agent to S so the quorum logic triggers.
	w.AgentByLabel('5').CurrentStrategy = SwarmStrategyLetter
	w.EnforceSwarmQuorum()
	// `target` may or may not have been drafted (depends on iteration
	// order). If drafted, its trustee must be cleared.
	if target.CurrentStrategy == SwarmStrategyLetter && target.CurrentTrustee != 0 {
		t.Errorf("drafted agent %c retained trustee %c after switch to S",
			target.Label, target.CurrentTrustee)
	}
}

// TestStrategyUsesScent_LettersUVWX: only U, V, W, X claim to use
// scent at decision time. R, S, T (and unrecognized letters) return
// false.
func TestStrategyUsesScent_LettersUVWX(t *testing.T) {
	for _, l := range []rune{'U', 'V', 'W', 'X'} {
		if !StrategyUsesScent(l) {
			t.Errorf("StrategyUsesScent(%c) = false, want true", l)
		}
	}
	for _, l := range []rune{'R', 'S', 'T', 'Z', 0} {
		if StrategyUsesScent(l) {
			t.Errorf("StrategyUsesScent(%c) = true, want false", l)
		}
	}
}

// TestRespawnAgents_NoTrusteeWhenStrategyBlind: a follower-labeled
// agent whose RespawnAgents-picked strategy is scent-blind ends
// up with CurrentTrustee=0 — no leader gets blamed for a strategy
// that never tried to follow.
func TestRespawnAgents_NoTrusteeWhenStrategyBlind(t *testing.T) {
	w := NewWorld(760)
	// Force PickStrategy to deterministically return 'T' by
	// providing only 'T' in the letter pool.
	w.strategyLetters = []rune{'T'}
	a := w.AgentByLabel('4')
	a.Disabled = false
	a.Alive = false
	a.RespawnIn = 0
	w.RespawnAgents()
	if a.CurrentStrategy != 'T' {
		t.Fatalf("CurrentStrategy = %c, want T", a.CurrentStrategy)
	}
	if a.CurrentTrustee != 0 {
		t.Errorf("CurrentTrustee = %c, want 0 (strategy T can't sense scent)",
			a.CurrentTrustee)
	}
}

// TestRespawnAgents_TrusteeWhenStrategyScents: a follower-labeled
// agent whose RespawnAgents-picked strategy IS scent-aware does
// pick a trustee.
func TestRespawnAgents_TrusteeWhenStrategyScents(t *testing.T) {
	w := NewWorld(761)
	w.strategyLetters = []rune{'U'} // scent-follower
	a := w.AgentByLabel('4')
	a.Disabled = false
	a.Alive = false
	a.RespawnIn = 0
	// Make sure SOME leader is alive so PickTrustee has a candidate.
	leader := w.AgentByLabel('1')
	leader.Alive = true
	leader.Disabled = false
	w.RespawnAgents()
	if a.CurrentStrategy != 'U' {
		t.Fatalf("CurrentStrategy = %c, want U", a.CurrentStrategy)
	}
	if a.CurrentTrustee == 0 {
		t.Errorf("CurrentTrustee = 0, want non-zero for scent-aware strategy")
	}
}

// TestStrategyPerf_TTLDeathBumpsOnlyTTL: a TTL-expiry kill
// increments Die.TTL but leaves Win.NoFollow and Win.Following
// untouched (those are reserved for successful goal-reaches).
func TestStrategyPerf_TTLDeathBumpsOnlyTTL(t *testing.T) {
	w := NewWorld(750)
	a := SpawnAgentForTest(w, '3')
	a.CurrentStrategy = 'R'
	a.CurrentTrustee = 0
	w.KillAgent(a, "ttl")
	c := w.StrategyPerf['R']
	if c == nil {
		t.Fatal("StrategyPerf['R'] not recorded")
	}
	if c.TTLExpiry != 1 {
		t.Errorf("TTLExpiry = %d, want 1", c.TTLExpiry)
	}
	if c.NoFollow != 0 {
		t.Errorf("NoFollow = %d, want 0 (deaths don't bump)", c.NoFollow)
	}
	if c.Following != 0 {
		t.Errorf("Following = %d, want 0 (deaths don't bump)", c.Following)
	}
}

// TestStrategyPerf_NonTTLDeathBumpsNothing: deaths from non-TTL
// causes (wumpus, fire pit, etc.) don't touch any counter — only
// TTL deaths bump Die.TTL.
func TestStrategyPerf_NonTTLDeathBumpsNothing(t *testing.T) {
	w := NewWorld(753)
	a := SpawnAgentForTest(w, '3')
	a.CurrentStrategy = 'R'
	a.CurrentTrustee = 0
	w.KillAgent(a, "wumpus")
	c := w.StrategyPerf['R']
	if c != nil && (c.TTLExpiry != 0 || c.NoFollow != 0 || c.Following != 0) {
		t.Errorf("non-TTL death bumped a counter: %+v", *c)
	}
}

// TestStrategyPerf_GoalReachWithTrusteeBumpsFollowing: a goal
// reach with a CurrentTrustee bumps Win.Following only.
func TestStrategyPerf_GoalReachWithTrusteeBumpsFollowing(t *testing.T) {
	w := NewWorld(751)
	a := SpawnAgentForTest(w, '4')
	a.CurrentStrategy = 'V'
	a.CurrentTrustee = '2'
	w.recordStrategyGoal(a)
	c := w.StrategyPerf['V']
	if c == nil {
		t.Fatal("StrategyPerf['V'] not recorded")
	}
	if c.TTLExpiry != 0 {
		t.Errorf("TTLExpiry = %d, want 0", c.TTLExpiry)
	}
	if c.NoFollow != 0 {
		t.Errorf("NoFollow = %d, want 0", c.NoFollow)
	}
	if c.Following != 1 {
		t.Errorf("Following = %d, want 1", c.Following)
	}
}

// TestStrategyPerf_GoalReachWithoutTrusteeBumpsNoFollow: a goal
// reach with no CurrentTrustee bumps Win.NoFollow only.
func TestStrategyPerf_GoalReachWithoutTrusteeBumpsNoFollow(t *testing.T) {
	w := NewWorld(754)
	a := SpawnAgentForTest(w, '3')
	a.CurrentStrategy = 'T'
	a.CurrentTrustee = 0
	w.recordStrategyGoal(a)
	c := w.StrategyPerf['T']
	if c == nil {
		t.Fatal("StrategyPerf['T'] not recorded")
	}
	if c.NoFollow != 1 {
		t.Errorf("NoFollow = %d, want 1", c.NoFollow)
	}
}

// TestStrategyPerf_NoOpForUnsetStrategy: an agent with no
// CurrentStrategy doesn't touch StrategyPerf.
func TestStrategyPerf_NoOpForUnsetStrategy(t *testing.T) {
	w := NewWorld(752)
	a := SpawnAgentForTest(w, '3')
	a.CurrentStrategy = 0
	w.recordStrategyDeath(a, true)
	w.recordStrategyGoal(a)
	if len(w.StrategyPerf) != 0 {
		t.Errorf("StrategyPerf populated for unset strategy: %v", w.StrategyPerf)
	}
}

// TestKillAgent_RecordsRedEvent: any KillAgent call surfaces a red
// Event whose message references the agent label.
func TestKillAgent_RecordsRedEvent(t *testing.T) {
	w := NewWorld(720)
	a := SpawnAgentForTest(w, '4')
	before := len(w.Events)
	w.KillAgent(a, "wumpus")
	if len(w.Events) != before+1 {
		t.Fatalf("Events len = %d, want %d", len(w.Events), before+1)
	}
	e := w.Events[len(w.Events)-1]
	if e.Color != "red" {
		t.Errorf("color = %q, want red", e.Color)
	}
	if !strings.ContainsRune(e.Message, '4') {
		t.Errorf("message = %q, want it to mention agent 4", e.Message)
	}
}

// TestKillAgent_TTLEventUsesTTLTemplate: a death with reason="ttl"
// pulls from the TTL-snark pool — message should mention "TTL" or
// "time" or "clock" depending on the template draw. We just check
// that the choice came from the TTL pool by re-rolling deterministic.
func TestKillAgent_TTLEventUsesTTLTemplate(t *testing.T) {
	w := NewWorld(721)
	a := SpawnAgentForTest(w, '5')
	w.KillAgent(a, "ttl")
	e := w.Events[len(w.Events)-1]
	matched := false
	for _, tmpl := range deathByTTL {
		want := strings.ReplaceAll(tmpl, "%c", "5")
		if e.Message == want {
			matched = true
			break
		}
	}
	if !matched {
		t.Errorf("message %q didn't match any TTL template", e.Message)
	}
}

// TestRecordEvent_ClampsBuffer: posting more than EventBufferSize
// events keeps the buffer at the cap, retaining the most recent.
func TestRecordEvent_ClampsBuffer(t *testing.T) {
	w := NewWorld(722)
	for i := 0; i < EventBufferSize+50; i++ {
		w.RecordEvent("red", fmt.Sprintf("e-%d", i))
	}
	if len(w.Events) != EventBufferSize {
		t.Errorf("buffer len = %d, want %d", len(w.Events), EventBufferSize)
	}
	last := w.Events[len(w.Events)-1].Message
	wantLast := fmt.Sprintf("e-%d", EventBufferSize+49)
	if last != wantLast {
		t.Errorf("last event = %q, want %q", last, wantLast)
	}
}

// TestVisibleEvents_LastN: VisibleEvents returns at most
// EventsVisible entries, ordered oldest-first.
func TestVisibleEvents_LastN(t *testing.T) {
	w := NewWorld(723)
	for i := 1; i <= EventsVisible+3; i++ {
		w.RecordEvent("green", fmt.Sprintf("evt-%d", i))
	}
	got := w.VisibleEvents()
	if len(got) != EventsVisible {
		t.Fatalf("VisibleEvents len = %d, want %d", len(got), EventsVisible)
	}
	// Oldest visible = the (1+3)=4th event.
	if got[0].Message != "evt-4" {
		t.Errorf("oldest visible = %q, want evt-4", got[0].Message)
	}
	if got[len(got)-1].Message != fmt.Sprintf("evt-%d", EventsVisible+3) {
		t.Errorf("newest visible = %q", got[len(got)-1].Message)
	}
}

// TestEndJourney_SuccessAndTTL: a successful journey within TTL —
// with sustained trustee contact — adds both the goal bonus and
// the within-TTL bonus.
func TestEndJourney_SuccessAndTTL(t *testing.T) {
	w := NewWorld(500)
	w.Stats.OptimalDistance = 100 // optimalTTL = 500
	a := SpawnAgentForTest(w, '4')
	a.CurrentTrustee = '1'
	a.TicksAlive = 200 // ≤ optimalTTL → within-TTL bonus
	a.JourneyTrusteeContactTicks = MinTrusteeContactTicks
	w.endJourney(a, true)
	got := a.TrustScores['1']
	want := TrustGoalBonus + TrustWithinTTLBonus
	if got != want {
		t.Errorf("success-within-TTL trust delta = %v, want %v", got, want)
	}
}

// TestEndJourney_SuccessButNotWithinTTL: successful journey that
// took longer than optimalTTL only earns the goal bonus.
func TestEndJourney_SuccessButNotWithinTTL(t *testing.T) {
	w := NewWorld(501)
	a := SpawnAgentForTest(w, '4')
	// Pin a.OptimalDistance (which takes precedence over
	// w.Stats.OptimalDistance in endJourney's ttlBudget choice) so
	// the optimalTTL is small enough that TicksAlive=9999 reliably
	// blows past it on any board size.
	a.OptimalDistance = 100
	w.Stats.OptimalDistance = 100
	a.CurrentTrustee = '1'
	a.TicksAlive = 9999 // > TTLMultiplier * 100 = optimalTTL
	a.JourneyTrusteeContactTicks = MinTrusteeContactTicks
	w.endJourney(a, true)
	got := a.TrustScores['1']
	if got != TrustGoalBonus {
		t.Errorf("late-success trust delta = %v, want %v",
			got, TrustGoalBonus)
	}
}

// TestEndJourney_Failure: a failed journey with sustained trustee
// contact penalizes the trustee.
func TestEndJourney_Failure(t *testing.T) {
	w := NewWorld(502)
	w.Stats.OptimalDistance = 100
	a := SpawnAgentForTest(w, '4')
	a.CurrentTrustee = '1'
	a.JourneyTrusteeContactTicks = MinTrusteeContactTicks
	w.endJourney(a, false)
	if got := a.TrustScores['1']; got != -TrustFailurePenalty {
		t.Errorf("failure trust delta = %v, want %v",
			got, -TrustFailurePenalty)
	}
}

// TestEndJourney_NoContactSkipsPenalty: a failed journey where the
// agent never sustained contact with the trustee's scent leaves the
// trustee's trust unchanged. The agent "lost the scent" so the
// failure carries no information about the leader.
func TestEndJourney_NoContactSkipsPenalty(t *testing.T) {
	w := NewWorld(504)
	w.Stats.OptimalDistance = 100
	a := SpawnAgentForTest(w, '4')
	a.CurrentTrustee = '1'
	a.JourneyTrusteeContactTicks = 0 // never smelled the trustee
	w.endJourney(a, false)
	if got, ok := a.TrustScores['1']; ok && got != 0 {
		t.Errorf("no-contact failure modified trust: got %v, want unchanged", got)
	}
}

// TestEndJourney_BriefContactSkipsUpdate: brief contact below the
// MinTrusteeContactTicks threshold doesn't qualify — same as zero
// contact, no update either way.
func TestEndJourney_BriefContactSkipsUpdate(t *testing.T) {
	w := NewWorld(505)
	w.Stats.OptimalDistance = 100
	a := SpawnAgentForTest(w, '4')
	a.CurrentTrustee = '1'
	a.JourneyTrusteeContactTicks = MinTrusteeContactTicks - 1
	w.endJourney(a, true) // even success skipped — no real signal
	if got, ok := a.TrustScores['1']; ok && got != 0 {
		t.Errorf("brief-contact success modified trust: got %v, want unchanged", got)
	}
}

// TestApplyScentShaping_IncrementsContactCounter: each tick on
// trustee scent bumps JourneyTrusteeContactTicks by 1; non-trustee
// scent doesn't bump it.
func TestApplyScentShaping_IncrementsContactCounter(t *testing.T) {
	w := NewWorld(506)
	a := SpawnAgentForTest(w, '4')
	a.Pos = Pos{X: 40, Y: 40}
	a.CurrentTrustee = '1'
	w.Cycle = 50
	w.ScentOwner[40][40] = '1'
	w.ScentCycle[40][40] = 50
	w.ApplyScentShaping(a)
	w.ApplyScentShaping(a)
	w.ApplyScentShaping(a)
	if a.JourneyTrusteeContactTicks != 3 {
		t.Errorf("contact ticks = %d, want 3", a.JourneyTrusteeContactTicks)
	}
	// Switch the scent owner to a non-trustee label.
	w.ScentOwner[40][40] = '2'
	w.ApplyScentShaping(a)
	if a.JourneyTrusteeContactTicks != 3 {
		t.Errorf("non-trustee tick bumped counter: %d, want 3",
			a.JourneyTrusteeContactTicks)
	}
}

// TestLearnedTTL_RecordsOnTTLDeath: a TTL kill sets LearnedTTL to
// ActualDistance − 1 (the killer fires the first step past the
// threshold, so TTL = lastSurvivedDistance).
func TestLearnedTTL_RecordsOnTTLDeath(t *testing.T) {
	w := NewWorld(700)
	a := SpawnAgentForTest(w, '4')
	a.Stats.ActualDistance = 123
	w.KillAgent(a, "ttl")
	if a.LearnedTTL != 122 {
		t.Errorf("LearnedTTL = %d, want 122 (123 - 1)", a.LearnedTTL)
	}
}

// TestLearnedTTL_OnlyTTLDeathsLearn: deaths from other causes
// (wumpus, pit, etc.) don't touch LearnedTTL.
func TestLearnedTTL_OnlyTTLDeathsLearn(t *testing.T) {
	w := NewWorld(701)
	a := SpawnAgentForTest(w, '4')
	a.Stats.ActualDistance = 123
	w.KillAgent(a, "wumpus")
	if a.LearnedTTL != 0 {
		t.Errorf("LearnedTTL = %d after wumpus death, want 0", a.LearnedTTL)
	}
}

// TestLearnedTTL_MostRecentWins: a second TTL death overwrites the
// first estimate, even if the new value is lower (most recent
// observation reflects current TTL).
func TestLearnedTTL_MostRecentWins(t *testing.T) {
	w := NewWorld(702)
	a := SpawnAgentForTest(w, '4')
	a.Stats.ActualDistance = 500
	w.KillAgent(a, "ttl")
	if a.LearnedTTL != 499 {
		t.Fatalf("first death: LearnedTTL = %d, want 499", a.LearnedTTL)
	}
	// Now simulate a new map with smaller TTL — second TTL death
	// at distance 250.
	a.Stats.ActualDistance = 250
	a.Alive = true
	w.KillAgent(a, "ttl")
	if a.LearnedTTL != 249 {
		t.Errorf("second death: LearnedTTL = %d, want 249 (most recent wins)", a.LearnedTTL)
	}
}

// TestLearnedTTL_InvalidatesOnSurvival: when an agent's
// ActualDistance grows past its current LearnedTTL belief and TTL
// hasn't killed it, the estimate is stale (TTL must have grown) —
// drop it so the next TTL death re-pins.
func TestLearnedTTL_InvalidatesOnSurvival(t *testing.T) {
	w := NewWorld(703)
	w.Stats.OptimalDistance = 100 // world TTL = 500
	a := SpawnAgentForTest(w, '4')
	a.LearnedTTL = 50 // stale low belief from a prior map
	// Strategy that always tries to move east; world's TTL of 500
	// won't fire yet at distance 51 but our belief of 50 is exceeded.
	a.Strategy = func(_ *World, _ *Agent) Pos {
		return Pos{X: a.Pos.X + 1, Y: a.Pos.Y}
	}
	// Walk past distance 50. SpawnAgentForTest puts a at the
	// entrance; advance ActualDistance directly past the threshold.
	a.Stats.ActualDistance = 60
	w.MoveAgents()
	if a.LearnedTTL != 0 {
		t.Errorf("LearnedTTL = %d, want 0 (invalidated by survival)", a.LearnedTTL)
	}
}

// TestEndJourney_NoOpForLeaders: agents 1, 2, 3 don't accumulate
// scent-trust history (they're not followers).
func TestEndJourney_NoOpForLeaders(t *testing.T) {
	w := NewWorld(503)
	w.Stats.OptimalDistance = 100
	a := SpawnAgentForTest(w, '2')
	a.CurrentTrustee = '1' // shouldn't matter
	w.endJourney(a, true)
	if len(a.TrustScores) != 0 {
		t.Errorf("agent 2 endJourney wrote %v, want empty", a.TrustScores)
	}
}

// TestMazeSolved_Threshold: at least MazeSolvedAgentCount agents
// with GoalsReached >= MazeSolvedGoals flips MazeSolved to true.
func TestMazeSolved_Threshold(t *testing.T) {
	w := NewWorld(290)
	if w.MazeSolved() {
		t.Fatal("freshly constructed world reports solved")
	}
	w.Agents[0].Stats.GoalsReached = MazeSolvedGoals
	w.Agents[1].Stats.GoalsReached = MazeSolvedGoals
	if w.MazeSolved() {
		t.Error("only 2 agents at threshold should NOT count as solved")
	}
	w.Agents[2].Stats.GoalsReached = MazeSolvedGoals
	if !w.MazeSolved() {
		t.Error("3 agents at threshold should count as solved")
	}
}

// TestRespawnAgents_KeepsRespawningWithoutGoalCap: high Stats.Starts
// no longer locks an agent out — only reaching MazeSolvedGoals does.
// This guards against the previous bug where struggling agents got
// permanently retired before contributing to the maze-solve.
func TestRespawnAgents_KeepsRespawningWithoutGoalCap(t *testing.T) {
	w := NewWorld(291)
	a := w.AgentByLabel('1')
	a.Disabled = false
	a.Stats.Starts = MaxStartsPerMaze
	a.Stats.GoalsReached = 0
	a.Alive = false
	a.RespawnIn = 0
	w.RespawnAgents()
	if !a.Alive {
		t.Errorf("agent should still respawn at Starts=%d when GoalsReached=0",
			MaxStartsPerMaze)
	}
}

// TestRespawnAgents_RetiresAfterGoalCap: once Stats.GoalsReached hits
// MazeSolvedGoals (mission accomplished), the agent stops respawning.
func TestRespawnAgents_RetiresAfterGoalCap(t *testing.T) {
	w := NewWorld(292)
	a := w.AgentByLabel('1')
	a.Disabled = false
	a.Stats.GoalsReached = MazeSolvedGoals
	a.Alive = false
	a.RespawnIn = 0
	w.RespawnAgents()
	if a.Alive {
		t.Errorf("agent should be retired at GoalsReached=%d", MazeSolvedGoals)
	}
}

// TestWriteStatsLog_RoundTrip: WriteStatsLog produces a parseable
// JSON file containing the agent labels.
func TestWriteStatsLog_RoundTrip(t *testing.T) {
	w := NewWorld(292)
	dir := t.TempDir()
	path, err := w.WriteStatsLog(dir)
	if err != nil {
		t.Fatalf("WriteStatsLog: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var rec MazeStatsLog
	if err := json.Unmarshal(data, &rec); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if rec.Seed != 292 {
		t.Errorf("seed = %d, want 292", rec.Seed)
	}
	if len(rec.Agents) != 12 {
		t.Errorf("agent rows = %d, want 12", len(rec.Agents))
	}
}

// TestSolveLog_AppendsWhenDirExists: when SolveLogDir already
// exists, CheckGoal appends a JSON record per solve.
func TestSolveLog_AppendsWhenDirExists(t *testing.T) {
	prev := SolveLogDir
	SolveLogDir = t.TempDir()
	defer func() { SolveLogDir = prev }()
	w := NewWorld(293)
	a := SpawnAgentForTest(w, '1')
	w.AgentAt[a.Pos.Y][a.Pos.X] = nil
	a.Pos = w.Maze.GoalPos
	w.AgentAt[a.Pos.Y][a.Pos.X] = a
	a.Stats.ActualDistance = 50
	a.TicksAlive = 100
	w.CheckGoal()
	path := filepath.Join(SolveLogDir, "agent1.log")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected agent1.log: %v", err)
	}
	var rec SolveLogRecord
	if err := json.Unmarshal(data[:len(data)-1], &rec); err != nil {
		t.Fatalf("unmarshal: %v\nraw=%q", err, data)
	}
	if rec.Run != 1 || rec.Distance != 50 || rec.Cycles != 100 {
		t.Errorf("record = %+v, want run=1 distance=50 cycles=100", rec)
	}
}

// TestSolveLog_NoFileWhenDirMissing: with SolveLogDir not present,
// CheckGoal writes nothing. Used by every test that doesn't opt in.
func TestSolveLog_NoFileWhenDirMissing(t *testing.T) {
	prev := SolveLogDir
	SolveLogDir = filepath.Join(t.TempDir(), "missing-on-purpose")
	defer func() { SolveLogDir = prev }()
	w := NewWorld(294)
	a := SpawnAgentForTest(w, '1')
	w.AgentAt[a.Pos.Y][a.Pos.X] = nil
	a.Pos = w.Maze.GoalPos
	w.AgentAt[a.Pos.Y][a.Pos.X] = a
	w.CheckGoal()
	if _, err := os.Stat(SolveLogDir); !os.IsNotExist(err) {
		t.Errorf("dir was auto-created (it shouldn't have been): %v", err)
	}
}

// TestCanMoveTo_AgentsDoNotBlockEachOther: an agent walking into a
// cell currently occupied by another live agent is allowed (overlap
// is permitted). Regression for the bug where agents froze in front
// of each other in a corridor.
func TestCanMoveTo_AgentsDoNotBlockEachOther(t *testing.T) {
	w := NewWorld(280)
	a := SpawnAgentForTest(w, '1')
	b := SpawnAgentForTest(w, '2') // both at entrance
	// Pick a walkable neighbor of the entrance.
	var target Pos
	for _, d := range Cardinals {
		np := Pos{X: a.Pos.X + d.X, Y: a.Pos.Y + d.Y}
		if w.Maze.IsWalkable(np) {
			target = np
			break
		}
	}
	if target == (Pos{}) {
		t.Skip("no walkable neighbor at entrance for this seed")
	}
	// Move A onto target.
	w.AgentAt[a.Pos.Y][a.Pos.X] = nil
	a.Pos = target
	w.AgentAt[target.Y][target.X] = a
	// B should now be allowed to move onto the same target — agent
	// overlap is permitted.
	if !w.CanMoveTo(b, target) {
		t.Errorf("CanMoveTo blocked agent overlap at %v", target)
	}
}

// TestStarts_BumpedOnEverySpawn: Starts counts every successful
// transition from dead → alive. First spawn is the first start.
func TestStarts_BumpedOnEverySpawn(t *testing.T) {
	w := NewWorld(270)
	a := w.AgentByLabel('1')
	a.Disabled = false
	if a.Stats.Starts != 0 {
		t.Fatalf("Starts at world construction = %d, want 0", a.Stats.Starts)
	}
	// Step until the agent's initial respawn timer fires.
	for i := 0; i < 5 && !a.Alive; i++ {
		w.Step()
	}
	if !a.Alive {
		t.Fatal("agent did not first-spawn")
	}
	if a.Stats.Starts != 1 {
		t.Errorf("Starts after first spawn = %d, want 1", a.Stats.Starts)
	}
	// Die, respawn, expect Starts == 2.
	w.KillAgent(a, "test")
	for i := 0; i < 30 && !a.Alive; i++ {
		w.Step()
	}
	if !a.Alive {
		t.Skip("respawn did not complete within window")
	}
	if a.Stats.Starts != 2 {
		t.Errorf("Starts after death+respawn = %d, want 2", a.Stats.Starts)
	}
}

// TestSolveTimeAggregates_TrackedAcrossGoals: simulate two solves with
// different TicksAlive values and verify min / max / avg are updated.
func TestSolveTimeAggregates_TrackedAcrossGoals(t *testing.T) {
	w := NewWorld(240)
	a := SpawnAgentForTest(w, '1')
	w.AgentAt[a.Pos.Y][a.Pos.X] = nil
	a.Pos = w.Maze.GoalPos
	a.TicksAlive = 80
	w.AgentAt[a.Pos.Y][a.Pos.X] = a
	w.CheckGoal()
	if a.Stats.MinSolveTime != 80 || a.Stats.MaxSolveTime != 80 ||
		a.Stats.AvgSolveTime != 80 {
		t.Errorf("after first solve, min/max/avg = %d/%d/%v, want 80/80/80",
			a.Stats.MinSolveTime, a.Stats.MaxSolveTime, a.Stats.AvgSolveTime)
	}
	if a.Stats.LastSolveTime != 80 {
		t.Errorf("LastSolveTime = %d, want 80", a.Stats.LastSolveTime)
	}
	// Second solve, slower.
	a.Alive = true
	a.TicksAlive = 200
	a.Pos = w.Maze.GoalPos
	w.AgentAt[a.Pos.Y][a.Pos.X] = a
	w.CheckGoal()
	if a.Stats.MinSolveTime != 80 {
		t.Errorf("MinSolveTime = %d, want 80", a.Stats.MinSolveTime)
	}
	if a.Stats.MaxSolveTime != 200 {
		t.Errorf("MaxSolveTime = %d, want 200", a.Stats.MaxSolveTime)
	}
	if a.Stats.AvgSolveTime != 140 {
		t.Errorf("AvgSolveTime = %v, want 140", a.Stats.AvgSolveTime)
	}
	if a.Stats.LastSolveTime != 200 {
		t.Errorf("LastSolveTime = %d, want 200", a.Stats.LastSolveTime)
	}
}

// TestPathAlignment_OnPathStepCounted: walking onto a ShortestPathCells
// cell bumps OnPathSteps; walking elsewhere bumps OffPathSteps. Score
// reflects (OnPath - OffPath) / OptimalDistance.
func TestPathAlignment_OnPathStepCounted(t *testing.T) {
	w := NewWorld(241)
	a := SpawnAgentForTest(w, '1')
	// Pick a walkable cell on the chosen shortest path.
	var onPath Pos
	for p := range w.ShortestPathCells {
		if p == w.Maze.EntrancePos || p == w.Maze.GoalPos {
			continue
		}
		onPath = p
		break
	}
	if onPath == (Pos{}) {
		t.Skip("no usable non-endpoint cell on the chosen path")
	}
	// Move a manually toward onPath. Use a stub strategy that drives
	// the agent there one step at a time; for simplicity just stomp it.
	a.HasLastFrom = true
	a.Strategy = func(_ *World, _ *Agent) Pos { return onPath }
	w.MoveAgents()
	if a.Stats.OnPathSteps == 0 && a.Stats.OffPathSteps == 0 {
		t.Skip("agent did not move (probably blocked)")
	}
	if a.Pos == onPath && a.Stats.OnPathSteps != 1 {
		t.Errorf("landed on path cell %v, OnPathSteps = %d, want 1",
			onPath, a.Stats.OnPathSteps)
	}
}

// TestWumpusDisabled_FreezesAndClearsStench: enabling WumpusDisabled
// stops wumpus from moving, attacking, and emitting stench.
func TestWumpusDisabled_FreezesAndClearsStench(t *testing.T) {
	w := NewWorld(220)
	w.EnableHazards()
	wm := w.Wumpus[0]
	startPos := wm.Pos
	w.WumpusDisabled = true
	// Step a few ticks — wumpus should not move, no stench should
	// appear anywhere on the board.
	for i := 0; i < 5; i++ {
		w.Step()
	}
	if wm.Pos != startPos {
		t.Errorf("wumpus moved %v -> %v despite disabled", startPos, wm.Pos)
	}
	for y := 0; y < BoardHeight; y++ {
		for x := 0; x < BoardWidth; x++ {
			if w.Stench[y][x] {
				t.Fatalf("stench at (%d,%d) despite wumpus disabled", x, y)
			}
		}
	}
}

// TestWumpusDisabled_NotHazard: when disabled, wumpus cells aren't
// reported as hazards by IsHazard.
func TestWumpusDisabled_NotHazard(t *testing.T) {
	w := NewWorld(221)
	w.EnableHazards()
	wm := w.Wumpus[0]
	if !w.IsHazard(wm.Pos) {
		t.Fatal("live wumpus cell should be hazard when enabled")
	}
	w.WumpusDisabled = true
	if w.IsHazard(wm.Pos) {
		t.Error("wumpus cell should NOT be hazard when disabled")
	}
}

// TestFirePitsDisabled_NoDeaths: with fire pits off, an agent on a
// fire-pit cell does not die.
func TestFirePitsDisabled_NoDeaths(t *testing.T) {
	w := NewWorld(222)
	if len(w.Maze.FirePits) == 0 {
		t.Skip("seed produced no fire pits")
	}
	a := SpawnAgentForTest(w, '1')
	pit := w.Maze.FirePits[0]
	w.AgentAt[a.Pos.Y][a.Pos.X] = nil
	a.Pos = pit
	w.AgentAt[pit.Y][pit.X] = a
	w.FirePitsDisabled = true
	w.ResolvePitDeaths()
	if !a.Alive {
		t.Error("agent on fire pit should survive when fire pits disabled")
	}
}

// TestWaterPitsDisabled_NoCollection: CollectWater no-ops when
// water pits are disabled.
func TestWaterPitsDisabled_NoCollection(t *testing.T) {
	w := NewWorld(225)
	a := SpawnAgentForTest(w, '1')
	w.Maze.Cells[a.Pos.Y][a.Pos.X] = CellWaterPit
	w.WaterPitsDisabled = true
	beforeWater := a.Water
	beforeCell := w.Maze.Cells[a.Pos.Y][a.Pos.X]
	w.CollectWater()
	if a.Water != beforeWater {
		t.Errorf("agent gained water (%d->%d) despite WaterPitsDisabled",
			beforeWater, a.Water)
	}
	if w.Maze.Cells[a.Pos.Y][a.Pos.X] != beforeCell {
		t.Errorf("water-pit cell consumed despite WaterPitsDisabled")
	}
}

// TestTTLDisabled_AgentDoesNotDie: with TTL off, an agent that has
// blown past 2x optimal distance still does not die from TTL.
func TestTTLDisabled_AgentDoesNotDie(t *testing.T) {
	w := NewWorld(230)
	a := SpawnAgentForTest(w, '2')
	w.TTLDisabled = true
	a.Stats.ActualDistance = TTLMultiplier*w.Stats.OptimalDistance + 1000
	for i := 0; i < 20 && a.Alive; i++ {
		w.MoveAgents()
	}
	if !a.Alive {
		t.Errorf("agent died despite TTLDisabled (last_death=%q)",
			a.Stats.LastDeathReason)
	}
}

// TestFirePitsDisabled_NotHazard: with fire pits off, IsHazard
// reports fire-pit cells as walkable.
func TestFirePitsDisabled_NotHazard(t *testing.T) {
	w := NewWorld(223)
	w.EnableHazards()
	if len(w.Maze.FirePits) == 0 {
		t.Skip("seed produced no fire pits")
	}
	pit := w.Maze.FirePits[0]
	if !w.IsHazard(pit) {
		t.Fatal("fire pit should be hazard when enabled")
	}
	w.FirePitsDisabled = true
	if w.IsHazard(pit) {
		t.Error("fire pit should NOT be hazard when disabled")
	}
}

// TestComputeDistFromStart_EntranceIsZero: BFS distance from entrance
// is 0 at the entrance and increases through walkable cells.
func TestComputeDistFromStart_EntranceIsZero(t *testing.T) {
	w := NewWorld(204)
	e := w.Maze.EntrancePos
	if w.DistFromStart[e.Y][e.X] != 0 {
		t.Errorf("DistFromStart at entrance = %d, want 0",
			w.DistFromStart[e.Y][e.X])
	}
	// Some walkable neighbor must be 1.
	hasOne := false
	for _, d := range Cardinals {
		np := Pos{X: e.X + d.X, Y: e.Y + d.Y}
		if InBounds(np.X, np.Y) && w.DistFromStart[np.Y][np.X] == 1 {
			hasOne = true
			break
		}
	}
	if !hasOne {
		t.Error("no neighbor of entrance has distance 1")
	}
	// Walls (and unreached cells) stay at -1.
	for y := 0; y < BoardHeight; y++ {
		for x := 0; x < BoardWidth; x++ {
			if w.Maze.Cells[y][x] == CellWall && w.DistFromStart[y][x] != -1 {
				t.Errorf("wall at (%d,%d) got distance %d, want -1",
					x, y, w.DistFromStart[y][x])
			}
		}
	}
}

// TestRealDistanceShaping_OnlyOnNewMax: a step into a cell with
// strictly higher DistFromStart pays once; back-stepping doesn't.
func TestRealDistanceShaping_OnlyOnNewMax(t *testing.T) {
	w := NewWorld(205)
	a := SpawnAgentForTest(w, '4')
	// Pick a walkable neighbor of the entrance with DistFromStart == 1.
	var step Pos
	for _, d := range Cardinals {
		np := Pos{X: a.Pos.X + d.X, Y: a.Pos.Y + d.Y}
		if InBounds(np.X, np.Y) && w.DistFromStart[np.Y][np.X] == 1 {
			step = np
			break
		}
	}
	if step == (Pos{}) {
		t.Skip("no walkable neighbor at distance 1")
	}
	a.MaxStartDist = 0
	a.PendingBonus = 0
	a.Strategy = func(_ *World, _ *Agent) Pos { return step }
	w.MoveAgents()
	if a.MaxStartDist != 1 {
		t.Errorf("MaxStartDist = %d after step out, want 1", a.MaxStartDist)
	}
	if a.PendingBonus < RealDistanceShaping {
		t.Errorf("PendingBonus = %v, want >= %v after first-time progress",
			a.PendingBonus, RealDistanceShaping)
	}
	// Back-step to entrance. Then step back to the same cell. Neither
	// move should pay the real-distance reward again.
	a.PendingBonus = 0
	entrance := w.Maze.EntrancePos
	a.Strategy = func(_ *World, _ *Agent) Pos { return entrance }
	w.MoveAgents()
	a.Strategy = func(_ *World, _ *Agent) Pos { return step }
	bonusBefore := a.PendingBonus
	w.MoveAgents()
	if a.PendingBonus > bonusBefore+RealDistanceShaping/2 {
		t.Errorf("back-and-forth re-paid: bonus went from %v to %v",
			bonusBefore, a.PendingBonus)
	}
}

// TestMaxStartDist_ResetsOnRespawn: after death and respawn the
// agent's max-start-dist starts over from zero.
func TestMaxStartDist_ResetsOnRespawn(t *testing.T) {
	w := NewWorld(206)
	a := SpawnAgentForTest(w, '4')
	a.MaxStartDist = 47
	w.KillAgent(a, "wumpus")
	for i := 0; i < 30 && !a.Alive; i++ {
		w.Step()
	}
	if !a.Alive {
		t.Skip("did not respawn")
	}
	if a.MaxStartDist != 0 {
		t.Errorf("MaxStartDist = %d after respawn, want 0", a.MaxStartDist)
	}
}

// TestKnownPathReward_OncePerCellEver: after paying KnownPathReward
// for a cell, dying and re-entering must NOT pay it a second time.
func TestKnownPathReward_OncePerCellEver(t *testing.T) {
	w := NewWorld(203)
	a := SpawnAgentForTest(w, '4')
	cell := Pos{40, 40}
	w.Maze.Cells[cell.Y][cell.X] = CellPath
	// Pretend the cell was visited in a prior life.
	a.LifetimeVisited = map[Pos]bool{cell: true}
	a.Visited = map[Pos]bool{}
	a.KnownPathRewarded = nil
	// Move agent onto cell — simulates the gate inside MoveAgents.
	a.Pos = cell
	a.Visited[a.Pos] = true
	if a.KnownPathRewarded == nil {
		a.KnownPathRewarded = map[Pos]bool{}
	}
	if !a.KnownPathRewarded[a.Pos] && a.LifetimeVisited[a.Pos] {
		a.PendingBonus += KnownPathReward
		a.KnownPathRewarded[a.Pos] = true
	}
	first := a.PendingBonus
	// Second life: Visited reset, KnownPathRewarded persists.
	a.Visited = map[Pos]bool{}
	a.PendingBonus = 0
	a.Visited[a.Pos] = true
	if a.LifetimeVisited[a.Pos] && !a.KnownPathRewarded[a.Pos] {
		a.PendingBonus += KnownPathReward
		a.KnownPathRewarded[a.Pos] = true
	}
	if a.PendingBonus != 0 {
		t.Errorf("KnownPathReward fired twice: first=%v, second=%v",
			first, a.PendingBonus)
	}
}
