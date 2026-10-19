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
