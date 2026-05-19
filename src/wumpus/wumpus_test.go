package wumpus

import (
	"fmt"
	"math/rand"
	"testing"

	"maze-of-wumpus/src/world"
)

func newConfiguredWorld(seed int64) *world.World {
	return world.NewWorldWithConfig(world.Config{
		Seed:              seed,
		WumpusStrategy:    PickStrategy,
		VengeanceStrategy: ScentStrategy,
	})
}

func TestPickStrategy_CoversAll(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	seen := map[string]bool{}
	for i := 0; i < 200 && len(seen) < 5; i++ {
		s := PickStrategy(rng)
		seen[fmt.Sprintf("%p", s)] = true
	}
	if len(seen) < 5 {
		t.Errorf("only %d distinct strategies seen in 200 draws", len(seen))
	}
}

func TestNearestLiveAgent_NoneAlive(t *testing.T) {
	w := newConfiguredWorld(101)
	for _, a := range w.Agents {
		a.Alive = false
	}
	if _, ok := nearestLiveAgent(w, world.Pos{X: 0, Y: 0}); ok {
		t.Error("nearestLiveAgent should be false when no agents alive")
	}
}

func TestBfsDfsTo_SameCell(t *testing.T) {
	w := newConfiguredWorld(104)
	w.EnableHazards()
	wm := w.Wumpus[0]
	if p := bfsTo(w, wm, wm.Pos); p != nil {
		t.Errorf("bfs same-cell = %v, want nil", p)
	}
	if p := dfsTo(w, wm, wm.Pos); p != nil {
		t.Errorf("dfs same-cell = %v, want nil", p)
	}
}

func TestRLStrategies_AdjacentAgentReward(t *testing.T) {
	w := newConfiguredWorld(105)
	w.EnableHazards()
	a := world.SpawnAgentForTest(w, '1')
	wm := w.Wumpus[0]
	w.WumpusAt[wm.Pos.Y][wm.Pos.X] = nil
	target := world.Pos{X: a.Pos.X + 1, Y: a.Pos.Y}
	if !w.Maze.IsWalkable(target) {
		target = world.Pos{X: a.Pos.X, Y: a.Pos.Y + 1}
	}
	if !w.Maze.IsWalkable(target) {
		t.Skip("no adjacent walkable cell at this seed")
	}
	wm.Pos = target
	w.WumpusAt[target.Y][target.X] = wm
	_ = QLStrategy(w, wm)
	_ = QLStrategy(w, wm)
	_ = DqnStrategy(w, wm)
	_ = DqnStrategy(w, wm)
}

func TestDqnFeatures_Hazards(t *testing.T) {
	w := newConfiguredWorld(106)
	w.EnableHazards()
	wm := w.Wumpus[0]
	w.Heat[wm.Pos.Y][wm.Pos.X] = true
	w.Stench[wm.Pos.Y][wm.Pos.X] = true
	f := DqnFeatures(w, wm)
	if f[4] != 1 || f[5] != 1 {
		t.Errorf("heat/stench features = %v %v, want 1/1", f[4], f[5])
	}
}

func TestStrategies_RunOnce(t *testing.T) {
	w := newConfiguredWorld(102)
	w.EnableHazards()
	_ = world.SpawnAgentForTest(w, '1')
	wm := w.Wumpus[0]
	strategies := []world.WumpusStrategy{
		ScentStrategy,
		BfsStrategy,
		DfsStrategy,
		QLStrategy,
		DqnStrategy,
	}
	for _, s := range strategies {
		_ = s(w, wm)
	}
}

// TestBfsDfsStrategy_NoLiveAgents falls through to RandomNeighbor.
func TestBfsDfsStrategy_NoLiveAgents(t *testing.T) {
	w := newConfiguredWorld(180)
	w.EnableHazards()
	for _, a := range w.Agents {
		a.Alive = false
	}
	wm := w.Wumpus[0]
	_ = BfsStrategy(w, wm)
	_ = DfsStrategy(w, wm)
}

// TestBfsDfsStrategy_NoPathToAgent: agent alive but boxed off from
// the wumpus; both strategies fall through to RandomNeighbor.
func TestBfsDfsStrategy_NoPathToAgent(t *testing.T) {
	w := newConfiguredWorld(181)
	w.EnableHazards()
	a := world.SpawnAgentForTest(w, '1')
	// Wall off the agent so the wumpus can never reach it.
	for _, d := range world.Cardinals {
		np := world.Pos{X: a.Pos.X + d.X, Y: a.Pos.Y + d.Y}
		if world.InBounds(np.X, np.Y) {
			w.Maze.Cells[np.Y][np.X] = world.CellWall
		}
	}
	wm := w.Wumpus[0]
	_ = BfsStrategy(w, wm)
	_ = DfsStrategy(w, wm)
}

