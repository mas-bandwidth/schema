package roadmap

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type doneSexpSymbol string

type doneNode struct {
	id       string
	typ      string
	state    string
	evidence []string
	children []string
	cells    []string
}

type doneSexpReader struct {
	src string
	pos int
}

func (r *doneSexpReader) skipSpace() {
	for r.pos < len(r.src) {
		switch r.src[r.pos] {
		case ' ', '\t', '\r', '\n':
			r.pos++
		case ';':
			for r.pos < len(r.src) && r.src[r.pos] != '\n' {
				r.pos++
			}
		default:
			return
		}
	}
}

func (r *doneSexpReader) readValue() (any, error) {
	r.skipSpace()
	if r.pos >= len(r.src) {
		return nil, fmt.Errorf("unexpected end of sexp")
	}
	switch r.src[r.pos] {
	case '(':
		r.pos++
		var list []any
		for {
			r.skipSpace()
			if r.pos >= len(r.src) {
				return nil, fmt.Errorf("unterminated list")
			}
			if r.src[r.pos] == ')' {
				r.pos++
				return list, nil
			}
			v, err := r.readValue()
			if err != nil {
				return nil, err
			}
			list = append(list, v)
		}
	case ')':
		return nil, fmt.Errorf("unexpected )")
	case '"':
		r.pos++
		var b strings.Builder
		for r.pos < len(r.src) {
			ch := r.src[r.pos]
			if ch == '\\' {
				r.pos++
				if r.pos >= len(r.src) {
					return nil, fmt.Errorf("unterminated string escape")
				}
				b.WriteByte(r.src[r.pos])
				r.pos++
				continue
			}
			r.pos++
			if ch == '"' {
				return b.String(), nil
			}
			b.WriteByte(ch)
		}
		return nil, fmt.Errorf("unterminated string")
	default:
		start := r.pos
		for r.pos < len(r.src) {
			ch := r.src[r.pos]
			if ch == ' ' || ch == '\t' || ch == '\r' || ch == '\n' || ch == '(' || ch == ')' || ch == ';' || ch == '"' {
				break
			}
			r.pos++
		}
		return doneSexpSymbol(r.src[start:r.pos]), nil
	}
}

func readDoneSexp(src string) ([]any, error) {
	r := &doneSexpReader{src: src}
	var forms []any
	for {
		r.skipSpace()
		if r.pos >= len(r.src) {
			return forms, nil
		}
		v, err := r.readValue()
		if err != nil {
			return nil, err
		}
		forms = append(forms, v)
	}
}

func doneSexpGet(list []any, key string) (any, bool) {
	for i := 0; i+1 < len(list); i += 2 {
		if sym, ok := list[i].(doneSexpSymbol); ok && string(sym) == key {
			return list[i+1], true
		}
	}
	return nil, false
}

func doneSexpString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case doneSexpSymbol:
		return string(x)
	default:
		return ""
	}
}

