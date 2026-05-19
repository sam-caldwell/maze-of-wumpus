package main

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"

	"maze-of-wumpus/src/tui"
	"maze-of-wumpus/src/world"
)

func TestRunApp_BadFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runApp([]string{"--not-a-flag"}, &stdout, &stderr); code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
}

func TestRunApp_HeadlessMode(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runApp([]string{"--headless", "--steps=5", "--seed=42"}, &stdout, &stderr)
	if code != 0 {
		t.Errorf("exit code = %d, want 0; stderr=%s", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "cycle=0") {
		t.Errorf("missing cycle=0 in output:\n%s", out)
	}
	if !strings.Contains(out, "1_alive=") {
		t.Errorf("missing 1_alive in output:\n%s", out)
	}
}

func TestRunApp_TUIPath_RunsProgram(t *testing.T) {
	called := false
	prev := runProgram
	runProgram = func(seed int64) error {
		called = true
		return nil
	}
	defer func() { runProgram = prev }()
	var stdout, stderr bytes.Buffer
	if code := runApp([]string{"--seed=1"}, &stdout, &stderr); code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if !called {
		t.Error("runProgram was not invoked")
	}
}

func TestRunApp_TUIPath_PropagatesError(t *testing.T) {
	prev := runProgram
	runProgram = func(seed int64) error { return errors.New("boom") }
	defer func() { runProgram = prev }()
	var stdout, stderr bytes.Buffer
	code := runApp([]string{"--seed=1"}, &stdout, &stderr)
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "boom") {
		t.Errorf("expected error in stderr, got: %q", stderr.String())
	}
}

func TestRunApp_DefaultSeed(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runApp([]string{"--headless", "--steps=1"}, &stdout, &stderr)
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout.String(), "cycle=0") {
		t.Error("default-seed headless run produced no output")
	}
}

func TestMain_FunctionRuns(t *testing.T) {
	prevExit, prevProg, prevArgs := exitFunc, runProgram, os.Args
	defer func() { exitFunc, runProgram, os.Args = prevExit, prevProg, prevArgs }()
	exitFunc = func(int) {}
	runProgram = func(int64) error { return nil }
	os.Args = []string{"maze-of-wumpus", "--seed=1"}
	main()
}

func TestRunHeadlessLoop_GameOverShortCircuit(t *testing.T) {
	w := world.NewWorld(11)
	w.GameOver = true
	var buf bytes.Buffer
	runHeadlessLoop(w, 100, &buf, nil)
	got := strings.Count(buf.String(), "cycle=")
	if got > 5 {
		t.Errorf("expected early exit, got %d state lines", got)
	}
	if !strings.Contains(buf.String(), "game_over=true") {
		t.Errorf("expected game_over=true line, got: %s", buf.String())
	}
}

func TestWriteHeadlessState_PerAgentFields(t *testing.T) {
	w := world.NewWorld(123)
	a := w.AgentByLabel('1')
	a.RespawnIn = 0
	w.RespawnAgents()
	var buf bytes.Buffer
	writeHeadlessState(&buf, w)
	s := buf.String()
	for _, want := range []string{
		"1_alive=", "2_alive=", "3_alive=", "4_alive=", "5_alive=", "6_alive=", "7_alive=",
		"1_score=", "2_score=", "3_score=", "4_score=", "5_score=", "6_score=", "7_score=",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in output: %s", want, s)
		}
	}
}

// TestProductionRunProgram_WithStubbedTea exercises the real
// runProgram body by stubbing the bubbletea entry point.
func TestProductionRunProgram_WithStubbedTea(t *testing.T) {
	called := false
	prev := teaRunner
	teaRunner = func(m tui.Model) error {
		called = true
		if m.World == nil {
			t.Error("model has no world")
		}
		if m.Logger == nil {
			t.Error("model has no logger")
		}
		return nil
	}
	defer func() { teaRunner = prev }()
	// Use a temp working directory so build/logs/ goes there.
	tmp := t.TempDir()
	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)
	_ = os.Chdir(tmp)
	if err := runProgram(123); err != nil {
		t.Errorf("runProgram returned %v", err)
	}
	if !called {
		t.Error("teaRunner not invoked")
	}
}

// TestProductionRunProgram_CallsAnnounce: runProgram fires announce()
// before handing off to the TUI runner. The announce var is swapped
// out so the real `say` binary is never invoked during tests.
func TestProductionRunProgram_CallsAnnounce(t *testing.T) {
	prevAnnounce, prevTea := announce, teaRunner
	defer func() { announce, teaRunner = prevAnnounce, prevTea }()
	announceCalled, teaCalled := false, false
	announce = func() { announceCalled = true }
	teaRunner = func(tui.Model) error {
		teaCalled = true
		if !announceCalled {
			t.Error("announce() should fire before the TUI runner starts")
		}
		return nil
	}
	tmp := t.TempDir()
	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)
	_ = os.Chdir(tmp)
	if err := runProgram(123); err != nil {
		t.Fatalf("runProgram returned %v", err)
	}
	if !announceCalled {
		t.Error("announce was never called")
	}
	if !teaCalled {
		t.Error("teaRunner was never called")
	}
}

func TestBuildWorld_AttachesStrategies(t *testing.T) {
	w := buildWorld(7)
	for _, label := range []rune{'1', '2', '3', '4', '5', '6', '7'} {
		a := w.AgentByLabel(label)
		if a.Strategy == nil {
			t.Errorf("agent %c missing strategy", label)
		}
	}
	for _, wm := range w.Wumpus {
		if wm.Strategy == nil {
			t.Errorf("wumpus #%d missing strategy", wm.ID)
		}
	}
}
