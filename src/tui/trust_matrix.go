// trust_matrix.go — renders the per-agent trust grid that appears to
// the right of the maze. Rows = trusting agent; columns = trustee
// candidate. Each cell shows a 16-level heat color encoding
// TrustScores[row][col], or a white '-' when no data has been
// recorded yet. The diagonal renders a dim '·' (self).
package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"maze-of-wumpus/src/world"
)

// ansiReset closes a 256-color FG/BG escape sequence. Used as a
// suffix on every precomputed colored cell.
const ansiReset = "\x1b[0m"

// trustCellByIdx[i] is the colored '█' block for heat index i, fully
// wrapped in ANSI escape codes. Built once at init() so trustCell()
// is a slice lookup instead of a per-call fmt.Sprintf. With ~228
// trust-cell calls per render this is the biggest single TUI alloc
// reduction in the View hot path.
var trustCellByIdx [16]string

// trustDiagCell and trustEmptyCell render the two non-heat variants
// of a matrix cell (self-diagonal dim '·' and "no data" white '-').
// Both are immutable so they live as package-level constants.
const trustDiagCell = "\x1b[38;5;240m·" + ansiReset
const trustEmptyCell = "\x1b[38;5;255m-" + ansiReset

// legendCellByIdx[i] is the precomputed "█ <2-digit idx>" cell shown
// in the heat legend column to the right of the Agent-Agent matrix.
var legendCellByIdx [16]string

// strategyPerfBgPrefix[i] is the ANSI "bg + bright-white fg" opening
// escape sequence for strategy-performance heat index i. Built once;
// strategyPerfCell concatenates this with a manually-formatted int
// and ansiReset to render a single perf cell.
var strategyPerfBgPrefix [16]string

func init() {
	for i, fg := range trustHeatFG {
		trustCellByIdx[i] = fmt.Sprintf("\x1b[38;5;%dm█%s", fg, ansiReset)
		legendCellByIdx[i] = fmt.Sprintf("\x1b[38;5;%dm█%s %2d", fg, ansiReset, i)
	}
	for i, bg := range strategyPerfHeatBG {
		strategyPerfBgPrefix[i] = fmt.Sprintf("\x1b[48;5;%dm\x1b[38;5;255m", bg)
	}
}

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
// background in the palette. Uses a precomputed bg-prefix table and
// manual int formatting to avoid per-cell fmt.Sprintf allocations.
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
	s := strconv.Itoa(value)
	pad := width - len(s)
	if pad < 0 {
		pad = 0
	}
	var b strings.Builder
	b.Grow(len(strategyPerfBgPrefix[idx]) + pad + len(s) + len(ansiReset))
	b.WriteString(strategyPerfBgPrefix[idx])
	for i := 0; i < pad; i++ {
		b.WriteByte(' ')
	}
	b.WriteString(s)
	b.WriteString(ansiReset)
	return b.String()
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
// Lookup-only — the colored variants are precomputed in
// trustCellByIdx at init().
func trustCell(score float64, present bool, isDiag bool) string {
	if isDiag {
		return trustDiagCell
	}
	if !present {
		return trustEmptyCell
	}
	return trustCellByIdx[trustColorIdx(score)]
}

// renderTrustMatrixLines builds the right-pane panels as a slice of
// pre-rendered text lines (one per row): the Agent-Algorithm Trust
// matrix (with the heat legend), Strategy Performance, the Agent
// Strategies legend, and the Events feed. The caller splices these next
// to the maze grid in View.
func renderTrustMatrixLines(w *world.World) []string {
	lines := make([]string, 0, len(w.Agents)+2+len(trustHeatFG)/2+1)
	// Agent-Algorithm Trust matrix. Rows are agents; columns are
	// strategy letters (R/S/T/U/V). Cells encode StrategyTrustScores,
	// heat-colored on the 0..15 scale shown by the legend spliced into
	// the right edge of the first rows.
	//
	// (The former "Agent-Agent Trust" matrix was removed: agents no
	// longer follow one another, so per-agent trust has nothing to
	// display.)
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
	// One row per agent. The 16-step heat legend (8 entries × 2 columns
	// = 0..15) is spliced onto the right edge of the first 8 rows so it
	// sits next to the matrix instead of below it.
	half := len(trustHeatFG) / 2 // 8
	for i, row := range w.Agents {
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
		if i < half {
			b.WriteString("  ")
			b.WriteString(legendCell(i))
			b.WriteString("  ")
			b.WriteString(legendCell(i + half))
		}
		lines = append(lines, b.String())
	}
	// When the roster has fewer rows than the legend needs (half = 8),
	// emit the leftover legend entries on their own lines so the full
	// 0..15 heat scale always renders, left-padded to clear the matrix.
	leftPad := 2 + 2*len(algoLetters)
	for i := len(w.Agents); i < half && i < len(legendCellByIdx); i++ {
		var b strings.Builder
		for j := 0; j < leftPad; j++ {
			b.WriteByte(' ')
		}
		b.WriteString("  ")
		b.WriteString(legendCell(i))
		b.WriteString("  ")
		b.WriteString(legendCell(i + half))
		lines = append(lines, b.String())
	}
	// Strategy Performance: per-letter run-end tallies (TTL deaths,
	// wins, total runs). Sits between the Agent-Algorithm matrix and
	// the Agent Strategies legend. Each numeric column is independently
	// normalized to a 0..15 heat scale so the dominant strategy in each
	// column glows red.
	lines = append(lines, "")
	lines = append(lines, trustMatrixTitleStyle.Render("Strategy Performance"))
	lines = append(lines, "    Die.TTL  Wins  #Runs")
	ttlMax, winsMax, totalMax := 0, 0, 0
	for _, l := range algoLetters {
		c := w.StrategyPerf[l]
		if c == nil {
			continue
		}
		if c.TTLExpiry > ttlMax {
			ttlMax = c.TTLExpiry
		}
		if c.Wins > winsMax {
			winsMax = c.Wins
		}
		// #Runs is the count of all runs STARTED on this strategy,
		// regardless of outcome (TTL death, hazard death, goal
		// reach). Bumped at RespawnAgents.
		if c.Started > totalMax {
			totalMax = c.Started
		}
	}
	for _, l := range algoLetters {
		c := w.StrategyPerf[l]
		if c == nil {
			c = &world.StrategyPerfCounts{}
		}
		lines = append(lines, fmt.Sprintf(" %c  %s  %s  %s",
			l,
			strategyPerfCell(c.TTLExpiry, 7, ttlMax),
			strategyPerfCell(c.Wins, 4, winsMax),
			strategyPerfCell(c.Started, 5, totalMax)))
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

// legendSpillRows returns how many heat-legend entries must spill
// onto their own lines below the agent rows — i.e. the shortfall
// between the roster size and the legend's half-height (8). Returns 0
// when the roster is at least 8 rows tall.
func legendSpillRows(numAgents int) int {
	half := len(trustHeatFG) / 2
	if numAgents >= half {
		return 0
	}
	return half - numAgents
}

// legendCell renders one "glyph value" pair for the legend, with
// the colored heat block followed by the 0-15 trust index in
// 2-char-aligned form so columns line up. Lookup-only; the strings
// are precomputed in legendCellByIdx at init().
func legendCell(idx int) string {
	return legendCellByIdx[idx]
}
