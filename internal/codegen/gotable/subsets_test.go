package gotable

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
)

// subsetCorpusUnits is the corpus whose generated Go packages must compile and
// pass go vet with any subset of the optional forms {json, cook, block,
// message} omitted (docs/SPEC-TABLES.md, "GO's optional forms"): every table corpus under
// tables/, the bench table units, and the wide-value examples.
var subsetCorpusUnits = []struct {
	name  string
	paths []string
}{
	{"tables/arms", []string{"tables/arms"}},
	{"tables/backend", []string{"tables/backend"}},
	{"tables/blobs", []string{"tables/blobs"}},
	{"tables/block", []string{"tables/block"}},
	{"tables/blockhome", []string{"tables/blockhome"}},
	{"tables/cards", []string{"tables/cards"}},
	{"tables/examples", []string{"tables/examples"}},
	{"tables/lists", []string{"tables/lists"}},
	{"tables/maps", []string{"tables/maps"}},
	{"tables/messages", []string{"tables/messages"}},
	{"tables/pointers", []string{"tables/pointers"}},
	{"tables/scalars", []string{"tables/scalars"}},
	{"tables/stream", []string{"tables/stream"}},
	{"tables/vocab", []string{"tables/vocab"}},
	{"tables/vocab9", []string{"tables/vocab9"}},
	{"examples-wide", []string{"examples-wide"}},
	{"bench/corpus/BenchTable", []string{"bench/corpus/BenchTable.schema"}},
	{"bench/corpus/FixedTable", []string{"bench/corpus/Bench.schema", "bench/corpus/FixedTable.schema"}},
}

// subsetForms is the optional forms, in the bit order a subset mask uses.
var subsetForms = []struct {
	form Form
	name string
}{{FormJson, "json"}, {FormCook, "cook"}, {FormBlock, "block"}, {FormMessage, "message"}}

// TestGoTableCompilesWithAnySubsetOfItsForms generates every corpus unit through
// the emitter's own omit path, GenerateOmitting, once for each of the sixteen
// subsets of {json, cook, block, message}, and builds and vets each package in
// t.TempDir(). No declaration is edited here: what is compiled is exactly what
// the emitter wrote. Beside the compile, it holds the split to its contract:
//
//   - omitting nothing is Generate, byte for byte;
//   - omitting a form only REMOVES files — every file left is byte-identical to
//     Generate's, so a form's omission cannot change the core;
//   - a form the unit carries has files of its own, and none of the
//     declarations in them survives the form's omission, so every form omitted
//     here is really gone.
//
// It runs under the default gate (no SCHEMA_SLOW): the compile is a local go
// build against the sibling serialize.go runtime, as the other
// generated-compile tests in this package resolve it.
func TestGoTableCompilesWithAnySubsetOfItsForms(t *testing.T) {
	t.Parallel()
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := filepath.Abs("../../../../serialize.go")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(runtime, "go.mod")); err != nil {
		t.Fatalf("the generated packet types import the sibling serialize.go runtime, not staged at %s: %v", runtime, err)
	}
	for _, entry := range subsetCorpusUnits {
		u := loadUnitPaths(t, root, entry.paths)
		t.Run(entry.name, func(t *testing.T) {
			t.Parallel()
			packet, err := golang.Generate(u)
			if err != nil {
				t.Fatalf("golang.Generate: %v", err)
			}
			full, err := Generate(u)
			if err != nil {
				t.Fatalf("Generate: %v", err)
			}
			// each form's own files, read off the omit path one form at a time
			formFiles := map[Form][]string{}
			for _, f := range subsetForms {
				without, err := GenerateOmitting(u, nil, f.form)
				if err != nil {
					t.Fatalf("GenerateOmitting(%s): %v", f.name, err)
				}
				for name := range full {
					if _, kept := without[name]; !kept {
						formFiles[f.form] = append(formFiles[f.form], name)
					}
				}
				slices.Sort(formFiles[f.form])
			}
			// the core and the forms that every table unit carries
			for _, f := range []Form{FormJson, FormCook, FormMessage} {
				if len(formFiles[f]) == 0 {
					t.Fatalf("form %d emitted no file of its own, so omitting it would prove nothing", f)
				}
			}
			formDecls := map[Form][]string{}
			for _, f := range subsetForms {
				for _, name := range formFiles[f.form] {
					formDecls[f.form] = append(formDecls[f.form], subsetDeclaredNames(t, name, full[name])...)
				}
			}
			modDir := t.TempDir()
			goMod := fmt.Sprintf("module subsetprobe\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go v0.0.0\nreplace github.com/mas-bandwidth/serialize.go => %q\n", runtime)
			if err := os.WriteFile(filepath.Join(modDir, "go.mod"), []byte(goMod), 0o600); err != nil {
				t.Fatal(err)
			}
			for mask := range 16 {
				omit := Form(0)
				for i, f := range subsetForms {
					if mask&(1<<i) != 0 {
						omit |= f.form
					}
				}
				name := subsetMaskName(mask)
				t.Run(name, func(t *testing.T) {
					t.Parallel()
					files, err := GenerateOmitting(u, nil, omit)
					if err != nil {
						t.Fatalf("GenerateOmitting: %v", err)
					}
					subsetCheckOmission(t, full, files, omit, formFiles, formDecls)
					pkgDir := filepath.Join(modDir, name)
					if err := os.MkdirAll(pkgDir, 0o700); err != nil {
						t.Fatal(err)
					}
					all := maps.Clone(packet)
					maps.Copy(all, files)
					for file, data := range all {
						if err := os.WriteFile(filepath.Join(pkgDir, file), data, 0o600); err != nil {
							t.Fatal(err)
						}
					}
					for _, verb := range []string{"build", "vet"} {
						if out, err := subsetGo(modDir, verb, "./"+name); err != nil {
							t.Fatalf("go %s with %s: %v\n%s", verb, name, err, out)
						}
					}
				})
			}
		})
	}
}

