package gotable

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// subsetCorpusUnits lists the corpus units whose generated Go packages must
// compile and pass go vet when any subset of {json, cook, block, message} is
// omitted (docs/SPEC-TABLES.md §2.9).
var subsetCorpusUnits = []struct {
	name  string
	paths []string
}{
	{"tables/examples", []string{"tables/examples"}},
	{"tables/pointers", []string{"tables/pointers"}},
	{"tables/lists", []string{"tables/lists"}},
	{"tables/maps", []string{"tables/maps"}},
	{"tables/messages", []string{"tables/messages"}},
	{"tables/cards", []string{"tables/cards"}},
	{"bench/corpus/BenchTable", []string{"bench/corpus/BenchTable.schema"}},
	{"bench/corpus/FixedTable", []string{"bench/corpus/Bench.schema", "bench/corpus/FixedTable.schema"}},
	{"tables/block", []string{"tables/block"}},
}

// TestGoTableCompilesWithAnySubsetOfItsForms generates each corpus unit and
// builds all sixteen subsets of {json, cook, block, message} omitted in
// t.TempDir() (with go build and go vet) without requiring SCHEMA_SLOW.
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
	units := make(map[string]*ir.Unit, len(subsetCorpusUnits))
	for _, entry := range subsetCorpusUnits {
		units[entry.name] = loadUnitPaths(t, root, entry.paths)
	}
	for _, entry := range subsetCorpusUnits {
		dir := entry.name
		u := units[dir]
		t.Run(dir, func(t *testing.T) {
			t.Parallel()
			baseFiles, err := golang.Generate(u)
			if err != nil {
				t.Fatalf("golang.Generate(%s): %v", dir, err)
			}
			tableFiles, err := Generate(u)
			if err != nil {
				t.Fatalf("gotable.Generate(%s): %v", dir, err)
			}
			modDir := t.TempDir()
			goMod := fmt.Sprintf("module subsetprobe\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go v0.0.0\nreplace github.com/mas-bandwidth/serialize.go => %q\n", runtime)
			if err := os.WriteFile(filepath.Join(modDir, "go.mod"), []byte(goMod), 0o600); err != nil {
				t.Fatal(err)
			}
			for mask := range 16 {
				mask := mask
				name := subsetMaskName(mask)
				t.Run(name, func(t *testing.T) {
					t.Parallel()
					pkgDir := filepath.Join(modDir, name)
					if err := os.MkdirAll(pkgDir, 0o700); err != nil {
						t.Fatal(err)
					}
					files := make(map[string][]byte, len(baseFiles)+len(tableFiles))
					maps.Copy(files, baseFiles)
					for file, data := range subsetFilterTableFiles(t, tableFiles, mask) {
						files[file] = data
					}
					for file, data := range files {
						if err := os.WriteFile(filepath.Join(pkgDir, file), data, 0o600); err != nil {
							t.Fatal(err)
						}
					}
					pkg := "./" + name
					if out, err := runGoCmd(modDir, "build", pkg); err != nil {
						t.Fatalf("go build %s (%s): %v\n%s", dir, name, err, out)
					}
					if out, err := runGoCmd(modDir, "vet", pkg); err != nil {
						t.Fatalf("go vet %s (%s): %v\n%s", dir, name, err, out)
					}
				})
			}
		})
	}
}

func runGoCmd(dir string, subcmd string, pkg string) ([]byte, error) {
	cmd := exec.Command("go", subcmd, pkg)
	cmd.Dir = dir
	return cmd.CombinedOutput()
}

func subsetMaskName(mask int) string {
	if mask == 0 {
		return "omit_none"
	}
	var parts []string
	if mask&1 != 0 {
		parts = append(parts, "json")
	}
	if mask&2 != 0 {
		parts = append(parts, "cook")
	}
	if mask&4 != 0 {
		parts = append(parts, "block")
	}
	if mask&8 != 0 {
		parts = append(parts, "message")
	}
	return "omit_" + strings.Join(parts, "_")
}

func subsetFilterTableFiles(t *testing.T, tableFiles map[string][]byte, mask int) map[string][]byte {
	t.Helper()
	omitJSON := mask&1 != 0
	omitCook := mask&2 != 0
	omitBlock := mask&4 != 0
	omitMessage := mask&8 != 0

	out := make(map[string][]byte, len(tableFiles))
	for name, data := range tableFiles {
		if omitJSON && strings.HasSuffix(name, "TableJson.go") {
			continue
		}
		if omitCook && strings.HasSuffix(name, "Cook.go") {
			continue
		}
		if omitBlock && strings.HasSuffix(name, "Block.go") {
			continue
		}
		if (omitCook || omitMessage) && strings.HasSuffix(name, "Table.go") {
			data = subsetStripTableDecls(t, name, data, omitCook, omitMessage)
		}
		out[name] = data
	}
	return out
}

