package tui

import (
	"fmt"
	"strings"
	"testing"

	"maze-of-wumpus/src/world"
)

// TestRenderTrustMatrix_HasTitle: the rendered matrix's first line
// is the "Agent-Agent Trust" caption.
func TestRenderTrustMatrix_HasTitle(t *testing.T) {
	w := world.NewWorld(1)
	lines := renderTrustMatrixLines(w)
	if len(lines) < 1 {
		t.Fatal("no matrix lines")
	}
	if !strings.Contains(lines[0], "Agent-Agent Trust") {
		t.Errorf("first line = %q, want it to contain title", lines[0])
	}
}

// TestRenderTrustMatrix_HeaderAfterTitle: line 1 is the column-label
// header row (now that the title occupies line 0).
func TestRenderTrustMatrix_HeaderAfterTitle(t *testing.T) {
	w := world.NewWorld(2)
	lines := renderTrustMatrixLines(w)
	if len(lines) < 2 {
		t.Fatal("expected ≥ 2 lines (title + header)")
	}
	// Header should list every agent label after the 2-space
	// row-label gutter.
	for _, a := range w.Agents {
		if !strings.ContainsRune(lines[1], a.Label) {
			t.Errorf("header missing label %c: %q", a.Label, lines[1])
		}
	}
}

// TestRenderTrustMatrix_LegendInline: the 16-step heat legend is
// spliced onto the right edge of the first 8 agent rows of the
// Agent-Agent Trust grid, so the legend sits ADJACENT to the
// matrix instead of below it. Each of the first 8 rows carries a
// (low, high) index pair.
func TestRenderTrustMatrix_LegendInline(t *testing.T) {
	w := world.NewWorld(3)
	lines := renderTrustMatrixLines(w)
	// Agent rows start at index 2 (after title + header).
	const agentRowStart = 2
	for i := 0; i < 8; i++ {
		row := lines[agentRowStart+i]
		low := i
		high := i + 8
		if !strings.Contains(row, fmt.Sprintf("%2d", low)) {
			t.Errorf("agent row %d missing low legend value %d: %q",
				i, low, row)
		}
		if !strings.Contains(row, fmt.Sprintf("%2d", high)) {
			t.Errorf("agent row %d missing high legend value %d: %q",
				i, high, row)
		}
	}
}

// TestRenderTrustMatrix_AlgorithmMatrixBelow: after the legend, a
// spacer + "Agent-Algorithm Trust" title + algorithm header + 12
// agent rows should appear when the world has strategy letters
// configured.
func TestRenderTrustMatrix_AlgorithmMatrixBelow(t *testing.T) {
	w := world.NewWorldWithConfig(world.Config{
		Seed:            4,
		StrategyLetters: []rune{'R', 'S', 'T'},
	})
	lines := renderTrustMatrixLines(w)
	// Layout post-legend-move: title + header + 12 agents + spacer
	// + algorithm-trust title.
	algoStart := 1 + 1 + len(w.Agents) + 1
	if len(lines) < algoStart+1+1+len(w.Agents) {
		t.Fatalf("not enough lines for algorithm matrix: %d", len(lines))
	}
	if !strings.Contains(lines[algoStart], "Agent-Algorithm Trust") {
		t.Errorf("expected algorithm title at line %d, got %q",
			algoStart, lines[algoStart])
	}
	algoHdr := lines[algoStart+1]
	for _, l := range []rune{'R', 'S', 'T'} {
		if !strings.ContainsRune(algoHdr, l) {
			t.Errorf("algorithm header missing %c: %q", l, algoHdr)
		}
	}
}

