package jstable

// EVERY UNION OF THE TABLE CORPUS CONSTRUCTS AND ROUND-TRIPS IN NODE
// (docs/SPEC-TABLES.md §2.6, "JavaScript: the union's storage class").
//
// internal/codegen/js writes a union's storage class with one `new <Arm>()`
// per arm that carries a declaration, and a TABLE arm's class is not the
// packet emitter's: it lives in the unit's fixed home (<Home>Table.js) when the
// union is in the fixed form's closure, and in the variable home
// (<Home>Variable.js) otherwise. A union class that named the arm without
// importing it was a module that loaded and then threw "ReferenceError:
// LoginRequest is not defined" the first time anything built one — and every
// fixed table that holds the union builds one in its own constructor.
//
// So this test generates every unit the build/tables-generated-js stamp recipe
// generates (make/js.mk), exactly as `schema generate --lang js` does, and runs
// testdata/unionroundtrip/driver.mjs over it under node's own test runner. For
// every union the units declare the driver:
//
//   - constructs it, and asserts that each arm's storage is an instance of the
//     class the DEFINING unit exports (the home this test names from the
//     closure, never from the module the union happens to import);
//   - for each arm in turn, builds a holder table around it, fills the arm,
//     writes the holder through the form that carries it (the fixed form's
//     Save/Load when the holder is a fixed root, the variable form's otherwise)
//     and reads it back, and asserts the value read is the value written.
//
// It skips BY NAME when node is absent (jsNode).
import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/js"
	"github.com/mas-bandwidth/schema/v2/ir"
)

type unionRoundTripArm struct {
	Name   string `json:"name"`   // the schema arm name, the variable form's key
	Member string `json:"member"` // the storage class's member
	Kind   string `json:"kind"`   // "void", "class" or "direct"
	Class  string `json:"class,omitempty"`
	Module string `json:"module,omitempty"` // the module that DEFINES Class
}

type unionRoundTripHolder struct {
	Form   string `json:"form"` // "fixed" or "variable"
	Root   string `json:"root"`
	Field  string `json:"field"`  // the holder class's member that holds the union
	Module string `json:"module"` // where the Save/Load verbs live
	Home   string `json:"home"`   // where the root class and the report live
}

type unionRoundTripCase struct {
	Dir    string                `json:"dir"`
	Union  string                `json:"union"`
	Module string                `json:"module"` // the module that declares the union class
	Arms   []unionRoundTripArm   `json:"arms"`
	Holder *unionRoundTripHolder `json:"holder,omitempty"`
}