// subsetCheckOmission holds one omitted output to the split's contract.
func subsetCheckOmission(t *testing.T, full, files map[string][]byte, omit Form, formFiles map[Form][]string, formDecls map[Form][]string) {
	t.Helper()
	if omit == 0 && !maps.EqualFunc(full, files, bytes.Equal) {
		t.Fatalf("GenerateOmitting with nothing omitted is not Generate")
	}
	for name, data := range files {
		if !bytes.Equal(full[name], data) {
			t.Fatalf("%s differs from Generate's: omitting a form changed a file it does not own", name)
		}
	}
	declared := map[string]bool{}
	for name, data := range files {
		for _, d := range subsetDeclaredNames(t, name, data) {
			declared[d] = true
		}
	}
	for _, f := range subsetForms {
		gone := omit&f.form != 0
		for _, name := range formFiles[f.form] {
			if _, kept := files[name]; kept == gone {
				t.Fatalf("%s (form %s) kept=%v with the form omitted=%v", name, f.name, kept, gone)
			}
		}
		if !gone {
			continue
		}
		for _, d := range formDecls[f.form] {
			if declared[d] {
				t.Fatalf("%s, declared by the %s form, is still declared with the form omitted", d, f.name)
			}
		}
	}
}

// subsetDeclaredNames is every package-level name a file declares, methods as
// Type.Method; init functions declare nothing.
func subsetDeclaredNames(t *testing.T, name string, src []byte) []string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), name, src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	var names []string
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Recv == nil {
				if d.Name.Name != "init" {
					names = append(names, d.Name.Name)
				}
				continue
			}
			recv := d.Recv.List[0].Type
			if star, ok := recv.(*ast.StarExpr); ok {
				recv = star.X
			}
			if id, ok := recv.(*ast.Ident); ok {
				names = append(names, id.Name+"."+d.Name.Name)
			}
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					names = append(names, s.Name.Name)
				case *ast.ValueSpec:
					for _, id := range s.Names {
						if id.Name != "_" {
							names = append(names, id.Name)
						}
					}
				}
			}
		}
	}
	return names
}

func subsetGo(dir, verb, pkg string) ([]byte, error) {
	cmd := exec.Command("go", verb, pkg)
	cmd.Dir = dir
	return cmd.CombinedOutput()
}

func subsetMaskName(mask int) string {
	var parts []string
	for i, f := range subsetForms {
		if mask&(1<<i) != 0 {
			parts = append(parts, f.name)
		}
	}
	if len(parts) == 0 {
		return "omit_none"
	}
	return "omit_" + strings.Join(parts, "_")
}
