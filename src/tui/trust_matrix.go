// trust_matrix.go — renders the per-agent trust grid that appears to
// the right of the maze. Rows = trusting agent; columns = trustee
// candidate. Each cell shows a 16-level heat color encoding
// TrustScores[row][col], or a white '-' when no data has been
// recorded yet. The diagonal renders a dim '·' (self).
package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"maze-of-wumpus/src/world"
)

// trustMatrixTitleStyle is the bold caption that sits above the
// trust grid in the TUI.
var trustMatrixTitleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("250"))

// eventStyleByColor maps an Event's semantic color tag to its ANSI
// style. "red" = death, "green" = goal reach. Unknown tags fall
// back to plain text (lipgloss zero-value).
var eventStyleByColor = map[string]lipgloss.Style{
	"red":    lipgloss.NewStyle().Foreground(lipgloss.Color("196")),
	"green":  lipgloss.NewStyle().Foreground(lipgloss.Color("46")),
	"yellow": lipgloss.NewStyle().Foreground(lipgloss.Color("226")),
}

// trustHeatFG: 16-step heat palette using ANSI 256-color codes.
// Index 0 = "no trust" (cold blue), 15 = "absolute trust" (deep red).
// Monotonic by perceived intensity so users can read it like a heat
// map. Values are 256-color FG codes; the cell block character is
// rendered in that color.
var trustHeatFG = [16]int{
	17, 18, 19, 20, // blue
	26, 33, 39, 45, // cyan
	51, 46, 82, 154, // green / yellow-green
	220, 214, 208, 196, // yellow → orange → red
}

// TrustHeatCap is the trust-score value that maps to color index 15
// (absolute trust). Above this, the color saturates. Sized so that
// ~5 within-TTL journeys (TrustWithinTTLBonus=2 + TrustGoalBonus=1 →
// +3 each) hits the top of the scale.
const TrustHeatCap = 15.0

// strategyPerfHeatBG: 16-step black-to-red background palette for
// the Strategy Performance table. Index 0 = near-black, 15 = bright
// red. Per-column normalization (max value → 15, zero → 0) lets
// users spot a winning/dominant column at a glance.
var strategyPerfHeatBG = [16]int{
	232, 233, 234, 235, // 0..3: near-black grayscale
	52, 52, 88, 88, // 4..7: dark reds
	124, 124, 160, 160, // 8..11: medium reds
	196, 196, 196, 196, // 12..15: bright red
}

// strategyPerfCell renders an integer value as a fixed-width
// background-colored cell. `max` is the column maximum (for
// normalization to [0,15]); a zero max leaves the cell at index 0.
// Foreground text is bright white (255) for legibility on any
// background in the palette.
func strategyPerfCell(value, width, max int) string {
	idx := 0
	if max > 0 {
		idx = value * 15 / max
		if idx > 15 {
			idx = 15
		} else if idx < 0 {
			idx = 0
		}
	}
	bg := strategyPerfHeatBG[idx]
	return fmt.Sprintf("\x1b[48;5;%dm\x1b[38;5;255m%*d\x1b[0m", bg, width, value)
}

// trustColorIdx maps a numeric trust score to a heat index in
// [0, 15]. Scores ≤ 0 (no trust / distrust) collapse to 0; scores
// ≥ TrustHeatCap saturate at 15.
func trustColorIdx(score float64) int {
	if score <= 0 {
		return 0
	}
	if score >= TrustHeatCap {
		return 15
	}
	return int(score / TrustHeatCap * 15)
}

// trustCell renders a single matrix cell as a 1-char ANSI-colored
// block (`█` in the heat color). 'isDiag' overrides to a dim '·'.
func trustCell(score float64, present bool, isDiag bool) string {
	if isDiag {
		return "\x1b[38;5;240m·\x1b[0m"
	}
	if !present {
		return "\x1b[38;5;255m-\x1b[0m" // bright white
	}
	idx := trustColorIdx(score)
	return fmt.Sprintf("\x1b[38;5;%dm█\x1b[0m", trustHeatFG[idx])
}

