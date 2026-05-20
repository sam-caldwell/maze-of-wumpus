package world

import (
	"math/rand"
	"testing"
)

func TestQLearning_GetSetMaxArgMax(t *testing.T) {
	q := NewQLearning()
	s := Pos{1, 2}
	if got := q.MaxQ(s); got != 0 {
		t.Errorf("MaxQ unseen = %v, want 0", got)
	}
	if got := q.ArgMaxQ(s); got != -1 {
		t.Errorf("ArgMaxQ unseen = %v, want -1", got)
	}
	q.SetQ(s, 0, 1.0)
	q.SetQ(s, 1, 5.0)
	q.SetQ(s, 2, 3.0)
	q.SetQ(s, 3, 2.0)
	if got := q.GetQ(s, 1); got != 5.0 {
		t.Errorf("GetQ(s,1) = %v, want 5.0", got)
	}
	if got := q.MaxQ(s); got != 5.0 {
		t.Errorf("MaxQ = %v, want 5.0", got)
	}
	if got := q.ArgMaxQ(s); got != 1 {
		t.Errorf("ArgMaxQ = %v, want 1", got)
	}
}

func TestDQN_ForwardAndUpdate(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	d := NewDQN(rng)
	in := make([]float64, DqnInput)
	for i := range in {
		in[i] = float64(i+1) / float64(DqnInput+1)
	}
	_, out := d.Forward(in)
	if len(out) != DqnOutput {
		t.Fatalf("output dim = %d, want %d", len(out), DqnOutput)
	}
	// Update with a target far from current output and confirm
	// the chosen action's weights change.
	before := make([]float64, len(d.W2))
	copy(before, d.W2)
	d.Update(in, 0, out[0]+50.0, 0.01)
	changed := false
	for i, v := range d.W2 {
		if v != before[i] {
			changed = true
			break
		}
	}
	if !changed {
		t.Error("DQN W2 weights did not change after non-trivial Update")
	}
}

func TestDQN_UpdateZeroDeltaIsNoop(t *testing.T) {
	rng := rand.New(rand.NewSource(8))
	d := NewDQN(rng)
	in := make([]float64, DqnInput)
	_, out := d.Forward(in)
	before := make([]float64, len(d.W1))
	copy(before, d.W1)
	d.Update(in, 0, out[0], 0.01)
	for i, v := range d.W1 {
		if v != before[i] {
			t.Errorf("W1[%d] changed despite zero-delta", i)
		}
	}
}

func TestArgMaxAndMaxFloat(t *testing.T) {
	v := []float64{1.0, 3.0, 2.0, 0.5}
	if ArgMaxFloat(v) != 1 {
		t.Errorf("ArgMaxFloat = %d, want 1", ArgMaxFloat(v))
	}
	if MaxFloat(v) != 3.0 {
		t.Errorf("MaxFloat = %v, want 3.0", MaxFloat(v))
	}
}

func TestAgentDqnFeatures_Shape(t *testing.T) {
	w := NewWorld(900)
	w.EnableHazards()
	a := SpawnAgentForTest(w, '5')
	w.Heat[a.Pos.Y][a.Pos.X] = true
	w.Stench[a.Pos.Y][a.Pos.X] = true
	f := AgentDqnFeatures(w, a)
	if len(f) != DqnInput {
		t.Errorf("feature len = %d, want %d", len(f), DqnInput)
	}
	if f[4] != 1 || f[5] != 1 {
		t.Errorf("heat/stench = %v %v, want 1/1", f[4], f[5])
	}
}

// TestAgentDqnFeatures_ScentSlots: the 4 trailing slots reflect
// signed scent freshness at the agent's cardinal neighbors, with
// the trustee scent producing a positive value and a negative-trust
// label producing a negative value.
func TestAgentDqnFeatures_ScentSlots(t *testing.T) {
	w := NewWorld(910)
	a := SpawnAgentForTest(w, '5')
	a.Pos = Pos{X: 40, Y: 40}
	a.CurrentTrustee = '2'
	a.TrustScores = map[rune]float64{'3': -1}
	w.Cycle = 50
	// Plant trustee scent EAST (Cardinals[3]) and negative-trust
	// scent WEST (Cardinals[2]). Make sure the OTHER scent cells
	// the test world might already have around (40,40) don't
	// pollute the assertion by zeroing N/S explicitly.
	north := Pos{X: 40, Y: 39}
	south := Pos{X: 40, Y: 41}
	east := Pos{X: 41, Y: 40}
	west := Pos{X: 39, Y: 40}
	w.ScentCycle[north.Y][north.X] = 0
	w.ScentCycle[south.Y][south.X] = 0
	w.ScentOwner[east.Y][east.X] = '2'
	w.ScentCycle[east.Y][east.X] = 50 // freshness = 1.0
	w.ScentOwner[west.Y][west.X] = '3'
	w.ScentCycle[west.Y][west.X] = 50
	f := AgentDqnFeatures(w, a)
	// Cardinals = {N, S, W, E} → slots 6..9 correspond to those.
	if f[6] != 0 {
		t.Errorf("north slot = %v, want 0", f[6])
	}
	if f[7] != 0 {
		t.Errorf("south slot = %v, want 0", f[7])
	}
	if f[8] != -1.0 {
		t.Errorf("west slot = %v, want -1.0 (negative trust)", f[8])
	}
	if f[9] != 1.0 {
		t.Errorf("east slot = %v, want +1.0 (trustee)", f[9])
	}
}

// TestHasState_Distinguishes: HasState returns false for an unseen
// position and true after any Q-value is written.
func TestHasState_Distinguishes(t *testing.T) {
	q := NewQLearning()
	s := Pos{X: 5, Y: 5}
	if q.HasState(s) {
		t.Error("HasState should be false before any SetQ")
	}
	q.SetQ(s, 2, 1.5)
	if !q.HasState(s) {
		t.Error("HasState should be true after SetQ")
	}
}
