// stage_seeds decodes the Go fuzz corpus and materialises the repository's
// wire/text/cook fixtures as a directory of raw seed files for libFuzzer.
//
// The Go fuzz corpus under internal/fuzz/testdata and internal/tabletext/
// testdata is stored in the `go test fuzz v1` text encoding, not as raw input
// bytes. Passing its directories to libFuzzer seeds the fuzzer with that
// encoding (and with nested directories it may not read at all). This program
// decodes each operand with strconv.Unquote into the bytes the Go fuzzer fed
// its target, and copies the conformance vectors (wire bytes, JSON text, and
// cooked files) so every seed the fuzzer sees is a real input, not a source
// row or a corpus wrapper.
package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: stage_seeds <outdir>")
		os.Exit(2)
	}
	out := os.Args[1]
	for _, d := range []string{"wire", "text", "cook", "go"} {
		if err := os.MkdirAll(filepath.Join(out, d), 0o755); err != nil {
			fail(err)
		}
	}
	if err := copyExt(filepath.Join(out, "wire"), "testdata/wire/tables", ".bin"); err != nil {
		fail(err)
	}
	if err := copyExt(filepath.Join(out, "text"), "testdata/conformance/tables/json", ".json"); err != nil {
		fail(err)
	}
	if err := copyExt(filepath.Join(out, "text"), "testdata/conformance/tables/json-hostile", ".json"); err != nil {
		fail(err)
	}
	if err := copyExt(filepath.Join(out, "cook"), "testdata/conformance/tables/cook-write", ".cook"); err != nil {
		fail(err)
	}
	if err := decodeGoFuzz(filepath.Join(out, "go"), "internal/fuzz/testdata"); err != nil {
		fail(err)
	}
	if err := decodeGoFuzz(filepath.Join(out, "go"), "internal/tabletext/testdata"); err != nil {
		fail(err)
	}
	if err := stageBuiltInFAdd(filepath.Join(out, "go")); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintf(os.Stderr, "stage_seeds: %v\n", err)
	os.Exit(1)
}

// flatName turns a repository-relative path into one seed file name.
func flatName(rel string) string {
	r := strings.NewReplacer("/", "__", "\\", "__", ".", "_", " ", "_").Replace(filepath.ToSlash(rel))
	return r
}

func copyExt(dst, src, ext string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.EqualFold(filepath.Ext(path), ext) {
			return nil
		}
		rel, err := filepath.Rel(".", path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, flatName(rel)), data, 0o644)
	})
}

// decodeGoFuzz walks one `go test fuzz v1` corpus tree and writes every
// operand as its own raw seed. Unknown operand kinds are refused loudly: a
// silently skipped seed is a seed nobody knows is missing.
func decodeGoFuzz(dst, src string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		lines := strings.Split(string(data), "\n")
		if len(lines) == 0 || strings.TrimSpace(lines[0]) != "go test fuzz v1" {
			return nil
		}
		rel, err := filepath.Rel(".", path)
		if err != nil {
			return err
		}
		n := 0
		for _, line := range lines[1:] {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			raw, err := operandBytes(line)
			if err != nil {
				return fmt.Errorf("%s: %w", rel, err)
			}
			name := fmt.Sprintf("%s__%d", flatName(rel), n)
			if err := os.WriteFile(filepath.Join(dst, name), raw, 0o644); err != nil {
				return err
			}
			n++
		}
		return nil
	})
}

// stageBuiltInFAdd materialises the seeds registered in source with f.Add.
// Checked-in corpus files cover fuzz-found regressions; these literals are the
// hand-picked starting points that plain `go test` registers before mutation.
// Keep the extraction structural so newly added literals ride automatically.
func stageBuiltInFAdd(dst string) error {
	files := []string{"internal/fuzz/fuzz_test.go", "internal/fuzz/tablefrontend_test.go", "internal/tabletext/text_test.go"}
	written := 0
	for _, path := range files {
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		write := func(expr ast.Expr) error {
			raw, ok, err := goSeedLiteral(expr)
			if err != nil {
				return fmt.Errorf("%s: %w", path, err)
			}
			if !ok {
				return nil
			}
			name := fmt.Sprintf("f_add__%s__%04d", flatName(path), written)
			written++
			return os.WriteFile(filepath.Join(dst, name), raw, 0o644)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.ValueSpec:
				for i, name := range n.Names {
					if (name.Name != "handSeeds" && name.Name != "tableHandSeeds") || i >= len(n.Values) {
						continue
					}
					if lit, ok := n.Values[i].(*ast.CompositeLit); ok {
						for _, elt := range lit.Elts {
							if err == nil {
								err = write(elt)
							}
						}
					}
				}
			case *ast.AssignStmt:
				for i, lhs := range n.Lhs {
					id, ok := lhs.(*ast.Ident)
					if !ok || id.Name != "seeds" || i >= len(n.Rhs) {
						continue
					}
					if lit, ok := n.Rhs[i].(*ast.CompositeLit); ok {
						for _, elt := range lit.Elts {
							if err == nil {
								err = write(elt)
							}
						}
					}
				}
			case *ast.CallExpr:
				sel, ok := n.Fun.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "Add" {
					break
				}
				for _, arg := range n.Args {
					if err == nil {
						err = write(arg)
					}
				}
			}
			return err == nil
		})
		if err != nil {
			return err
		}
	}
	if written == 0 {
		return fmt.Errorf("no built-in f.Add seeds found")
	}
	return nil
}

func goSeedLiteral(expr ast.Expr) ([]byte, bool, error) {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return nil, false, nil
	}
	s, err := strconv.Unquote(lit.Value)
	if err != nil {
		return nil, false, err
	}
	return []byte(s), true, nil
}

func operandBytes(line string) ([]byte, error) {
	for _, prefix := range []string{"[]byte(", "string("} {
		if !strings.HasPrefix(line, prefix) || !strings.HasSuffix(line, ")") {
			continue
		}
		lit := strings.TrimSuffix(strings.TrimPrefix(line, prefix), ")")
		s, err := strconv.Unquote(lit)
		if err != nil {
			return nil, err
		}
		return []byte(s), nil
	}
	return nil, fmt.Errorf("unsupported fuzz operand %q", line)
}
