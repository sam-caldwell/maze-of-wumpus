package strategy

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// TestPO_NoOptimalPathLeak is a partial-observability regression guard.
//
// The agent field ShortestPath and the world field ShortestPathCells
// both encode the FULL optimal entrance→goal route — the "answer key"
// to a maze the agent has not perceived. They exist only for scoring
// (on-path vs off-path tally) and the TUI overlay. No strategy
// decision path may read them: doing so would let an agent navigate by
// the optimal route it never sensed, violating strict partial
// observability.
//
// KnownShortestPath is a DIFFERENT, legitimate field — a clone's seed
// path built from cells the swarm has actually perceived — and is
// intentionally allowed (its selector name differs, so it never trips
// this guard).
//
// The check walks the AST of every non-test file in this package and
// fails on any selector ending in ShortestPath/ShortestPathCells, so
// it ignores comments and string literals and survives renames of the
// receiver variable.
func TestPO_NoOptimalPathLeak(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	fset := token.NewFileSet()
	scanned := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, perr := parser.ParseFile(fset, name, nil, 0)
		if perr != nil {
			t.Fatalf("parse %s: %v", name, perr)
		}
		scanned++
		ast.Inspect(f, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			switch sel.Sel.Name {
			case "ShortestPath", "ShortestPathCells":
				pos := fset.Position(sel.Sel.Pos())
				t.Errorf("partial-observability violation: %s reads .%s (the full optimal "+
					"entrance→goal route) at %s — strategies must navigate only by perceived "+
					"KnownCells, never the answer-key ShortestPath", name, sel.Sel.Name, pos)
			}
			return true
		})
	}
	if scanned == 0 {
		t.Fatal("PO guard scanned no source files — did the package layout change?")
	}
}