// TestRenderTrustMatrix_AlgorithmLegend: after the algorithm matrix
// rows, a spacer + one row per letter with a (truncated) description
// appears when StrategyDescriptionForLetter is configured.
func TestRenderTrustMatrix_AlgorithmLegend(t *testing.T) {
	desc := map[rune]string{
		'R': "First strategy",
		'S': "Second strategy",
	}
	w := world.NewWorldWithConfig(world.Config{
		Seed:                         5,
		StrategyLetters:              []rune{'R', 'S'},
		StrategyDescriptionForLetter: func(l rune) string { return desc[l] },
	})
	lines := renderTrustMatrixLines(w)
	// Layout (from top), after the heat legend was moved inline
	// next to the Agent-Agent Trust grid:
	//   title + header + agents
	// + spacer + algo-title + algo-header + agents
	// + spacer + perf-title + perf-header + N perf-rows
	// + spacer + "Agent Strategies" title + N algo-legend
	algoLetterCount := 2 // R, S configured below
	titleIdx := 1 + 1 + len(w.Agents) + 1 + 1 + 1 + len(w.Agents) + 1 + 1 + 1 + algoLetterCount + 1
	legendStart := titleIdx + 1
	if len(lines) < legendStart+2 {
		t.Fatalf("not enough lines: %d (need ≥ %d)", len(lines), legendStart+2)
	}
	if !strings.Contains(lines[titleIdx], "Agent Strategies") {
		t.Errorf("expected 'Agent Strategies' title at line %d, got %q",
			titleIdx, lines[titleIdx])
	}
	if !strings.Contains(lines[legendStart], "First strategy") {
		t.Errorf("legend row 0 = %q, want R description", lines[legendStart])
	}
	if !strings.Contains(lines[legendStart+1], "Second strategy") {
		t.Errorf("legend row 1 = %q, want S description", lines[legendStart+1])
	}
}

// TestRenderTrustMatrix_StrategyPerfTable: the Strategy Performance
// section renders one row per configured strategy letter with the
// expected counters.
func TestRenderTrustMatrix_StrategyPerfTable(t *testing.T) {
	w := world.NewWorldWithConfig(world.Config{
		Seed:            12,
		StrategyLetters: []rune{'R', 'S'},
	})
	if w.StrategyPerf == nil {
		w.StrategyPerf = map[rune]*world.StrategyPerfCounts{}
	}
	w.StrategyPerf['R'] = &world.StrategyPerfCounts{TTLExpiry: 3, NoFollow: 7, Following: 2}
	w.StrategyPerf['S'] = &world.StrategyPerfCounts{TTLExpiry: 0, NoFollow: 1, Following: 5}
	lines := renderTrustMatrixLines(w)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "Strategy Performance") {
		t.Error("missing 'Strategy Performance' title")
	}
	for _, want := range []string{"Die.TTL", "Win.NoFollow", "Win.Following", "#Runs"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing column header %q", want)
		}
	}
	// Verify R's row has all four numbers. R's #Runs == 7 + 2 = 9.
	for _, want := range []string{"3", "7", "2", "9"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing R-row counter %q", want)
		}
	}
}

// TestRenderTrustMatrix_StrategyPerfHeatBG: each column in the
// Strategy Performance table is normalized to a [0..15] background
// heat. The row with the column maximum should carry the brightest
// red bg (256-color 196); a row with value 0 should carry the
// near-black bg (256-color 232).
func TestRenderTrustMatrix_StrategyPerfHeatBG(t *testing.T) {
	w := world.NewWorldWithConfig(world.Config{
		Seed:            13,
		StrategyLetters: []rune{'R', 'S'},
	})
	w.StrategyPerf = map[rune]*world.StrategyPerfCounts{
		'R': {TTLExpiry: 10, NoFollow: 0, Following: 0},
		'S': {TTLExpiry: 0, NoFollow: 0, Following: 0},
	}
	lines := renderTrustMatrixLines(w)
	// Find R's perf row: starts with " R  " (then ANSI bg).
	var rRow, sRow string
	for _, l := range lines {
		// Strip ANSI to inspect prefix.
		if strings.HasPrefix(l, " R  \x1b") {
			rRow = l
		}
		if strings.HasPrefix(l, " S  \x1b") {
			sRow = l
		}
	}
	if rRow == "" || sRow == "" {
		t.Fatalf("perf rows not found; lines=%v", lines)
	}
	// R has TTL=10 (the column max) → bg 196.
	if !strings.Contains(rRow, "\x1b[48;5;196m") {
		t.Errorf("R row should contain bg=196 (max heat): %q", rRow)
	}
	// S has TTL=0 → bg 232 (coldest).
	if !strings.Contains(sRow, "\x1b[48;5;232m") {
		t.Errorf("S row should contain bg=232 (min heat): %q", sRow)
	}
}

