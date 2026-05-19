package logging

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"maze-of-wumpus/src/world"
)

// spawnAgentForTest plants the labeled agent at the entrance.
func spawnAgentForTest(w *world.World, label rune) *world.Agent {
	entrance := w.Maze.EntrancePos
	if existing := w.AgentAt[entrance.Y][entrance.X]; existing != nil {
		existing.Alive = false
		w.AgentAt[entrance.Y][entrance.X] = nil
	}
	a := w.AgentByLabel(label)
	a.Alive = true
	a.Pos = entrance
	a.RespawnIn = -1
	a.Stats.ActualDistance = 0
	w.AgentAt[entrance.Y][entrance.X] = a
	return a
}

func TestAgentLogger_CreatesFiveFiles(t *testing.T) {
	dir := t.TempDir()
	al := NewAgentLogger(dir)
	defer al.Close()
	for _, label := range []rune{'1', '2', '3', '4', '5', '6', '7'} {
		path := filepath.Join(dir, string(label)+".log")
		if _, err := os.Stat(path); err != nil {
			t.Errorf("missing %s: %v", path, err)
		}
	}
}

func TestAgentLogger_LogTick_WritesJSON(t *testing.T) {
	dir := t.TempDir()
	al := NewAgentLogger(dir)
	al.SetStrategyNamer(func(label rune) string {
		return "test-" + string(label)
	})
	w := world.NewWorld(900)
	al.LogTick(w)
	al.Close()
	for _, label := range []rune{'1', '2', '3', '4', '5', '6', '7'} {
		path := filepath.Join(dir, string(label)+".log")
		f, err := os.Open(path)
		if err != nil {
			t.Fatalf("open %s: %v", path, err)
		}
		sc := bufio.NewScanner(f)
		if !sc.Scan() {
			t.Errorf("%s: no line written", path)
			f.Close()
			continue
		}
		var rec AgentLogRecord
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			t.Errorf("%s: invalid JSON: %v", path, err)
		}
		if !strings.EqualFold(rec.Label, string(label)) {
			t.Errorf("%s: label = %q, want %q", path, rec.Label, string(label))
		}
		if rec.Strategy == "" {
			t.Errorf("%s: empty strategy", path)
		}
		f.Close()
	}
}

func TestAgentLogger_NilSafe(t *testing.T) {
	var al *AgentLogger
	w := world.NewWorld(901)
	al.LogTick(w)
	al.Close()
	al.SetStrategyNamer(nil)
}

func TestAgentLogger_TruncatesExistingFiles(t *testing.T) {
	dir := t.TempDir()
	junk := []byte("stale content from a previous launch\nsecond line\n")
	for _, label := range []rune{'1', '2', '3', '4', '5', '6', '7'} {
		path := filepath.Join(dir, string(label)+".log")
		if err := os.WriteFile(path, junk, 0644); err != nil {
			t.Fatalf("seed %s: %v", path, err)
		}
	}
	al := NewAgentLogger(dir)
	al.Close()
	for _, label := range []rune{'1', '2', '3', '4', '5', '6', '7'} {
		path := filepath.Join(dir, string(label)+".log")
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat %s: %v", path, err)
		}
		if info.Size() != 0 {
			t.Errorf("%s: size = %d, want 0", path, info.Size())
		}
	}
}

func TestAgentLogger_CreatesMissingDir(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "build", "logs")
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("precondition: %s should not exist yet", dir)
	}
	al := NewAgentLogger(dir)
	defer al.Close()
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("dir not created: %v", err)
	}
	for _, label := range []rune{'1', '2', '3', '4', '5', '6', '7'} {
		path := filepath.Join(dir, string(label)+".log")
		if _, err := os.Stat(path); err != nil {
			t.Errorf("missing %s: %v", path, err)
		}
	}
}

// TestAgentLogger_SkipsUnknownLabel: an agent with a label outside
// a..e is silently skipped (encoders map miss).
func TestAgentLogger_SkipsUnknownLabel(t *testing.T) {
	dir := t.TempDir()
	al := NewAgentLogger(dir)
	defer al.Close()
	w := world.NewWorld(910)
	// Inject a fake agent with an unmapped label.
	w.Agents = append(w.Agents, &world.Agent{Label: 'Z'})
	al.LogTick(w) // must not panic / write
}

// TestAgentLogger_CreateFailure: pointing at a directory we cannot
// create exercises the "skip on error" path of NewAgentLogger.
func TestAgentLogger_CreateFailure(t *testing.T) {
	// On Unix, /proc/1 is a directory whose contents are owned by
	// the kernel; trying to create files there fails. Use that as a
	// non-writable path. Skip if it doesn't exist (e.g., macOS).
	if _, err := os.Stat("/proc/1"); err != nil {
		dir := t.TempDir()
		// Make the dir read-only.
		_ = os.Chmod(dir, 0500)
		defer os.Chmod(dir, 0700)
		al := NewAgentLogger(dir)
		al.Close()
		return
	}
	al := NewAgentLogger("/proc/1/maze-logs")
	al.Close()
}

func TestAgentLogger_E_HasDqnQValues(t *testing.T) {
	dir := t.TempDir()
	al := NewAgentLogger(dir)
	w := world.NewWorld(902)
	_ = spawnAgentForTest(w, '5')
	al.LogTick(w)
	al.Close()
	f, err := os.Open(filepath.Join(dir, "5.log"))
	if err != nil {
		t.Fatalf("open 5.log: %v", err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	if !sc.Scan() {
		t.Fatal("no line in 5.log")
	}
	var rec AgentLogRecord
	if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(rec.DqnQValues) != world.DqnOutput {
		t.Errorf("dqn_q length = %d, want %d", len(rec.DqnQValues), world.DqnOutput)
	}
}