func doneSexpStrings(v any) []string {
	list, ok := v.([]any)
	if !ok {
		return nil
	}
	var out []string
	for _, item := range list {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func doneNodeFromForm(form []any) (*doneNode, bool) {
	id, ok := doneSexpGet(form, ":id")
	if !ok {
		return nil, false
	}
	n := &doneNode{id: doneSexpString(id)}
	if v, ok := doneSexpGet(form, ":type"); ok {
		n.typ = doneSexpString(v)
	}
	if v, ok := doneSexpGet(form, ":state"); ok {
		n.state = doneSexpString(v)
	}
	if v, ok := doneSexpGet(form, ":evidence"); ok {
		n.evidence = doneSexpStrings(v)
	}
	if v, ok := doneSexpGet(form, ":children"); ok {
		n.children = doneSexpStrings(v)
	}
	if v, ok := doneSexpGet(form, ":cells"); ok {
		if cells, ok := v.([]any); ok {
			for _, cell := range cells {
				parts, ok := cell.([]any)
				if !ok || len(parts) < 3 {
					continue
				}
				if target, ok := parts[2].(string); ok {
					n.cells = append(n.cells, target)
				}
			}
		}
	}
	return n, true
}

func loadDoneNodes(root string) (map[string]*doneNode, []string, error) {
	raw, err := os.ReadFile(filepath.Join(root, "docs", "roadmap.sexp"))
	if err != nil {
		return nil, nil, err
	}
	forms, err := readDoneSexp(string(raw))
	if err != nil {
		return nil, nil, err
	}
	if len(forms) != 1 {
		return nil, nil, fmt.Errorf("docs/roadmap.sexp carries %d top-level forms; want 1", len(forms))
	}
	rootForm, ok := forms[0].([]any)
	if !ok {
		return nil, nil, fmt.Errorf("docs/roadmap.sexp root is not a plist")
	}
	nodesValue, ok := doneSexpGet(rootForm, ":nodes")
	if !ok {
		return nil, nil, fmt.Errorf("docs/roadmap.sexp carries no :nodes")
	}
	nodeForms, ok := nodesValue.([]any)
	if !ok {
		return nil, nil, fmt.Errorf("docs/roadmap.sexp :nodes is not a list")
	}
	nodes := make(map[string]*doneNode)
	var order []string
	for _, form := range nodeForms {
		plist, ok := form.([]any)
		if !ok {
			continue
		}
		n, ok := doneNodeFromForm(plist)
		if !ok {
			continue
		}
		nodes[n.id] = n
		order = append(order, n.id)
	}
	overlays, err := filepath.Glob(filepath.Join(root, "docs", "roadmap-evidence", "*.sexp"))
	if err != nil {
		return nil, nil, err
	}
	sort.Strings(overlays)
	for _, path := range overlays {
		if strings.HasPrefix(filepath.Base(path), ".") {
			continue
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, nil, err
		}
		forms, err := readDoneSexp(string(raw))
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", path, err)
		}
		for _, form := range forms {
			plist, ok := form.([]any)
			if !ok {
				continue
			}
			n, ok := doneNodeFromForm(plist)
			if !ok {
				continue
			}
			node := nodes[n.id]
			if node == nil {
				continue
			}
			if _, ok := doneSexpGet(plist, ":state"); ok {
				node.state = n.state
			}
			if _, ok := doneSexpGet(plist, ":evidence"); ok {
				node.evidence = n.evidence
			}
		}
	}
	return nodes, order, nil
}

func doneLeafIDs(nodes map[string]*doneNode, order []string) map[string]bool {
	leaves := make(map[string]bool)
	var collect func(string, map[string]bool) []string
	collect = func(id string, visiting map[string]bool) []string {
		if visiting[id] {
			return nil
		}
		node := nodes[id]
		if node == nil {
			return nil
		}
		switch node.typ {
		case ":task":
			return []string{id}
		case ":work-set":
			visiting[id] = true
			var out []string
			for _, child := range node.children {
				out = append(out, collect(child, visiting)...)
			}
			delete(visiting, id)
			return out
		case ":roadmap":
			visiting[id] = true
			var out []string
			for _, cell := range node.cells {
				out = append(out, collect(cell, visiting)...)
			}
			delete(visiting, id)
			return out
		}
		return nil
	}
	for _, id := range order {
		if nodes[id].typ != ":roadmap" {
			continue
		}
		for _, leaf := range collect(id, map[string]bool{}) {
			leaves[leaf] = true
		}
	}
	return leaves
}

var doneEvidencePathRx = regexp.MustCompile(`(?:\A|[^A-Za-z0-9_./-])((?:\.github|internal|compiler|test|testdata|tools|cmd|docs|examples128|examples|tables|bench|generated|make|third_party|ir|working|images)/[A-Za-z0-9_./%*+-]+)`)

var doneEvidenceTestRx = regexp.MustCompile(`\bTest[A-Za-z0-9_]+`)

var doneEvidenceGoTestRx = regexp.MustCompile(`(?m)^func\s+(Test[A-Za-z0-9_]+)\s*\(`)

type doneEvidencePath struct {
	start int
	end   int
	path  string
}

func doneEvidenceStripPath(path string) string {
	return strings.TrimRight(path, ".,;:")
}

func doneEvidenceIsBuildOutput(path string) bool {
	return strings.HasPrefix(path, "build/") || strings.Contains(path, "/build/") || strings.HasSuffix(path, ".stamp")
}

func doneEvidenceGoTests(root string) (map[string]bool, error) {
	defined := make(map[string]bool)
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "build", "dist", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, m := range doneEvidenceGoTestRx.FindAllStringSubmatch(string(src), -1) {
			defined[m[1]] = true
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return defined, nil
}

func doneEvidenceContainsIdentifier(src, name string) bool {
	for start := 0; ; {
		i := strings.Index(src[start:], name)
		if i < 0 {
			return false
		}
		i += start
		beforeOK := i == 0 || !doneEvidenceIdentByte(src[i-1])
		after := i + len(name)
		afterOK := after >= len(src) || !doneEvidenceIdentByte(src[after])
		if beforeOK && afterOK {
			return true
		}
		start = i + len(name)
	}
}

func doneEvidenceIdentByte(b byte) bool {
	return b == '_' || b >= '0' && b <= '9' || b >= 'A' && b <= 'Z' || b >= 'a' && b <= 'z'
}

func checkDoneEvidence(root string, nodes map[string]*doneNode, order []string) error {
	goTests, err := doneEvidenceGoTests(root)
	if err != nil {
		return err
	}
	leaves := doneLeafIDs(nodes, order)
	var problems []string
	for _, id := range order {
		if !leaves[id] {
			continue
		}
		node := nodes[id]
		if node.typ != ":task" || node.state != ":done" {
			continue
		}
		for _, evidence := range node.evidence {
			var paths []doneEvidencePath
			for _, loc := range doneEvidencePathRx.FindAllStringSubmatchIndex(evidence, -1) {
				path := doneEvidenceStripPath(evidence[loc[2]:loc[3]])
				if doneEvidenceIsBuildOutput(path) {
					continue
				}
				paths = append(paths, doneEvidencePath{start: loc[2], end: loc[3], path: path})
				if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(path))); err != nil {
					problems = append(problems, fmt.Sprintf("%s: cited path %s does not exist", id, path))
				}
			}
			for _, loc := range doneEvidenceTestRx.FindAllStringIndex(evidence, -1) {
				name := evidence[loc[0]:loc[1]]
				if name == "Tests" {
					continue
				}
				insidePath := false
				for _, path := range paths {
					if loc[0] >= path.start && loc[0] < path.end {
						insidePath = true
						break
					}
				}
				if insidePath {
					continue
				}
				var cited string
				for _, path := range paths {
					if path.start < loc[0] {
						cited = path.path
					}
				}
				if cited == "" {
					continue
				}
				full := filepath.Join(root, filepath.FromSlash(cited))
				info, err := os.Stat(full)
				if err != nil {
					continue
				}
				switch {
				case strings.HasSuffix(cited, ".go"), info.IsDir():
					if !goTests[name] {
						problems = append(problems, fmt.Sprintf("%s: cited Go test %s is not defined in a _test.go file", id, name))
					}
				default:
					src, err := os.ReadFile(full)
					if err != nil {
						return err
					}
					if !doneEvidenceContainsIdentifier(string(src), name) {
						problems = append(problems, fmt.Sprintf("%s: cited non-Go test %s is not in %s", id, name, cited))
					}
				}
			}
		}
	}
	if len(problems) == 0 {
		return nil
	}
	sort.Strings(problems)
	return fmt.Errorf("%s", strings.Join(problems, "\n"))
}
