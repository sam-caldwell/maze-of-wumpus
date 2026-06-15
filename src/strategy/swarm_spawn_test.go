package strategy

import (
	"testing"

	"maze-of-wumpus/src/world"
)

// TestUntakenUnexploredBranches: at a 4-way junction, the untaken arms
// that open onto unperceived territory are returned; the taken arm and
// any fully-explored direction are excluded.
func TestUntakenUnexploredBranches(t *testing.T) {
	w := newConfiguredWorld(30)
	a := world.SpawnAgentForTest(w, '4')
	a.Pos = world.Pos{X: 50, Y: 50}
	full := map[world.Pos]bool{}
	for _, p := range []world.Pos{{X: 50, Y: 50}, {X: 50, Y: 49}, {X: 50, Y: 51}, {X: 49, Y: 50}, {X: 51, Y: 50}} {
		w.Maze.Cells[p.Y][p.X] = world.CellPath
		full[p] = true
	}
	// Arm-ends are walkable but UNperceived, so each arm opens to
	// unexplored space.
	for _, p := range []world.Pos{{X: 50, Y: 48}, {X: 50, Y: 52}, {X: 48, Y: 50}, {X: 52, Y: 50}} {
		w.Maze.Cells[p.Y][p.X] = world.CellPath
	}
	a.KnownCells = full
	taken := world.Pos{X: 51, Y: 50} // moving east
	branches := untakenUnexploredBranches(w, a, full, taken)
	if len(branches) != 3 {
		t.Errorf("branches = %d, want 3 (N/S/W arms, minus taken E)", len(branches))
	}
	for _, b := range branches {
		if b == taken {
			t.Error("taken branch should be excluded")
		}
	}
}

// TestApplyForks_RespectsCap: forking more cells than the cap yields
// exactly SwarmClonesPerLeader clones.
func TestApplyForks_RespectsCap(t *testing.T) {
	w := newConfiguredWorld(33)
	a := &world.Agent{}
	forks := make([]forkReq, world.SwarmClonesPerLeader+5)
	for i := range forks {
		forks[i] = forkReq{at: world.Pos{X: i, Y: 0}}
	}
	applyForks(w, a, forks)
	if len(a.SwarmClones) != world.SwarmClonesPerLeader {
		t.Errorf("clones = %d, want cap %d", len(a.SwarmClones), world.SwarmClonesPerLeader)
	}
}

// TestSwarmStrategy_SoloLeaderForksAtJunction: a solo leader at a
// junction with multiple unexplored branches forks at least one clone
// (lazy spawn). QMDP forks branches of comparable utility.
func TestSwarmStrategy_SoloLeaderForksAtJunction(t *testing.T) {
	w := newConfiguredWorld(33)
	a := world.SpawnAgentForTest(w, '4')
	a.CurrentStrategy = StrategyQMDP
	a.SwarmGroupID = 1
	a.Beliefs = world.NewAgentBeliefs()
	a.Pos = world.Pos{X: 60, Y: 60}
	full := map[world.Pos]bool{}
	for _, p := range []world.Pos{{X: 60, Y: 60}, {X: 60, Y: 59}, {X: 60, Y: 61}, {X: 59, Y: 60}, {X: 61, Y: 60}} {
		w.Maze.Cells[p.Y][p.X] = world.CellPath
		full[p] = true
	}
	for _, p := range []world.Pos{{X: 60, Y: 58}, {X: 60, Y: 62}, {X: 58, Y: 60}, {X: 62, Y: 60}} {
		w.Maze.Cells[p.Y][p.X] = world.CellPath
	}
	a.KnownCells = full
	if len(a.SwarmClones) != 0 {
		t.Fatal("leader should start solo (0 clones)")
	}
	SwarmStrategy(w, a)
	if len(a.SwarmClones) == 0 {
		t.Error("expected the solo leader to fork ≥1 clone at the junction")
	}
	if len(a.SwarmClones) > world.SwarmClonesPerLeader {
		t.Errorf("forked %d clones, exceeds cap %d", len(a.SwarmClones), world.SwarmClonesPerLeader)
	}
}

