package ci

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// pythonRoadmapCellCounts is how many python cells each table carries: one
// per row of the table.
var pythonRoadmapCellCounts = map[string]int{
	"packet-wire":     27,
	"fixed-tables":    56,
	"variable-tables": 32,
}

// pythonLayerTaskRx names the layer tasks P4 to P16 that once held whole
// tables of python cells.
var pythonLayerTaskRx = regexp.MustCompile(`^port/python/p([4-9]|1[0-6])-`)

// TestPythonCellsEachNameTheirOwnTask holds the python column of the three
// roadmap tables to one node per row: every python cell names a node under
// the port/python work-set, no two cells share one, and no cell names a P4 to
// P16 layer task. A card then moves its own rows without waiting on a layer.
func TestPythonCellsEachNameTheirOwnTask(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), "docs", "roadmap.sexp"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if err := checkPythonRoadmapCells(text); err != nil {
		t.Fatal(err)
	}

	// Negative controls: a fault planted in a copy of the real data goes red
	// by name.
	for _, control := range []struct {
		name, old, new, want string
	}{
		{
			name: "layer task",
			old:  `("ranged-integers" "python" "port/python/packet/ranged-integers")`,
			new:  `("ranged-integers" "python" "port/python/p4-packet")`,
			want: "names layer task",
		},
		{
			name: "shared node",
			old:  `("bits" "python" "port/python/packet/bits")`,
			new:  `("bits" "python" "port/python/packet/ranged-integers")`,
			want: "shared by",
		},
		{
			name: "outside port/python",
			old:  `("maps" "python" "port/python/vt/maps")`,
			new:  `("maps" "python" "port/squirrel/vt/maps")`,
			want: "not under port/python",
		},
	} {
		if !strings.Contains(text, control.old) {
			t.Fatalf("negative control %s: docs/roadmap.sexp no longer holds %s", control.name, control.old)
		}
		planted := strings.Replace(text, control.old, control.new, 1)
		err := checkPythonRoadmapCells(planted)
		if err == nil || !strings.Contains(err.Error(), control.want) {
			t.Fatalf("negative control %s: want an error naming %q, got %v", control.name, control.want, err)
		}
	}
}

func checkPythonRoadmapCells(text string) error {
	forms, err := pythonRoadmapRead(text)
	if err != nil {
		return err
	}
	if len(forms) != 1 {
		return fmt.Errorf("docs/roadmap.sexp holds %d top-level forms, want 1", len(forms))
	}
	root, ok := forms[0].([]any)
	if !ok {
		return fmt.Errorf("docs/roadmap.sexp: the top-level form is not a list")
	}
	nodeList, ok := pythonRoadmapGet(root, ":nodes").([]any)
	if !ok {
		return fmt.Errorf("docs/roadmap.sexp: no :nodes list")
	}
	nodes := map[string][]any{}
	for _, n := range nodeList {
		plist, ok := n.([]any)
		if !ok {
			continue
		}
		if id, ok := pythonRoadmapGet(plist, ":id").(string); ok {
			nodes[id] = plist
		}
	}

	under := map[string]bool{}
	var walk func(id string)
	walk = func(id string) {
		if under[id] {
			return
		}
		under[id] = true
		children, _ := pythonRoadmapGet(nodes[id], ":children").([]any)
		for _, c := range children {
			if s, ok := c.(string); ok {
				walk(s)
			}
		}
	}
	if _, ok := nodes["port/python"]; !ok {
		return fmt.Errorf("docs/roadmap.sexp has no port/python node")
	}
	walk("port/python")

	var problems []string
	owner := map[string]string{}
	for _, table := range []string{"packet-wire", "fixed-tables", "variable-tables"} {
		node, ok := nodes[table]
		if !ok {
			problems = append(problems, fmt.Sprintf("table %s is missing", table))
			continue
		}
		cells, _ := pythonRoadmapGet(node, ":cells").([]any)
		count := 0
		for _, c := range cells {
			cell, ok := c.([]any)
			if !ok || len(cell) < 3 {
				continue
			}
			row, _ := cell[0].(string)
			col, _ := cell[1].(string)
			work, _ := cell[2].(string)
			if col != "python" {
				continue
			}
			count++
			where := table + "/" + row
			switch {
			case pythonLayerTaskRx.MatchString(work):
				problems = append(problems, fmt.Sprintf("python cell %s names layer task %s", where, work))
			case nodes[work] == nil:
				problems = append(problems, fmt.Sprintf("python cell %s names %s, which is not a node", where, work))
			case !under[work] || work == "port/python":
				problems = append(problems, fmt.Sprintf("python cell %s names %s, not under port/python", where, work))
			}
			if prev, dup := owner[work]; dup {
				problems = append(problems, fmt.Sprintf("python cell %s names %s, shared by %s", where, work, prev))
			} else {
				owner[work] = where
			}
		}
		if want := pythonRoadmapCellCounts[table]; count != want {
			problems = append(problems, fmt.Sprintf("table %s has %d python cells, want %d", table, count, want))
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("%d python cell problems:\n%s", len(problems), strings.Join(problems, "\n"))
	}
	return nil
}

func pythonRoadmapGet(plist []any, key string) any {
	for i := 0; i+1 < len(plist); i++ {
		if k, ok := plist[i].(pythonRoadmapSymbol); ok && string(k) == key {
			return plist[i+1]
		}
	}
	return nil
}

// pythonRoadmapSymbol is a bare atom (a keyword, a number or nil); a string
// atom reads as a Go string, a list as []any.
type pythonRoadmapSymbol string

func pythonRoadmapRead(text string) ([]any, error) {
	var stack [][]any
	var top []any
	push := func(v any) {
		if len(stack) == 0 {
			top = append(top, v)
			return
		}
		stack[len(stack)-1] = append(stack[len(stack)-1], v)
	}
	for i := 0; i < len(text); {
		ch := text[i]
		switch {
		case ch == ';':
			for i < len(text) && text[i] != '\n' {
				i++
			}
		case ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r':
			i++
		case ch == '(':
			stack = append(stack, []any{})
			i++
		case ch == ')':
			if len(stack) == 0 {
				return nil, fmt.Errorf("unbalanced ) at byte %d", i)
			}
			list := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			push(list)
			i++
		case ch == '"':
			var b strings.Builder
			i++
			for {
				if i >= len(text) {
					return nil, fmt.Errorf("unterminated string")
				}
				c := text[i]
				if c == '\\' && i+1 < len(text) {
					b.WriteByte(text[i+1])
					i += 2
					continue
				}
				i++
				if c == '"' {
					break
				}
				b.WriteByte(c)
			}
			push(b.String())
		default:
			start := i
			for i < len(text) && !strings.ContainsRune(" \t\r\n()\";", rune(text[i])) {
				i++
			}
			push(pythonRoadmapSymbol(text[start:i]))
		}
	}
	if len(stack) != 0 {
		return nil, fmt.Errorf("%d unclosed lists", len(stack))
	}
	return top, nil
}