// renderTrustMatrixLines builds the trust matrix as a slice of pre-
// rendered text lines (one per row). The caller splices these next
// to the maze grid in View. The shape is:
//
//	   1 2 3 4 5 6 7 8 9 A B C    ← header row (column labels)
//	1  · - - - - - - - - - - -    ← agent 1's outgoing trust
//	2  - · - - - - - - - - - -
//	...
//	C  - - - - - - - - - - - ·    ← agent C's outgoing trust
func renderTrustMatrixLines(w *world.World) []string {
	lines := make([]string, 0, len(w.Agents)+2+len(trustHeatFG)/2+1)
	// Title line above the grid.
	lines = append(lines, trustMatrixTitleStyle.Render("Agent-Agent Trust"))
	// Header line: blank for row-label column, then column labels.
	var hdr strings.Builder
	hdr.WriteString("  ")
	for _, col := range w.Agents {
		hdr.WriteString(string(col.Label))
		hdr.WriteByte(' ')
	}
	lines = append(lines, hdr.String())
	// One row per agent. The 16-step heat legend (8 entries × 2
	// columns = 0..15) gets spliced into the right edge of the
	// first 8 agent rows so it sits next to the matrix instead
	// of below it — frees up vertical space for the panels that
	// follow.
	half := len(trustHeatFG) / 2 // 8
	for i, row := range w.Agents {
		var b strings.Builder
		b.WriteString(string(row.Label))
		b.WriteByte(' ')
		for _, col := range w.Agents {
			isDiag := row.Label == col.Label
			var score float64
			present := false
			if !isDiag && row.TrustScores != nil {
				score, present = row.TrustScores[col.Label]
			}
			b.WriteString(trustCell(score, present, isDiag))
			b.WriteByte(' ')
		}
		if i < half {
			b.WriteString("  ")
			b.WriteString(legendCell(i))
			b.WriteString("  ")
			b.WriteString(legendCell(i + half))
		}
		lines = append(lines, b.String())
	}
	// Agent-Algorithm Trust matrix sits below the legend. Rows are
	// agents (same order as above); columns are strategy letters
	// (R/S/T/U/V/W/X). Cells encode StrategyTrustScores using the
	// same heat scale as the Agent-Agent matrix.
	lines = append(lines, "")
	lines = append(lines, trustMatrixTitleStyle.Render("Agent-Algorithm Trust"))
	algoLetters := w.StrategyLettersForWorld()
	if len(algoLetters) == 0 {
		// Strategy registry not wired (tests, embedded use). Show a
		// placeholder so the layout stays predictable.
		lines = append(lines, "  (no strategies configured)")
		return lines
	}
	// Header row.
	var algoHdr strings.Builder
	algoHdr.WriteString("  ")
	for _, l := range algoLetters {
		algoHdr.WriteString(string(l))
		algoHdr.WriteByte(' ')
	}
	lines = append(lines, algoHdr.String())
	// One row per agent.
	for _, row := range w.Agents {
		var b strings.Builder
		b.WriteString(string(row.Label))
		b.WriteByte(' ')
		for _, col := range algoLetters {
			var score float64
			present := false
			if row.StrategyTrustScores != nil {
				score, present = row.StrategyTrustScores[col]
			}
			b.WriteString(trustCell(score, present, false))
			b.WriteByte(' ')
		}
		lines = append(lines, b.String())
	}
	// Strategy Performance: per-letter run-end tallies (TTL deaths,
	// no-follow vs following). Sits between the Agent-Algorithm
	// matrix and the Agent Strategies legend. Each numeric column
	// is independently normalized to a 0..15 heat scale so the
	// winning/dominant strategy in each column glows red.
	lines = append(lines, "")
	lines = append(lines, trustMatrixTitleStyle.Render("Strategy Performance"))
	lines = append(lines, "    Die.TTL  Win.NoFollow  Win.Following  #Runs")
	ttlMax, noFollowMax, followingMax, totalMax := 0, 0, 0, 0
	for _, l := range algoLetters {
		c := w.StrategyPerf[l]
		if c == nil {
			continue
		}
		if c.TTLExpiry > ttlMax {
			ttlMax = c.TTLExpiry
		}
		if c.NoFollow > noFollowMax {
			noFollowMax = c.NoFollow
		}
		if c.Following > followingMax {
			followingMax = c.Following
		}
		total := c.NoFollow + c.Following
		if total > totalMax {
			totalMax = total
		}
	}
	for _, l := range algoLetters {
		c := w.StrategyPerf[l]
		if c == nil {
			c = &world.StrategyPerfCounts{}
		}
		total := c.NoFollow + c.Following
		lines = append(lines, fmt.Sprintf(" %c  %s  %s  %s  %s",
			l,
			strategyPerfCell(c.TTLExpiry, 7, ttlMax),
			strategyPerfCell(c.NoFollow, 12, noFollowMax),
			strategyPerfCell(c.Following, 13, followingMax),
			strategyPerfCell(total, 5, totalMax)))
	}
	// Algorithm legend: one row per letter, "<letter>  <description>".
	// Spacer + bold title first so the legend sits visually distinct.
	lines = append(lines, "")
	lines = append(lines, trustMatrixTitleStyle.Render("Agent Strategies"))
	for _, l := range algoLetters {
		desc := w.StrategyDescription(l)
		if desc == "" {
			lines = append(lines, fmt.Sprintf("%c", l))
			continue
		}
		if len(desc) > 64 {
			desc = desc[:64]
		}
		lines = append(lines, fmt.Sprintf("%c  %s", l, desc))
	}
	// Wumpus Strategies legend: one row per hunt mode currently in
	// use by at least one alive wumpus. Spacer + bold title first.
	lines = append(lines, "")
	lines = append(lines, trustMatrixTitleStyle.Render("Wumpus Strategies"))
	active := w.ActiveWumpusModes()
	if len(active) == 0 {
		lines = append(lines, "  (no active wumpus)")
	}
	for _, mode := range active {
		desc := world.WumpusHuntModeDescription(mode)
		if len(desc) > 64 {
			desc = desc[:64]
		}
		count := w.WumpusModeCount(mode)
		lines = append(lines, fmt.Sprintf("%3d  %s", count, desc))
	}
	// Events table: bottom panel that scrolls newest-at-bottom. Always
	// renders exactly world.EventsVisible lines (padded with blanks
	// when the buffer hasn't filled yet).
	lines = append(lines, "")
	lines = append(lines, trustMatrixTitleStyle.Render("Events"))
	visible := w.VisibleEvents()
	pad := world.EventsVisible - len(visible)
	for i := 0; i < pad; i++ {
		lines = append(lines, "")
	}
	for _, e := range visible {
		style, ok := eventStyleByColor[e.Color]
		if ok {
			lines = append(lines, style.Render(e.Message))
		} else {
			lines = append(lines, e.Message)
		}
	}
	return lines
}

// legendCell renders one "glyph value" pair for the legend, with
// the colored heat block followed by the 0-15 trust index in
// 2-char-aligned form so columns line up.
func legendCell(idx int) string {
	return fmt.Sprintf("\x1b[38;5;%dm█\x1b[0m %2d", trustHeatFG[idx], idx)
}
