package main

import (
	"io"
	"strings"
	"testing"
	"time"
)

// TestRunParallel_FullStrategiesRaceFree runs the real, swarm-wired world
// (buildWorld dispatches the swarm clone path, POMCP rollouts, per-agent
// RNG, scent buffering) through both the serial baseline and the parallel
// runner. Run with -race to validate the full concurrency model end-to-
// end, including the swarm clone moves and lifecycle barrier.
func TestRunParallel_FullStrategiesRaceFree(t *testing.T) {
	var out strings.Builder
	runBenchmark(1, 250*time.Millisecond, &out)
	s := out.String()
	if !strings.Contains(s, "serial:") || !strings.Contains(s, "parallel:") {
		t.Errorf("report missing serial/parallel sections:\n%s", s)
	}
}

// TestRunParallel_Discard is a minimal smoke run writing nowhere.
func TestRunParallel_Discard(t *testing.T) {
	runBenchmark(2, 150*time.Millisecond, io.Discard)
}
