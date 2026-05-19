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
	in := []float64{0.1, 0.2, 0.3, 0.4, 0, 1}
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
	in := []float64{0, 0, 0, 0, 0, 0}
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