func TestTableUnionsConstructAndRoundTripJavaScript(t *testing.T) {
	t.Parallel()
	node := jsNode(t)
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()

	recipe := stampRecipe(t, "build/tables-generated-js/.stamp")
	re := regexp.MustCompile(`generate --lang js --out build/tables-generated-js/(\S+) (\S+)`)
	var cases []unionRoundTripCase
	units := 0
	for _, m := range re.FindAllStringSubmatch(recipe, -1) {
		dir, src := m[1], m[2]
		units++
		u := loadUnitPaths(t, []string{filepath.Join(repo, src)})
		unitCases := unionRoundTripCases(t, u, dir)
		if len(unitCases) == 0 {
			continue
		}
		packet, err := js.Generate(u)
		if err != nil {
			t.Fatalf("%s: js.Generate: %v", dir, err)
		}
		tables, err := Generate(u)
		if err != nil {
			t.Fatalf("%s: jstable.Generate: %v", dir, err)
		}
		out := filepath.Join(work, dir)
		if err := os.MkdirAll(out, 0o700); err != nil {
			t.Fatal(err)
		}
		for _, files := range []map[string][]byte{packet, tables} {
			for name, data := range files {
				if err := os.WriteFile(filepath.Join(out, name), data, 0o600); err != nil {
					t.Fatal(err)
				}
			}
		}
		cases = append(cases, unitCases...)
	}
	if units == 0 {
		t.Fatal("the build/tables-generated-js stamp recipe names no unit — this test would pass over the empty set")
	}
	if len(cases) == 0 {
		t.Fatal("the table corpus declares no union — this test would pass over the empty set")
	}
	held := 0
	for _, c := range cases {
		if c.Holder != nil {
			held++
		}
	}
	if held == 0 {
		t.Fatal("no union of the table corpus is held by a root this leg carries — nothing would round-trip")
	}

	spec, err := json.Marshal(map[string]any{"root": work, "cases": cases})
	if err != nil {
		t.Fatal(err)
	}
	specPath := filepath.Join(work, "spec.json")
	if err := os.WriteFile(specPath, spec, 0o600); err != nil {
		t.Fatal(err)
	}
	driver, err := filepath.Abs(filepath.Join("testdata", "unionroundtrip", "driver.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, "--test-reporter=tap", driver, specPath)
	cmd.Dir = work
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the union round trip failed under node (%v):\n%s", err, output)
	}
	text := string(output)
	for _, c := range cases {
		if !strings.Contains(text, "ok ") || !strings.Contains(text, c.Dir+" "+c.Union) {
			t.Errorf("node ran no test for %s %s:\n%s", c.Dir, c.Union, text)
		}
	}
	if !strings.Contains(text, "# fail 0") {
		t.Fatalf("node's test runner did not report zero failures:\n%s", text)
	}
	t.Logf("%d unions constructed, %d round-tripped through a holder", len(cases), held)
}

// unionRoundTripCases answers one case per union a unit declares (packet and
// table-closure alike), naming the home that DEFINES each arm's class and the
// holder root the round trip goes through.
func unionRoundTripCases(t *testing.T, u *ir.Unit, dir string) []unionRoundTripCase {
	t.Helper()
	home := runtimeHome(u)
	// internal/codegen/js names the home through ir; this package through its
	// own runtimeHome (make/js.mk's runtime-home control patches that line by
	// text). They are one answer or a union imports from a module that is not
	// there.
	if other := ir.JSTableRuntimeHome(u); other != home {
		t.Errorf("%s: ir.JSTableRuntimeHome = %q but jstable's runtimeHome = %q", dir, other, home)
	}

	// THE FIXED CLOSURE: every table reached from a fixed root, and every
	// union one of them holds.
	seen := map[string]bool{}
	var closure []*ir.Struct
	fixedRootFile := map[string]string{}
	for _, f := range u.Files {
		for _, st := range fixedRoots(u, f.Tables) {
			fixedRootFile[st.Name] = f.Base
			fixedCollectTypes(st, seen, &closure)
		}
	}
	fixedUnion := map[string]bool{}
	for _, st := range closure {
		for _, f := range st.Fields {
			if r, ok := f.Type.Ref.(*ir.Union); ok && f.Type.Kind == ir.TNamed {
				fixedUnion[r.Name] = true
			}
		}
	}
	vc := newVariableClass(u)

	var out []unionRoundTripCase
	for _, f := range u.Files {
		var unions []*ir.Union
		for _, d := range f.Decls {
			if r, ok := d.(*ir.Union); ok {
				unions = append(unions, r)
			}
		}
		unions = append(unions, f.TableUnions...)
		for _, un := range unions {
			c := unionRoundTripCase{Dir: dir, Union: un.Name, Module: f.Base + ".js"}
			for _, v := range un.Variants {
				arm := unionRoundTripArm{Name: v.Name, Member: ir.GoExportName(v.Name)}
				switch {
				case v.Void():
					arm.Kind = "void"
				case v.Ref == nil:
					arm.Kind = "direct"
				default:
					arm.Kind = "class"
					arm.Class = v.Type
					switch {
					case !v.Ref.IsTable:
						arm.Module = u.DeclFile[v.Type] + ".js"
					case fixedUnion[un.Name]:
						arm.Module = home + "Table.js"
					default:
						arm.Module = home + "Variable.js"
					}
				}
				c.Arms = append(c.Arms, arm)
			}
			c.Holder = unionRoundTripHolderFor(u, un, home, fixedRootFile, vc)
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Union < out[j].Union })
	return out
}

// unionRoundTripHolderFor picks the root that holds the union BY VALUE,
// preferring a fixed root (the form whose classes the union class constructs
// when it is in the fixed closure), else a root the variable form carries.
func unionRoundTripHolderFor(u *ir.Unit, un *ir.Union, home string, fixedRootFile map[string]string, vc *variableClass) *unionRoundTripHolder {
	holds := func(st *ir.Struct) *ir.Field {
		for _, f := range st.Fields {
			if f.Type.Kind == ir.TNamed && f.Type.Ref == ir.Decl(un) && f.Array == ir.ArrayNone && !f.Type.Optional && !f.IsMap() {
				return f
			}
		}
		return nil
	}
	for _, f := range u.Files {
		for _, st := range f.Tables {
			if _, ok := fixedRootFile[st.Name]; !ok {
				continue
			}
			if fl := holds(st); fl != nil {
				return &unionRoundTripHolder{Form: "fixed", Root: st.Name, Field: ir.GoExportName(fl.Name),
					Module: f.Base + "Table.js", Home: home + "Table.js"}
			}
		}
	}
	for _, f := range u.Files {
		for _, st := range variableRoots(u, f.Tables) {
			if !vc.carried(st.Name) {
				continue
			}
			if fl := holds(st); fl != nil {
				return &unionRoundTripHolder{Form: "variable", Root: st.Name, Field: fl.Name,
					Module: home + "Variable.js", Home: home + "Variable.js"}
			}
		}
	}
	return nil
}
