// decisionlog.go — a rolling trace of agent/clone navigation decisions
// for the TUI's toggleable decision viewport. It shows, tick by tick,
// what each active agent and clone decided and why, so a viewer can
// watch how the strategies make their choices as they navigate.
//
// Logging is gated on DecisionLogEnabled (the UI sets it when the
// viewport is open), so it adds no cost during normal play.
package world

// DecisionLogMax caps the stored trace. The viewport renders the most
// recent screen-height entries; the rest is recent history.
const DecisionLogMax = 2000

// SetDecisionLogEnabled turns decision tracing on or off. When turned
// off the existing trace is cleared so the next open starts fresh.
func (w *World) SetDecisionLogEnabled(on bool) {
	w.DecisionLogEnabled = on
	if !on {
		w.DecisionLog = nil
	}
}

// LogDecision appends one decision entry, clamped to DecisionLogMax.
// No-op unless tracing is enabled.
func (w *World) LogDecision(entry string) {
	if !w.DecisionLogEnabled {
		return
	}
	w.DecisionLog = append(w.DecisionLog, entry)
	if len(w.DecisionLog) > DecisionLogMax {
		w.DecisionLog = w.DecisionLog[len(w.DecisionLog)-DecisionLogMax:]
	}
}

// VisibleDecisions returns the last n trace entries (or fewer if the
// trace is shorter), oldest-first so the viewport reads top-to-bottom
// with the newest at the bottom.
func (w *World) VisibleDecisions(n int) []string {
	if n <= 0 || len(w.DecisionLog) == 0 {
		return nil
	}
	if len(w.DecisionLog) <= n {
		return w.DecisionLog
	}
	return w.DecisionLog[len(w.DecisionLog)-n:]
}
