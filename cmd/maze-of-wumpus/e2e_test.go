package main

// e2e_test.go — spawn the compiled binary as a subprocess and verify
// behavior via stdin/stdout/stderr/exit-code. All e2e tests run in
// --headless mode with a fixed --seed so they're deterministic.

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

var binPath string

// TestMain builds the binary once into a temp dir before any test
// runs. e2e tests skip cleanly if the build fails.
func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "maze-e2e-*")
	if err == nil {
		bin := filepath.Join(tmp, "maze-of-wumpus-e2e")
		build := exec.Command("go", "build", "-o", bin, ".")
		if out, berr := build.CombinedOutput(); berr == nil {
			binPath = bin
		} else {
			_, _ = os.Stderr.Write(out)
		}
		defer os.RemoveAll(tmp)
	}
	os.Exit(m.Run())
}

func requireBinary(t *testing.T) {
	t.Helper()
	if binPath == "" {
		t.Skip("binary not built")
	}
}

func TestE2E_HeadlessOutputFormat(t *testing.T) {
	requireBinary(t)
	cmd := exec.Command(binPath, "--headless", "--steps=10", "--seed=42")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("run failed: %v\nstderr=%s", err, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Errorf("unexpected stderr: %q", stderr.String())
	}
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(lines) < 1 {
		t.Fatalf("no output lines")
	}
	// Build a per-agent matcher fragment for each label in the
	// current 6-agent lineup so the regex stays in lockstep with
	// any future renumbering.
	agentPattern := func(label string) string {
		return label + `_alive=(true|false) ` +
			label + `_deaths=\d+ ` +
			label + `_goals=\d+ ` +
			label + `_dist=\d+ ` +
			label + `_score=-?\d+\.\d+ `
	}
	pat := `^cycle=(\d+) optimal=\d+ paths=\d+ `
	for _, l := range []string{"1", "2", "3", "4", "5", "6"} {
		pat += agentPattern(l)
	}
	pat += `game_over=(true|false)$`
	re := regexp.MustCompile(pat)
	for i, ln := range lines {
		m := re.FindStringSubmatch(ln)
		if m == nil {
			t.Errorf("line %d malformed: %q", i, ln)
			continue
		}
		c, _ := strconv.Atoi(m[1])
		if c != i {
			t.Errorf("line %d: cycle=%d, want %d", i, c, i)
		}
	}
}

func TestE2E_HeadlessDeterminism(t *testing.T) {
	requireBinary(t)
	run := func() string {
		cmd := exec.Command(binPath, "--headless", "--steps=30", "--seed=99")
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("run: %v", err)
		}
		return string(out)
	}
	a, b := run(), run()
	if a != b {
		t.Error("same seed produced different output across runs")
	}
}

func TestE2E_DifferentSeeds(t *testing.T) {
	requireBinary(t)
	get := func(seed string) string {
		cmd := exec.Command(binPath, "--headless", "--steps=50", "--seed="+seed)
		out, _ := cmd.Output()
		return string(out)
	}
	if get("1") == get("2") {
		t.Error("seed=1 and seed=2 produced identical output")
	}
}

func TestE2E_ExitCodeZero(t *testing.T) {
	requireBinary(t)
	cmd := exec.Command(binPath, "--headless", "--steps=3", "--seed=7")
	if err := cmd.Run(); err != nil {
		t.Errorf("clean headless run exited non-zero: %v", err)
	}
}

func TestE2E_BadFlag_ExitCode2(t *testing.T) {
	requireBinary(t)
	cmd := exec.Command(binPath, "--not-a-flag")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		t.Fatal("expected non-zero exit code for bad flag")
	}
	if exit, ok := err.(*exec.ExitError); ok {
		if exit.ExitCode() != 2 {
			t.Errorf("exit code = %d, want 2", exit.ExitCode())
		}
	}
	if stderr.Len() == 0 {
		t.Error("expected error message on stderr")
	}
}

func TestE2E_InitialPopulationMatches(t *testing.T) {
	requireBinary(t)
	cmd := exec.Command(binPath, "--headless", "--steps=0", "--seed=1")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	first := strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)[0]
	if !strings.HasPrefix(first, "cycle=0") {
		t.Errorf("first line not cycle=0: %q", first)
	}
	for _, l := range []string{"1_alive=", "2_alive=", "3_alive=", "4_alive=", "5_alive=", "6_alive="} {
		if !strings.Contains(first, l) {
			t.Errorf("missing %s in first record: %q", l, first)
		}
	}
}
