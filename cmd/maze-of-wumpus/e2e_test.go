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
	re := regexp.MustCompile(
		`^cycle=(\d+) wumpus_died=\d+ wumpus_alive=\d+ optimal=\d+ paths=\d+ ` +
			`1_alive=(true|false) 1_deaths=\d+ 1_kills=\d+ 1_goals=\d+ 1_dist=\d+ 1_score=-?\d+\.\d+ ` +
			`2_alive=(true|false) 2_deaths=\d+ 2_kills=\d+ 2_goals=\d+ 2_dist=\d+ 2_score=-?\d+\.\d+ ` +
			`3_alive=(true|false) 3_deaths=\d+ 3_kills=\d+ 3_goals=\d+ 3_dist=\d+ 3_score=-?\d+\.\d+ ` +
			`4_alive=(true|false) 4_deaths=\d+ 4_kills=\d+ 4_goals=\d+ 4_dist=\d+ 4_score=-?\d+\.\d+ ` +
			`5_alive=(true|false) 5_deaths=\d+ 5_kills=\d+ 5_goals=\d+ 5_dist=\d+ 5_score=-?\d+\.\d+ ` +
			`6_alive=(true|false) 6_deaths=\d+ 6_kills=\d+ 6_goals=\d+ 6_dist=\d+ 6_score=-?\d+\.\d+ ` +
			`7_alive=(true|false) 7_deaths=\d+ 7_kills=\d+ 7_goals=\d+ 7_dist=\d+ 7_score=-?\d+\.\d+ ` +
			`game_over=(true|false)$`,
	)
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
	re := regexp.MustCompile(`wumpus_alive=(\d+)`)
	m := re.FindStringSubmatch(first)
	if m == nil {
		t.Fatalf("could not find wumpus_alive in %q", first)
	}
	n, _ := strconv.Atoi(m[1])
	// Wumpus default to disabled at construction → none exist.
	if n != 0 {
		t.Errorf("wumpus_alive=%d, want 0 (default-disabled)", n)
	}
	for _, l := range []string{"1_alive=", "2_alive=", "3_alive=", "4_alive=", "5_alive=", "6_alive=", "7_alive="} {
		if !strings.Contains(first, l) {
			t.Errorf("missing %s in first record: %q", l, first)
		}
	}
}
