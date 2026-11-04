//go:build schema_leg_python

// One built-in target, registered from its own init so a target is one file
// and a new one adds a file (docs/CONTRIBUTING.md, "Adding a language").
//
// Scaffolded by `schema new-leg python`; docs/PORT-PYTHON.md is the port's
// design. The schema_leg_python tag keeps this target out of the builtins
// until the packet surface has claims in compiler/*_test.go;
// `go build -tags schema_leg_python` is `schema generate --lang python`. The
// tag is dropped when the port joins the builtins.
package compiler

import (
	"github.com/mas-bandwidth/schema/v2/internal/codegen/python"
	"github.com/mas-bandwidth/schema/v2/ir"
)

type pythonTarget struct{}

func (pythonTarget) Names() []string { return []string{"python"} }

// Generate is the NO-LINEAGE case: a unit whose lock the caller did not read,
// or that has none. Every fixed table then carries the single entry it can
// always compute, its own (docs/FIXED-FORM-ALGORITHM.md §5.9 #1).
func (t pythonTarget) Generate(u *ir.Unit, opts Options) (map[string][]byte, error) {
	return t.GenerateLineage(u, opts, nil)
}

// GenerateLineage is the packet surface (docs/PORT-PYTHON.md, THE LAYERS: P4)
// and the fixed form (P5). The shared refusal chain every port calls refuses,
// by name, each construct the target does not carry (docs/PORT-GOOSE.md, THE
// CARRIES PATTERN); the constructs keyed in compiler/carries.go are refused by
// the driver against the python carries set (compiler/carries_python*.go).
// The fixed form carries the wide and fixed-point kinds, so the target does
// not refuse them here; the emitter refuses every table the fixed form does
// not carry (a variable table, a fixed table past the form's ceiling, a union
// with a table arm) by name. THE LINEAGE IS THE DRIVER'S (§5.9 #1):
// compiler/generate.go reads the unit's lock once and hands the entries down
// as data.
func (pythonTarget) GenerateLineage(u *ir.Unit, _ Options, lineage *FixedLineage) (map[string][]byte, error) {
	if err := refuseWideText(u, "python"); err != nil {
		return nil, err
	}
	if err := refuseUnported(u, "python"); err != nil {
		return nil, err
	}
	if err := refuseOptionalArrays(u, "python"); err != nil {
		return nil, err
	}
	if err := refuseMaps(u, "python"); err != nil {
		return nil, err
	}
	if err := refuseLists(u, "python"); err != nil {
		return nil, err
	}
	return python.GenerateLineage(u, pythonTableLineage(u, lineage))
}

// pythonTableLineage is the lock's lineage in the Python backend's own
// spelling; nil in, nil out. Each entry's Record is already the whole record.
func pythonTableLineage(u *ir.Unit, lineage *FixedLineage) map[string][]python.FixedLineageEntry {
	if lineage == nil {
		return nil
	}
	out := map[string][]python.FixedLineageEntry{}
	for _, st := range ir.TableFixedFormRoots(u) {
		for _, e := range lineage.Entries(st.Name) {
			out[st.Name] = append(out[st.Name], python.FixedLineageEntry{
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
	// tables and the unions a table closure holds: the fixed form carries
	// both (the emitter refuses the variable ones by name)
	registerBuiltin(pythonTarget{}, true, true, false, false)
}
