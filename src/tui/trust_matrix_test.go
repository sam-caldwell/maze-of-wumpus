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
// spacer + "Agent-Algorithm Trust" title + algorithm header + agent count
// agent rows should appear when the world has strategy letters
// configured.
func TestRenderTrustMatrix_AlgorithmMatrixBelow(t *testing.T) {
	w := world.NewWorldWithConfig(world.Config{
		Seed:            4,
		StrategyLetters: []rune{'R', 'S', 'T'},
	})
	lines := renderTrustMatrixLines(w)
	// Layout post-legend-move: title + header + agents + extra-legend
	// rows + spacer + algorithm-trust title. The 16-step heat legend
	// needs 8 rows; when the roster is shorter, the leftover entries
	// spill onto their own lines below the agent rows.
	extra := legendSpillRows(len(w.Agents))
	algoStart := 1 + 1 + len(w.Agents) + extra + 1
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
	extra := legendSpillRows(len(w.Agents))
	titleIdx := 1 + 1 + len(w.Agents) + extra + 1 + 1 + 1 + len(w.Agents) + 1 + 1 + 1 + algoLetterCount + 1
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

// TestTrustCell_Diagonal: the self-diagonal must render as the dim
// '·' glyph regardless of score/present arguments.
func TestTrustCell_Diagonal(t *testing.T) {
	got := trustCell(0, false, true)
	want := "\x1b[38;5;240m·\x1b[0m"
	if got != want {
		t.Errorf("trustCell(_,_,diag) = %q, want %q", got, want)
	}
	// Score+present shouldn't affect diagonal output.
	if trustCell(10, true, true) != want {
		t.Error("diagonal output should be invariant to score/present")
	}
}

// TestTrustCell_Empty: non-diagonal cells with no recorded score
// must render as the bright-white '-' placeholder.
func TestTrustCell_Empty(t *testing.T) {
	got := trustCell(0, false, false)
	want := "\x1b[38;5;255m-\x1b[0m"
	if got != want {
		t.Errorf("trustCell(0,false,false) = %q, want %q", got, want)
	}
}

// TestTrustCell_HeatPalette: each in-range heat index must produce
// the exact precomputed colored '█' string. Locks in byte-for-byte
// equivalence with the old fmt.Sprintf implementation.
func TestTrustCell_HeatPalette(t *testing.T) {
	for idx, fg := range trustHeatFG {
		// Pick a score that maps to exactly this idx.
		var score float64
		if idx == 15 {
			score = TrustHeatCap + 1 // saturates
		} else if idx == 0 {
			score = 0.0001 // > 0 so present-branch fires, idx=0
		} else {
			// idx = score / TrustHeatCap * 15 → score = idx * TrustHeatCap / 15
			score = float64(idx) * TrustHeatCap / 15
		}
		got := trustCell(score, true, false)
		want := fmt.Sprintf("\x1b[38;5;%dm█\x1b[0m", fg)
		if got != want {
			t.Errorf("trustCell(idx=%d) = %q, want %q", idx, got, want)
		}
	}
}

// TestLegendCell_Range: every legend index 0..15 returns the expected
// "█ <2-digit idx>" string for the matching heat color.
func TestLegendCell_Range(t *testing.T) {
	for idx, fg := range trustHeatFG {
		got := legendCell(idx)
		want := fmt.Sprintf("\x1b[38;5;%dm█\x1b[0m %2d", fg, idx)
		if got != want {
			t.Errorf("legendCell(%d) = %q, want %q", idx, got, want)
		}
	}
}

// TestStrategyPerfCell_ByteEquivalence: across a range of values and
// widths the refactored helper must produce byte-identical output to
// the prior fmt.Sprintf implementation.
func TestStrategyPerfCell_ByteEquivalence(t *testing.T) {
	cases := []struct {
		value, width, max int
	}{
		{0, 5, 0},   // max=0 → idx=0
		{0, 5, 10},  // value=0 → idx=0
		{10, 5, 10}, // value=max → idx=15
		{5, 7, 10},  // mid
		{3, 12, 7},  // wide column
		{99, 3, 99}, // padding == 0
	}
	for _, c := range cases {
		idx := 0
		if c.max > 0 {
			idx = c.value * 15 / c.max
			if idx > 15 {
				idx = 15
			} else if idx < 0 {
				idx = 0
			}
		}
		want := fmt.Sprintf("\x1b[48;5;%dm\x1b[38;5;255m%*d\x1b[0m",
			strategyPerfHeatBG[idx], c.width, c.value)
		got := strategyPerfCell(c.value, c.width, c.max)
		if got != want {
			t.Errorf("strategyPerfCell(%d,%d,%d) = %q, want %q",
				c.value, c.width, c.max, got, want)
		}
	}
}

// TestStrategyPerfCell_PaddingClampsAtZero: when value's printed
// width >= column width, the cell must contain no leading spaces
// (i.e., no negative-padding crash, no double-padding).
func TestStrategyPerfCell_PaddingClampsAtZero(t *testing.T) {
	got := strategyPerfCell(12345, 3, 12345) // value wider than width
	// No leading spaces inside the colored region.
	if strings.Contains(got, "  12345") {
		t.Errorf("strategyPerfCell over-padded: %q", got)
	}
	if !strings.Contains(got, "12345") {
		t.Errorf("strategyPerfCell missing value: %q", got)
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