// TestRenderTrustMatrix_WumpusLegend_WhenWumpusAlive: when at least
// one wumpus is alive with each HuntMode, the Wumpus Strategies
// legend renders the title + one row per active mode.
func TestRenderTrustMatrix_WumpusLegend_WhenWumpusAlive(t *testing.T) {
	w := world.NewWorldWithConfig(world.Config{
		Seed:            7,
		StrategyLetters: []rune{'R'},
	})
	w.EnableHazards()
	// Force exactly one wumpus per mode so all 3 rows render.
	if len(w.Wumpus) < 3 {
		t.Skip("not enough wumpus spawned at this seed")
	}
	for i, mode := range []world.WumpusHuntMode{
		world.WumpusHuntBayesian,
		world.WumpusHuntWander,
		world.WumpusHuntCrowd,
	} {
		w.Wumpus[i].HuntMode = mode
		w.Wumpus[i].Alive = true
	}
	lines := renderTrustMatrixLines(w)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "Wumpus Strategies") {
		t.Error("output missing 'Wumpus Strategies' title")
	}
	for _, want := range []string{
		"Inductive Bayesian smell-tracking",
		"Random walk lightly biased",
		"Swarm hunting",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("output missing wumpus description fragment %q", want)
		}
	}
}

// TestRenderTrustMatrix_WumpusLegend_PerModeCount: each rendered
// row should start with the number of alive wumpus using that
// mode. Force 2 Bayesian + 1 Crowd alive; verify the prefix.
func TestRenderTrustMatrix_WumpusLegend_PerModeCount(t *testing.T) {
	w := world.NewWorldWithConfig(world.Config{
		Seed:            11,
		StrategyLetters: []rune{'R'},
	})
	w.EnableHazards()
	if len(w.Wumpus) < 3 {
		t.Skip("not enough wumpus spawned at this seed")
	}
	for _, wm := range w.Wumpus {
		wm.Alive = false
	}
	w.Wumpus[0].HuntMode = world.WumpusHuntBayesian
	w.Wumpus[0].Alive = true
	w.Wumpus[1].HuntMode = world.WumpusHuntBayesian
	w.Wumpus[1].Alive = true
	w.Wumpus[2].HuntMode = world.WumpusHuntCrowd
	w.Wumpus[2].Alive = true
	lines := renderTrustMatrixLines(w)
	joined := strings.Join(lines, "\n")
	// Bayesian row: "  2  Inductive..."
	if !strings.Contains(joined, "  2  Inductive Bayesian smell-tracking") {
		t.Errorf("expected '  2  Inductive Bayesian...' row in output")
	}
	// Crowd row: "  1  Swarm..."
	if !strings.Contains(joined, "  1  Swarm hunting") {
		t.Errorf("expected '  1  Swarm hunting...' row in output")
	}
	// Wander should NOT appear (no alive wander wumpus).
	if strings.Contains(joined, "Random walk lightly") {
		t.Errorf("Wander row should not render (no alive wander wumpus)")
	}
}