// TestFrontierSectorReps_OpenRoomMultipleDirections: in an open room
// the perceived frontier ring lands in multiple directional sectors,
// so the swarm has several distinct regions to fork toward.
func TestFrontierSectorReps_OpenRoomMultipleDirections(t *testing.T) {
	w := newConfiguredWorld(40)
	from := world.Pos{X: 200, Y: 200}
	// Carve + perceive a solid 7x7 room; leave the surrounding ring
	// unperceived so the room's edge cells are frontier in every
	// direction.
	known := map[world.Pos]bool{}
	for y := from.Y - 3; y <= from.Y+3; y++ {
		for x := from.X - 3; x <= from.X+3; x++ {
			w.Maze.Cells[y][x] = world.CellPath
			known[world.Pos{X: x, Y: y}] = true
		}
	}
	reps := frontierSectorReps(w, known, from)
	if len(reps) < 4 {
		t.Errorf("open room should yield frontier in many sectors, got %d", len(reps))
	}
}

// TestSwarmRegionForks_SaturatesOpenSpace: a solo leader in an open
// room forks clones toward multiple distinct frontier directions when
// slots are free (open-space saturation), bounded by the cap.
func TestSwarmRegionForks_SaturatesOpenSpace(t *testing.T) {
	w := newConfiguredWorld(41)
	a := world.SpawnAgentForTest(w, '4')
	a.CurrentStrategy = StrategyQMDP
	a.SwarmGroupID = 1
	a.Beliefs = world.NewAgentBeliefs()
	a.Pos = world.Pos{X: 300, Y: 300}
	known := map[world.Pos]bool{}
	for y := a.Pos.Y - 3; y <= a.Pos.Y+3; y++ {
		for x := a.Pos.X - 3; x <= a.Pos.X+3; x++ {
			w.Maze.Cells[y][x] = world.CellPath
			known[world.Pos{X: x, Y: y}] = true
		}
	}
	a.KnownCells = known
	var forks []forkReq
	swarmRegionForks(w, a, known, a.Pos, &forks)
	if len(forks) < 2 {
		t.Errorf("open room should fork toward multiple directions, got %d forks", len(forks))
	}
	if len(forks) > world.SwarmClonesPerLeader {
		t.Errorf("forks %d exceed cap %d", len(forks), world.SwarmClonesPerLeader)
	}
	// Region forks carry a seed path so the clone heads to its region.
	for _, f := range forks {
		if len(f.seed) < 2 {
			t.Errorf("region fork missing seed path: %+v", f)
		}
	}
}

// TestSwarmDispersionPenalty_GatedAndRepels: zero with no peers and
// once the goal is perceived; otherwise higher closer to peers.
func TestSwarmDispersionPenalty_GatedAndRepels(t *testing.T) {
	w := newConfiguredWorld(31)
	a := world.SpawnAgentForTest(w, '4')
	a.KnownCells = map[world.Pos]bool{}
	if swarmDispersionPenalty(w, a, world.Pos{X: 10, Y: 10}) != 0 {
		t.Error("no peers → penalty should be 0")
	}
	a.SwarmPeers = []world.Pos{{X: 10, Y: 10}}
	near := swarmDispersionPenalty(w, a, world.Pos{X: 10, Y: 10})
	far := swarmDispersionPenalty(w, a, world.Pos{X: 100, Y: 100})
	if near <= far {
		t.Errorf("penalty should be higher near peers: near=%v far=%v", near, far)
	}
	a.KnownCells[w.Maze.GoalPos] = true
	if swarmDispersionPenalty(w, a, world.Pos{X: 10, Y: 10}) != 0 {
		t.Error("goal perceived → repulsion should switch off (convergence)")
	}
}

// TestSeedGoalConvergencePath: once the goal is perceived, a member
// gets a known-graph path to it that its planner can replay.
func TestSeedGoalConvergencePath(t *testing.T) {
	w := newConfiguredWorld(32)
	a := world.SpawnAgentForTest(w, '4')
	goal := w.Maze.GoalPos
	a.Pos = world.Pos{X: goal.X - 1, Y: goal.Y}
	w.Maze.Cells[a.Pos.Y][a.Pos.X] = world.CellPath
	a.KnownCells = map[world.Pos]bool{a.Pos: true, goal: true}
	a.KnownShortestPath = nil
	seedGoalConvergencePath(w, a)
	if len(a.KnownShortestPath) < 2 {
		t.Fatalf("convergence path not seeded: %v", a.KnownShortestPath)
	}
	if a.KnownShortestPath[len(a.KnownShortestPath)-1] != goal {
		t.Errorf("convergence path does not end at goal: %v", a.KnownShortestPath)
	}
}
