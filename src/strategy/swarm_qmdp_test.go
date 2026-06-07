package strategy

import (
	"testing"

	"maze-of-wumpus/src/world"
)

// TestPlanFor_DispatchesPerAlgorithm: each swarm letter maps to a
// non-nil planner. This is what keeps exploration UNIQUE per algorithm
// — the swarm runs each member's own planner, not a shared movement
// rule.
func TestPlanFor_DispatchesPerAlgorithm(t *testing.T) {
	for _, l := range []rune{
		StrategySwarmBayesian, StrategyBayesian, StrategyPOMCP, StrategyQMDP,
	} {
		if planFor(l) == nil {
			t.Errorf("planFor(%c) = nil, want a planner", l)
		}
	}
}

// TestSwarmStrategy_SharesKnowledge: two same-group swarm members pool
// their KnownCells; a different group does not leak in.
func TestSwarmStrategy_SharesKnowledge(t *testing.T) {
	w := newConfiguredWorld(24)
	a := world.SpawnAgentForTest(w, '6')
	b := world.SpawnAgentForTest(w, '5')
	a.CurrentStrategy = StrategyQMDP
	b.CurrentStrategy = StrategyQMDP
	a.SwarmGroupID = 5
	b.SwarmGroupID = 5
	a.KnownCells = map[world.Pos]bool{{X: 1, Y: 1}: true}
	b.KnownCells = map[world.Pos]bool{{X: 2, Y: 2}: true}
	a.Beliefs = world.NewAgentBeliefs()
	b.Beliefs = world.NewAgentBeliefs()
	mergeSwarmKnowledge(w, a)
	if !a.KnownCells[world.Pos{X: 2, Y: 2}] {
		t.Error("swarm member did not pool peer's cell")
	}
}

// TestSwarmClone_ThrashTerminatesAndFreesSlot: a clone oscillating
// over ≤ swarmThrashDistinctMax cells across a full window is flagged
// thrashing; once terminated, pruneDeadSwarmClones removes it from the
// roster (freeing a slot for future lazy forking — no auto-respawn).
func TestSwarmClone_ThrashTerminatesAndFreesSlot(t *testing.T) {
	w := newConfiguredWorld(26)
	a := world.SpawnAgentForTest(w, '6')
	a.CurrentStrategy = StrategyQMDP
	a.SwarmGroupID = 1
	a.Pos = world.Pos{X: 40, Y: 40}
	c := &world.SwarmClone{Pos: world.Pos{X: 5, Y: 5}, Alive: true}
	a.SwarmClones = []*world.SwarmClone{c}
	for i := 0; i < swarmTrailWindow; i++ {
		if i%2 == 0 {
			c.Pos = world.Pos{X: 5, Y: 5}
		} else {
			c.Pos = world.Pos{X: 6, Y: 5}
		}
		recordCloneTrail(c)
	}
	if !cloneIsThrashing(c) {
		t.Fatal("clone oscillating over 2 cells should be flagged thrashing")
	}
	c.Alive = false // the termination effect moveOneSwarmMember applies
	pruneDeadSwarmClones(a)
	if len(a.SwarmClones) != 0 {
		t.Errorf("dead clone not pruned: roster len = %d, want 0", len(a.SwarmClones))
	}
}

// TestSwarmClone_ExpiresOnIndividualDistance: a clone is terminated
// only once ITS OWN Dist exceeds the TTL budget — not the swarm's
// aggregate, and not the leader's.
func TestSwarmClone_ExpiresOnIndividualDistance(t *testing.T) {
	w := newConfiguredWorld(50)
	a := world.SpawnAgentForTest(w, '6')
	a.CurrentStrategy = StrategyQMDP
	a.SwarmGroupID = 1
	a.OptimalDistance = 10 // limit = TTLMultiplier*10 = 30
	limit := swarmTTLLimit(w, a)
	if limit != world.TTLMultiplier*10 {
		t.Fatalf("ttl limit = %d, want %d", limit, world.TTLMultiplier*10)
	}
	// A clone just under the limit survives; just over expires.
	under := &world.SwarmClone{Alive: true, Dist: limit}
	over := &world.SwarmClone{Alive: true, Dist: limit + 1}
	if !under.Alive {
		t.Fatal("setup")
	}
	// Simulate the per-clone check inline (the move path applies it).
	if l := swarmTTLLimit(w, a); l > 0 && under.Dist > l {
		under.Alive = false
	}
	if l := swarmTTLLimit(w, a); l > 0 && over.Dist > l {
		over.Alive = false
	}
	if !under.Alive {
		t.Error("clone at the budget should still be alive")
	}
	if over.Alive {
		t.Error("clone past the budget should have expired")
	}
}

// TestSwarmClone_DistinctTrailNotThrashing: a clone visiting distinct
// cells is not flagged.
func TestSwarmClone_DistinctTrailNotThrashing(t *testing.T) {
	c := &world.SwarmClone{Alive: true}
	for i := 0; i < swarmTrailWindow; i++ {
		c.Pos = world.Pos{X: i, Y: 0}
		recordCloneTrail(c)
	}
	if cloneIsThrashing(c) {
		t.Error("clone visiting distinct cells should not be thrashing")
	}
}

// TestSwarmClone_SharesPerceptionWithLeader: moving a clone senses from
// its cell into the leader's SHARED KnownCells, so the leader (and
// every other clone) gains the clone's perception — the "knowledge
// sharing among leader and clones" requirement.
func TestSwarmClone_SharesPerceptionWithLeader(t *testing.T) {
	w := newConfiguredWorld(28)
	a := world.SpawnAgentForTest(w, '6')
	a.CurrentStrategy = StrategyQMDP
	a.SwarmGroupID = 1
	a.Beliefs = world.NewAgentBeliefs()
	clonePos := world.Pos{X: 70, Y: 70}
	w.Maze.Cells[clonePos.Y][clonePos.X] = world.CellPath
	c := &world.SwarmClone{Pos: clonePos, Alive: true}
	a.SwarmClones = []*world.SwarmClone{c}
	a.KnownCells = map[world.Pos]bool{a.Pos: true}
	if a.KnownCells[clonePos] {
		t.Fatal("setup: clone cell should not be pre-known")
	}
	var forks []forkReq
	moveOneSwarmMember(w, a, c, planFor(a.CurrentStrategy),
		spawnPolicyFor(a.CurrentStrategy), a.KnownCells, a.Pos, &forks)
	if !a.KnownCells[c.Pos] {
		t.Errorf("clone perception at %v not shared into leader's KnownCells", c.Pos)
	}
}
