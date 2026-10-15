// One built-in target, registered from its own init so a target is one file
// and a new one adds a file (docs/CONTRIBUTING.md, "Adding a language").
package compiler

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/gotable"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// surfaceNames is the list of Go surfaces `generate --surface` accepts
// (docs/SPEC-TABLES.md §8.4). It mirrors gotable.surfaceNames.
var surfaceNames = []string{"wire", "fixed", "json", "cook", "block", "view", "message", "retain"}

// parseSurfaces splits a `--surface a,b,c` list, validates each name and
// returns the requested surfaces. A blank input is the "all surfaces" default
// and returns nil.
func parseSurfaces(s string) ([]string, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	valid := map[string]bool{}
	for _, n := range surfaceNames {
		valid[n] = true
	}
	seen := map[string]bool{}
	var out []string
	for _, part := range strings.Split(s, ",") {
		n := strings.TrimSpace(part)
		if n == "" {
			return nil, fmt.Errorf("--surface has an empty entry in %q", s)
		}
		if !valid[n] {
			return nil, fmt.Errorf("unknown surface %q (choose from %s)", n, strings.Join(surfaceNames, ", "))
		}
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	return out, nil
}

// goTarget emits Go.
type goTarget struct{}

func (goTarget) Names() []string { return []string{"go"} }

// Generate is the NO-LINEAGE case: a unit whose lock the caller did not read,
// or that has none. Every fixed table then carries the single entry it can
// always compute — its own (docs/FIXED-FORM-ALGORITHM.md §5.9 #1).
func (t goTarget) Generate(u *ir.Unit, opts Options) (map[string][]byte, error) {
	return t.GenerateLineage(u, opts, nil)
}

// GenerateLineage is the second entry point of §5.9 #1: the driver read the
// unit's lock once (compiler/lineage.go) and hands the lineage down as DATA, so
// gotable opens no file and a build ships a reader for every layout the lock
// records.
func (goTarget) GenerateLineage(u *ir.Unit, opts Options, lineage *FixedLineage) (map[string][]byte, error) {
	// Wide text is carried by both packet and table surfaces.
	if err := refuseWideText(u, "go"); err != nil {
		return nil, err
	}
	if err := refuseUnported(u, "go"); err != nil {
		return nil, err
	}
	files, err := golang.Generate(u)
	if err != nil {
		return nil, err
	}
	// units that declare tables ALSO get <Base>Table.go per file — the
	// TABLE-wire codecs and the reflection descriptors (SPEC-TABLES.md); a
	// table-free unit's output is byte-identical to what the packet emitter
	// alone produces. `--surface <list>` (docs/SPEC-TABLES.md §8.4) narrows
	// that to the files and functions the named surfaces need; absent, it is
	// the default all-surfaces emit, byte-identical to before.
	tableLineage := goTableLineage(u, lineage)
	var tables map[string][]byte
	if surf, ok := opts["surface"]; ok {
		names, perr := parseSurfaces(surf)
		if perr != nil {
			return nil, perr
		}
		tables, err = gotable.GenerateSurface(u, tableLineage, names)
	} else {
		tables, err = gotable.GenerateLineage(u, tableLineage)
	}
	if err != nil {
		return nil, err
	}
	for name, data := range tables {
		if _, dup := files[name]; dup {
			return nil, fmt.Errorf("generated file %s is claimed twice — a schema file named <X>.schema beside <X minus Table>.schema with tables collides on the Table source; rename one file (SPEC-TABLES.md)", name)
		}
		files[name] = data
	}
	return files, nil
}

// goTableLineage is the lock's lineage in the Go backend's own spelling. Nil in,
// nil out: a unit with no lock hands nothing.
func goTableLineage(u *ir.Unit, lineage *FixedLineage) map[string][]gotable.FixedLineageEntry {
	if lineage == nil {
		return nil
	}
	out := map[string][]gotable.FixedLineageEntry{}
	for _, st := range ir.TableFixedRoots(u) {
		entries := lineage.Entries(st.Name)
		if len(entries) == 0 {
			continue
		}
		for _, e := range entries {
			out[st.Name] = append(out[st.Name], gotable.FixedLineageEntry{
				Wire:    e.Wire,
				Layout:  e.Layout,
				Record:  e.Record,
				Retired: e.Retired,
				Reason:  e.Reason,
			})
		}
	}
	return out
}

func init() {
	registerBuiltin(goTarget{}, true, true, true, true)
	registerOptionalArrayCarrier("go")
	registerListCarrier("go")
	registerMapCarrier("go")
	valueDefaultTargets = append(valueDefaultTargets, "go")
	wasRowTargets = append(wasRowTargets, "go")
	registerPacketValueDefaultCarrier("go")
	registerWideTextCarrier("go")
}