func subsetStripTableDecls(t *testing.T, filename string, src []byte, omitCook, omitMessage bool) []byte {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, src, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse %s: %v", filename, err)
	}
	var kept []ast.Decl
	for _, decl := range file.Decls {
		if subsetDropDecl(decl, omitCook, omitMessage) {
			continue
		}
		kept = append(kept, decl)
	}
	file.Decls = kept
	var bodyBuf bytes.Buffer
	for _, decl := range file.Decls {
		if gd, ok := decl.(*ast.GenDecl); ok && gd.Tok == token.IMPORT {
			continue
		}
		if err := format.Node(&bodyBuf, fset, decl); err != nil {
			t.Fatalf("format decl in %s: %v", filename, err)
		}
		bodyBuf.WriteByte('\n')
	}
	bodyText := bodyBuf.String()
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.IMPORT {
			continue
		}
		var specs []ast.Spec
		for _, sp := range gd.Specs {
			imp, ok := sp.(*ast.ImportSpec)
			if !ok || imp.Path == nil {
				continue
			}
			path := strings.Trim(imp.Path.Value, `"`)
			pkg := path
			if idx := strings.LastIndex(path, "/"); idx >= 0 {
				pkg = path[idx+1:]
			}
			pkg = strings.TrimSuffix(pkg, ".go")
			if strings.Contains(bodyText, pkg+".") {
				specs = append(specs, sp)
			}
		}
		gd.Specs = specs
	}
	var finalDecls []ast.Decl
	for _, decl := range file.Decls {
		if gd, ok := decl.(*ast.GenDecl); ok && gd.Tok == token.IMPORT && len(gd.Specs) == 0 {
			continue
		}
		finalDecls = append(finalDecls, decl)
	}
	file.Decls = finalDecls
	file.Comments = nil
	var out bytes.Buffer
	if err := format.Node(&out, fset, file); err != nil {
		t.Fatalf("format %s: %v", filename, err)
	}
	return out.Bytes()
}

func subsetDropDecl(decl ast.Decl, omitCook, omitMessage bool) bool {
	switch d := decl.(type) {
	case *ast.FuncDecl:
		if d.Recv != nil && len(d.Recv.List) > 0 {
			recv := subsetRecvTypeName(d.Recv.List[0].Type)
			if omitCook && recv == "tableCookWriter" {
				return true
			}
			if omitMessage && subsetIsMessageRecv(recv) {
				return true
			}
		}
		name := d.Name.Name
		if name == "init" && d.Body != nil {
			var buf bytes.Buffer
			_ = format.Node(&buf, token.NewFileSet(), d.Body)
			text := buf.String()
			if omitMessage && !strings.Contains(text, "TableTypeInfo{") && (strings.Contains(text, "SaveMessageBody") || strings.Contains(text, "LoadMessageBody")) {
				return true
			}
		}
		if omitCook && subsetIsCookIdent(name) {
			return true
		}
		if omitMessage && subsetIsMessageIdent(name) {
			return true
		}
	case *ast.GenDecl:
		if d.Tok == token.IMPORT {
			return false
		}
		var specs []ast.Spec
		for _, sp := range d.Specs {
			switch s := sp.(type) {
			case *ast.TypeSpec:
				if omitCook && subsetIsCookType(s.Name.Name) {
					continue
				}
				if omitMessage && subsetIsMessageType(s.Name.Name) {
					continue
				}
			case *ast.ValueSpec:
				var names []*ast.Ident
				for _, id := range s.Names {
					if omitCook && subsetIsCookIdent(id.Name) {
						continue
					}
					if omitMessage && subsetIsMessageIdent(id.Name) {
						continue
					}
					names = append(names, id)
				}
				if len(names) == 0 {
					continue
				}
				s.Names = names
			}
			specs = append(specs, sp)
		}
		if len(specs) == 0 {
			return true
		}
		d.Specs = specs
	}
	return false
}

func subsetRecvTypeName(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return subsetRecvTypeName(t.X)
	}
	return ""
}

func subsetIsCookType(name string) bool {
	return name == "TableByteOrder" || name == "tableCookWriter"
}

func subsetIsCookIdent(name string) bool {
	if name == "tableCookBuildVersion" || name == "TableByteOrderLittle" || name == "TableByteOrderBig" ||
		name == "tableCookFixed" || name == "tableCookRegion" || name == "tableCookRegionReason" {
		return true
	}
	for _, suf := range []string{"CookMeasure", "CookMeasureReason", "CookFrom"} {
		if strings.HasSuffix(name, suf) {
			return true
		}
	}
	return false
}

func subsetIsMessageRecv(recv string) bool {
	switch recv {
	case "TableBitWriter", "TableBitReader", "TableMessageWriter", "TableMessageReader",
		"TableVocabulary", "TableRetainMessageReader", "tableRetainMessageIn":
		return true
	}
	return false
}

func subsetIsMessageType(name string) bool {
	return name == "tableRetainMessageIn"
}

func subsetIsMessageIdent(name string) bool {
	if name == "Announce" || name == "AnnounceMeasure" || name == "AnnounceRead" ||
		name == "tableAnnounceRead" || name == "tableAnnouncement" ||
		name == "TableMessageEntriesHere" || name == "TableMessageRefBitsHere" || name == "TableMessageBatchMax" ||
		strings.HasPrefix(name, "tableMessage") || strings.HasPrefix(name, "tableRetainMessage") {
		return true
	}
	for _, suf := range []string{
		"MeasureMessages",
		"SaveMessages",
		"LoadMessages",
		"SaveMessageBody",
		"LoadMessageBody",
		"LoadMessageBodyRetain",
		"LoadMeasureMessages",
		"LoadBuilderMessages",
		"SaveRetainMessages",
		"LoadRetainMeasureMessages",
		"LoadRetainMessages",
		"LoadRetainBuilderMessages",
	} {
		if strings.HasSuffix(name, suf) {
			return true
		}
	}
	return false
}