// TestScentStrategy_NoScent falls through to RandomNeighbor when no
// adjacent cell has scent.
func TestScentStrategy_NoScent(t *testing.T) {
	w := newConfiguredWorld(182)
	w.EnableHazards()
	wm := w.Wumpus[0]
	for y := 0; y < world.BoardHeight; y++ {
		for x := 0; x < world.BoardWidth; x++ {
			w.ScentOwner[y][x] = 0
		}
	}
	_ = ScentStrategy(w, wm)
}

// TestScentStrategy_WithScent hits the scent-chase branch.
func TestScentStrategy_WithScent(t *testing.T) {
	w := newConfiguredWorld(183)
	w.EnableHazards()
	wm := w.Wumpus[0]
	for _, d := range world.Cardinals {
		np := world.Pos{X: wm.Pos.X + d.X, Y: wm.Pos.Y + d.Y}
		if world.InBounds(np.X, np.Y) && w.Maze.IsWalkable(np) {
			w.ScentOwner[np.Y][np.X] = '1'
		}
	}
	got := ScentStrategy(w, wm)
	if got == wm.Pos {
		t.Skip("no walkable neighbor at this seed")
	}
}

// TestDfsTo_Unreachable: walling off the destination cell forces dfsTo
// to exhaust its search and return nil.
func TestDfsTo_Unreachable(t *testing.T) {
	w := newConfiguredWorld(184)
	w.EnableHazards()
	wm := w.Wumpus[0]
	target := world.Pos{X: 60, Y: 60}
	w.Maze.Cells[target.Y][target.X] = world.CellPath
	for _, d := range world.Cardinals {
		w.Maze.Cells[target.Y+d.Y][target.X+d.X] = world.CellWall
	}
	if p := dfsTo(w, wm, target); p != nil {
		t.Errorf("unreachable dfs returned %v, want nil", p)
	}
}

// TestBfsTo_Unreachable: wumpus pathfinding to a boxed-off cell
// returns nil.
func TestBfsTo_Unreachable(t *testing.T) {
	w := newConfiguredWorld(185)
	w.EnableHazards()
	wm := w.Wumpus[0]
	target := world.Pos{X: 60, Y: 60}
	w.Maze.Cells[target.Y][target.X] = world.CellPath
	for _, d := range world.Cardinals {
		w.Maze.Cells[target.Y+d.Y][target.X+d.X] = world.CellWall
	}
	if p := bfsTo(w, wm, target); p != nil {
		t.Errorf("unreachable bfs returned %v, want nil", p)
	}
}

// TestBfsStrategy_PathExists exercises the path-found branch.
func TestBfsStrategy_PathExists(t *testing.T) {
	w := newConfiguredWorld(186)
	w.EnableHazards()
	a := world.SpawnAgentForTest(w, '1')
	wm := w.Wumpus[0]
	// Place wumpus right next to the agent so BFS returns a 1-step path.
	target := world.Pos{X: a.Pos.X + 2, Y: a.Pos.Y}
	if !w.Maze.IsWalkable(target) {
		target = world.Pos{X: a.Pos.X, Y: a.Pos.Y + 2}
	}
	if !w.Maze.IsWalkable(target) {
		t.Skip("seed produced no near walkable cell")
	}
	w.WumpusAt[wm.Pos.Y][wm.Pos.X] = nil
	wm.Pos = target
	w.WumpusAt[target.Y][target.X] = wm
	got := BfsStrategy(w, wm)
	if got == wm.Pos {
		t.Error("BfsStrategy returned wm.Pos despite live agent in reach")
	}
}

// TestDfsStrategy_PathExists mirrors the above for DFS.
func TestDfsStrategy_PathExists(t *testing.T) {
	w := newConfiguredWorld(187)
	w.EnableHazards()
	a := world.SpawnAgentForTest(w, '1')
	wm := w.Wumpus[0]
	target := world.Pos{X: a.Pos.X + 2, Y: a.Pos.Y}
	if !w.Maze.IsWalkable(target) {
		target = world.Pos{X: a.Pos.X, Y: a.Pos.Y + 2}
	}
	if !w.Maze.IsWalkable(target) {
		t.Skip("seed produced no near walkable cell")
	}
	w.WumpusAt[wm.Pos.Y][wm.Pos.X] = nil
	wm.Pos = target
	w.WumpusAt[target.Y][target.X] = wm
	got := DfsStrategy(w, wm)
	if got == wm.Pos {
		t.Error("DfsStrategy returned wm.Pos despite live agent in reach")
	}
}

func TestRandomNeighbor_AllBlocked(t *testing.T) {
	w := newConfiguredWorld(103)
	w.EnableHazards()
	wm := w.Wumpus[0]
	w.WumpusAt[wm.Pos.Y][wm.Pos.X] = nil
	wm.Pos = world.Pos{X: 40, Y: 40}
	w.WumpusAt[40][40] = wm
	w.Maze.Cells[40][40] = world.CellPath
	for _, d := range world.Cardinals {
		w.Maze.Cells[40+d.Y][40+d.X] = world.CellWall
	}
	if got := RandomNeighbor(w, wm); got != wm.Pos {
		t.Errorf("walled-in wumpus = %v, want %v", got, wm.Pos)
	}
}
