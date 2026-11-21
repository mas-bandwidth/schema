package roadmap

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// The roadmap as render.lisp sees it: docs/roadmap.sexp with every overlay
// under docs/roadmap-evidence/ applied in name order, and each cell ✅ exactly
// when every leaf task under its work node is :done (render.lisp
// evaluate-cell, assessed-cell-text). It reads the data with the package's own
// sexp reader (doneevidence.go), so there is one parser in Go.

type truthRow struct {
	id    string
	title string
}

type truthRoadmap struct {
	id      string
	title   string
	rows    []truthRow
	columns []string
	cells   map[[2]string]string // (row, column) -> work node id
}

type truthGraph struct {
	nodes    map[string][]any // id -> the node's plist, overlays applied
	roadmaps []*truthRoadmap
}

func truthPlistPut(plist []any, key string, value any) []any {
	out := append([]any(nil), plist...)
	for i := 0; i+1 < len(out); i += 2 {
		if sym, ok := out[i].(doneSexpSymbol); ok && string(sym) == key {
			out[i+1] = value
			return out
		}
	}
	return append(out, doneSexpSymbol(key), value)
}

func loadTruthGraph(root string) (*truthGraph, error) {
	raw, err := os.ReadFile(filepath.Join(root, "docs", "roadmap.sexp"))
	if err != nil {
		return nil, err
	}
	forms, err := readDoneSexp(string(raw))
	if err != nil {
		return nil, err
	}
	if len(forms) != 1 {
		return nil, fmt.Errorf("docs/roadmap.sexp carries %d top-level forms; want 1", len(forms))
	}
	rootForm, ok := forms[0].([]any)
	if !ok {
		return nil, fmt.Errorf("docs/roadmap.sexp root is not a plist")
	}
	nodesValue, ok := doneSexpGet(rootForm, ":nodes")
	if !ok {
		return nil, fmt.Errorf("docs/roadmap.sexp carries no :nodes")
	}
	nodeForms, ok := nodesValue.([]any)
	if !ok {
		return nil, fmt.Errorf("docs/roadmap.sexp :nodes is not a list")
	}
	g := &truthGraph{nodes: map[string][]any{}}
	for _, form := range nodeForms {
		plist, ok := form.([]any)
		if !ok {
			continue
		}
		id, ok := doneSexpGet(plist, ":id")
		if !ok {
			continue
		}
		g.nodes[doneSexpString(id)] = plist
	}
	// The overlay, as render.lisp apply-evidence-overlay applies it: :state
	// always replaces; :evidence, :note and :tested-revision replace when the
	// entry carries a non-empty one.
	overlays, err := filepath.Glob(filepath.Join(root, "docs", "roadmap-evidence", "*.sexp"))
	if err != nil {
		return nil, err
	}
	sort.Strings(overlays)
	for _, path := range overlays {
		if strings.HasPrefix(filepath.Base(path), ".") {
			continue
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		forms, err := readDoneSexp(string(raw))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		for _, form := range forms {
			entry, ok := form.([]any)
			if !ok {
				continue
			}
			id := doneSexpString(func() any { v, _ := doneSexpGet(entry, ":id"); return v }())
			node, ok := g.nodes[id]
			if !ok {
				return nil, fmt.Errorf("%s: overlay id %q is not a node of the roadmap", path, id)
			}
			state, ok := doneSexpGet(entry, ":state")
			if !ok {
				return nil, fmt.Errorf("%s: overlay entry %q carries no :state", path, id)
			}
			node = truthPlistPut(node, ":state", state)
			if ev, ok := doneSexpGet(entry, ":evidence"); ok {
				if list, isList := ev.([]any); isList && len(list) > 0 {
					node = truthPlistPut(node, ":evidence", ev)
				}
			}
			g.nodes[id] = node
		}
	}
	page, ok := doneSexpGet(rootForm, ":page")
	if !ok {
		return nil, fmt.Errorf("docs/roadmap.sexp carries no :page")
	}
	pageList, _ := page.([]any)
	ids, _ := doneSexpGet(pageList, ":roadmaps")
	for _, id := range doneSexpStrings(ids) {
		node, ok := g.nodes[id]
		if !ok {
			return nil, fmt.Errorf("page roadmap %q is not a node", id)
		}
		rm := &truthRoadmap{id: id, cells: map[[2]string]string{}}
		if v, ok := doneSexpGet(node, ":title"); ok {
			rm.title = doneSexpString(v)
		}
		rowsV, _ := doneSexpGet(node, ":rows")
		rows, _ := rowsV.([]any)
		for _, r := range rows {
			pair, _ := r.([]any)
			if len(pair) < 2 {
				return nil, fmt.Errorf("roadmap %s: malformed row %v", id, r)
			}
			rm.rows = append(rm.rows, truthRow{id: doneSexpString(pair[0]), title: doneSexpString(pair[1])})
		}
		colsV, _ := doneSexpGet(node, ":columns")
		cols, _ := colsV.([]any)
		for _, c := range cols {
			pair, _ := c.([]any)
			if len(pair) < 1 {
				return nil, fmt.Errorf("roadmap %s: malformed column %v", id, c)
			}
			rm.columns = append(rm.columns, doneSexpString(pair[0]))
		}
		cellsV, _ := doneSexpGet(node, ":cells")
		cells, _ := cellsV.([]any)
		for _, c := range cells {
			triple, _ := c.([]any)
			if len(triple) < 3 {
				return nil, fmt.Errorf("roadmap %s: malformed cell %v", id, c)
			}
			rm.cells[[2]string{doneSexpString(triple[0]), doneSexpString(triple[1])}] = doneSexpString(triple[2])
		}
		g.roadmaps = append(g.roadmaps, rm)
	}
	return g, nil
}

// leaves is every task under a work node (render.lisp collect-work-leaves).
func (g *truthGraph) leaves(id string, visiting map[string]bool) ([]string, error) {
	if visiting[id] {
		return nil, fmt.Errorf("cycle through %q", id)
	}
	node, ok := g.nodes[id]
	if !ok {
		return nil, fmt.Errorf("dangling work node %q", id)
	}
	typ := doneSexpString(func() any { v, _ := doneSexpGet(node, ":type"); return v }())
	switch typ {
	case ":task":
		return []string{id}, nil
	case ":work-set":
		visiting[id] = true
		defer delete(visiting, id)
		ch, _ := doneSexpGet(node, ":children")
		seen := map[string]bool{}
		var out []string
		for _, c := range doneSexpStrings(ch) {
			ls, err := g.leaves(c, visiting)
			if err != nil {
				return nil, err
			}
			for _, l := range ls {
				if !seen[l] {
					seen[l] = true
					out = append(out, l)
				}
			}
		}
		return out, nil
	case ":roadmap":
		visiting[id] = true
		defer delete(visiting, id)
		cv, _ := doneSexpGet(node, ":cells")
		cells, _ := cv.([]any)
		seen := map[string]bool{}
		var out []string
		for _, c := range cells {
			triple, _ := c.([]any)
			if len(triple) < 3 {
				continue
			}
			ls, err := g.leaves(doneSexpString(triple[2]), visiting)
			if err != nil {
				return nil, err
			}
			for _, l := range ls {
				if !seen[l] {
					seen[l] = true
					out = append(out, l)
				}
			}
		}
		return out, nil
	}
	return nil, fmt.Errorf("work node %q has unsupported type %q", id, typ)
}

// ticked reports whether the cell renders ✅: every leaf :done.
func (g *truthGraph) ticked(rm *truthRoadmap, row, col string) (bool, error) {
	work, ok := rm.cells[[2]string{row, col}]
	if !ok {
		return false, fmt.Errorf("roadmap %s has no cell (%s, %s)", rm.id, row, col)
	}
	leaves, err := g.leaves(work, map[string]bool{})
	if err != nil {
		return false, err
	}
	if len(leaves) == 0 {
		return false, fmt.Errorf("cell (%s, %s) of %s has no leaf tasks", row, col, rm.id)
	}
	for _, l := range leaves {
		st, _ := doneSexpGet(g.nodes[l], ":state")
		if doneSexpString(st) != ":done" {
			return false, nil
		}
	}
	return true, nil
}
