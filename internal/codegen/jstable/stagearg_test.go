package jstable

// TestFixedDecodeCallsMatchDeclarations holds every <T>FixedDecode call in the
// generated *Table.js modules to its declaration's parameter count. The decode
// declares `report` only where its body reads it (emitDecodeBody), so a load
// that handed `stage` to a three-parameter decode passed an argument nobody
// takes, which the checkJs gate names TS2554. Pure Go over the emitted text of
// the corpus units: no node, no tsc, no time, no files written.

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// stageargCorpus is every corpus unit that lays out fixed tables.
var stageargCorpus = []string{
	"examples", "tables/examples", "tables/pointers", "tables/blobs", "tables/maps",
	"tables/lists", "tables/block", "tables/blockhome", "tables/messages", "tables/stream",
	"tables/scalars", "tables/vocab", "tables/vocab9", "tables/backend",
	"test/tables/V1.schema", "test/tables/V2.schema", "test/tables/W1.schema", "test/tables/W2.schema",
	"test/tables/R1.schema", "test/tables/R2.schema", "test/tables/P1.schema", "test/tables/P3.schema",
	"test/tables/M1.schema", "test/tables/M2.schema", "test/tables/Scalars2.schema",
}

var (
	stageargDecl = regexp.MustCompile(`function ([A-Za-z_$][\w$]*FixedDecode)\(([^)]*)\)`)
	stageargCall = regexp.MustCompile(`([A-Za-z_$][\w$]*FixedDecode)\(`)
)

// stageargCount answers how many top-level arguments the call whose opening
// parenthesis is at src[open] passes, or -1 when the call does not close.
func stageargCount(src string, open int) int {
	depth, n, empty := 0, 1, true
	for i := open; i < len(src); i++ {
		switch c := src[i]; c {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
			if depth == 0 {
				if empty {
					return 0
				}
				return n
			}
		case ',':
			if depth == 1 {
				n++
			}
		case ' ', '\t', '\n':
		default:
			empty = false
		}
	}
	return -1
}

// stageargFaults lists every FixedDecode call across one unit's modules whose
// argument count differs from its declaration's parameter count.
func stageargFaults(modules map[string]string) (faults []string, calls int) {
	params := map[string]int{}
	for _, src := range modules {
		for _, m := range stageargDecl.FindAllStringSubmatch(src, -1) {
			n := 0
			if strings.TrimSpace(m[2]) != "" {
				n = len(strings.Split(m[2], ","))
			}
			params[m[1]] = n
		}
	}
	names := make([]string, 0, len(modules))
	for name := range modules {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		src := modules[name]
		for _, loc := range stageargCall.FindAllStringSubmatchIndex(src, -1) {
			fn := src[loc[2]:loc[3]]
			if strings.HasSuffix(src[:loc[0]], "function ") {
				continue // the declaration itself
			}
			want, ok := params[fn]
			if !ok {
				continue
			}
			calls++
			got := stageargCount(src, loc[1]-1)
			if got != want {
				line := strings.Count(src[:loc[0]], "\n") + 1
				faults = append(faults, fmt.Sprintf("%s:%d: %s called with %d arguments, declared with %d", name, line, fn, got, want))
			}
		}
	}
	return faults, calls
}

func TestFixedDecodeCallsMatchDeclarations(t *testing.T) {
	t.Parallel()
	const root = "../../.."
	total := 0
	for _, entry := range stageargCorpus {
		var paths []string
		if strings.HasSuffix(entry, ".schema") {
			paths = []string{filepath.Join(root, entry)}
		} else {
			matches, err := filepath.Glob(filepath.Join(root, entry, "*.schema"))
			if err != nil {
				t.Fatal(err)
			}
			if len(matches) == 0 {
				t.Fatalf("corpus unit %s has no .schema files", entry)
			}
			sort.Strings(matches)
			paths = matches
		}
		out, err := Generate(loadUnit(t, paths...))
		if err != nil {
			t.Fatalf("%s: Generate: %v", entry, err)
		}
		modules := map[string]string{}
		for name, data := range out {
			if strings.HasSuffix(name, "Table.js") {
				modules[entry+"/"+name] = string(data)
			}
		}
		faults, calls := stageargFaults(modules)
		total += calls
		for _, f := range faults {
			t.Error(f)
		}
	}
	if total == 0 {
		t.Fatal("no FixedDecode call was found across the corpus; the walk is broken")
	}
}

// TestFixedDecodeCallsReversedWitness holds the check itself: a planted fourth
// argument to a three-parameter decode is named, and a matching call is not.
func TestFixedDecodeCallsReversedWitness(t *testing.T) {
	t.Parallel()
	decl := "export function AFixedDecode(value, view, at) {\n}\n"
	bad := map[string]string{"u/ATable.js": decl, "u/RTable.js": "  AFixedDecode(values[k], imageView, 0, stage);\n"}
	if faults, _ := stageargFaults(bad); len(faults) != 1 {
		t.Fatalf("a four-argument call to a three-parameter decode gave %d faults, want 1: %v", len(faults), faults)
	}
	good := map[string]string{"u/ATable.js": decl, "u/RTable.js": "  AFixedDecode(values[k], f(a, b), at + 8);\n"}
	if faults, calls := stageargFaults(good); len(faults) != 0 || calls != 1 {
		t.Fatalf("a matching call gave faults %v over %d calls, want none over 1", faults, calls)
	}
}