// TestRenderTrustMatrix_WumpusLegend_NoAlive: when no wumpus is
// alive, the legend shows a placeholder line instead of empty rows.
func TestRenderTrustMatrix_WumpusLegend_NoAlive(t *testing.T) {
	w := world.NewWorldWithConfig(world.Config{
		Seed:            8,
		StrategyLetters: []rune{'R'},
	})
	// World defaults: WumpusDisabled=true → no wumpus spawned at all.
	lines := renderTrustMatrixLines(w)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "no active wumpus") {
		t.Errorf("expected '(no active wumpus)' placeholder somewhere in output")
	}
}

// TestRenderTrustMatrix_EventsTable_FiveLines: the Events table
// always renders exactly EventsVisible (=5) message lines, padding
// with blanks when the buffer is shorter.
func TestRenderTrustMatrix_EventsTable_FiveLines(t *testing.T) {
	w := world.NewWorldWithConfig(world.Config{
		Seed:            9,
		StrategyLetters: []rune{'R'},
	})
	w.RecordEvent("red", "Agent 1 killed by Wumpus")
	w.RecordEvent("green", "Agent 2 reached goal")
	lines := renderTrustMatrixLines(w)
	// Find the "Events" title.
	titleIdx := -1
	for i, l := range lines {
		if strings.Contains(l, "Events") && !strings.Contains(l, "Strategies") {
			titleIdx = i
			break
		}
	}
	if titleIdx < 0 {
		t.Fatal("'Events' title not found in output")
	}
	if titleIdx+world.EventsVisible >= len(lines) {
		t.Fatalf("not enough lines after events title: %d", len(lines))
	}
	// 5 lines below the title; with 2 events posted, the first 3
	// should be blank and the last 2 should carry the messages.
	bodyStart := titleIdx + 1
	if !strings.Contains(lines[bodyStart+3], "Agent 1 killed by Wumpus") {
		t.Errorf("expected death event on line %d, got %q",
			bodyStart+3, lines[bodyStart+3])
	}
	if !strings.Contains(lines[bodyStart+4], "Agent 2 reached goal") {
		t.Errorf("expected goal event on line %d, got %q",
			bodyStart+4, lines[bodyStart+4])
	}
}

// TestRenderTrustMatrix_EventsTable_ScrollsNewestAtBottom: with
// more than 5 events posted, only the LAST 5 render, with the most
// recent at the bottom.
func TestRenderTrustMatrix_EventsTable_ScrollsNewestAtBottom(t *testing.T) {
	w := world.NewWorldWithConfig(world.Config{
		Seed:            10,
		StrategyLetters: []rune{'R'},
	})
	// Post 7 distinct events. After scrolling, only the last 5
	// (events 3..7) should be visible.
	for i := 1; i <= 7; i++ {
		w.RecordEvent("red", fmt.Sprintf("event-%d", i))
	}
	lines := renderTrustMatrixLines(w)
	last := lines[len(lines)-1]
	if !strings.Contains(last, "event-7") {
		t.Errorf("bottom line = %q, want 'event-7' (newest)", last)
	}
	for _, l := range lines {
		if strings.Contains(l, "event-1") || strings.Contains(l, "event-2") {
			t.Errorf("scrolled-off event still visible: %q", l)
		}
	}
}

// TestRenderTrustMatrix_AlgorithmLegendTruncates64: descriptions
// longer than 64 chars are truncated so the column stays within
// terminal-friendly width.
func TestRenderTrustMatrix_AlgorithmLegendTruncates64(t *testing.T) {
	long := strings.Repeat("x", 100)
	w := world.NewWorldWithConfig(world.Config{
		Seed:                         6,
		StrategyLetters:              []rune{'R'},
		StrategyDescriptionForLetter: func(rune) string { return long },
	})
	lines := renderTrustMatrixLines(w)
	last := lines[len(lines)-1]
	// last should be "R  " + at most 64 chars of x's. Total ≤ 67.
	if len(last) > 4+64 {
		t.Errorf("legend row exceeds 64-char description bound: %d chars", len(last))
	}
}
