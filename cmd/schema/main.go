// The schema compiler CLI (SPEC §7).
//
//	schema check    [dir|files...]                 parse + typecheck; exit code for CI
//	schema generate --lang cpp --out <dir> [dir]   emit generated code
//	schema id       [dir|files...]                 print the protocol id
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/internal/check"
	"github.com/mas-bandwidth/schema/internal/codegen/cpp"
	"github.com/mas-bandwidth/schema/internal/codegen/csharp"
	"github.com/mas-bandwidth/schema/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/internal/codegen/rust"
	"github.com/mas-bandwidth/schema/internal/format"
	"github.com/mas-bandwidth/schema/internal/ir"
	"github.com/mas-bandwidth/schema/internal/pack"
	"github.com/mas-bandwidth/schema/internal/parser"
	"github.com/mas-bandwidth/schema/internal/version"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	// Accept every spelling a newcomer will try. `--version` costs nothing to
	// support and its absence reads as a broken tool.
	case "version", "--version", "-version", "-v":
		fmt.Println(version.UserAgent())
	case "help", "--help", "-help", "-h":
		usage()
	case "check":
		unit := loadUnit(os.Args[2:])
		fmt.Printf("ok: package %s, protocol id 0x%016x\n", unit.Package, unit.ProtocolId)
	case "id":
		unit := loadUnit(os.Args[2:])
		fmt.Printf("0x%016x\n", unit.ProtocolId)
	case "fmt":
		// standalone formatting; check/generate/id already format before
		// processing, so this exists for editors and pre-commit hooks
		for _, p := range gatherPaths(os.Args[2:]) {
			formatInPlace(p)
		}
	case "generate":
		fs := flag.NewFlagSet("generate", flag.ExitOnError)
		lang := fs.String("lang", "cpp", "target language (cpp, cs, go, rust)")
		out := fs.String("out", "generated", "output directory")
		cppMessage := fs.String("cpp-message", "union", "C++ message representation: union (default) or variant")
		fs.Parse(os.Args[2:])
		unit := loadUnit(fs.Args())
		var files map[string][]byte
		var err error
		switch *lang {
		case "cpp":
			files, err = cpp.Generate(unit, cpp.Options{MessageRepr: *cppMessage})
		case "cs", "csharp":
			files, err = csharp.Generate(unit)
		case "go":
			files, err = golang.Generate(unit)
		case "rust":
			files, err = rust.Generate(unit)
		default:
			fatalf("target %q is not implemented — cpp, cs, go and rust are the live targets", *lang)
		}
		if err != nil {
			fatalf("%v", err)
		}
		if err := os.MkdirAll(*out, 0o755); err != nil {
			fatalf("%v", err)
		}
		names := make([]string, 0, len(files))
		for n := range files {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			path := filepath.Join(*out, n)
			if err := os.WriteFile(path, files[n], 0o644); err != nil {
				fatalf("%v", err)
			}
			fmt.Printf("wrote %s\n", path)
		}
	case "pack":
		if len(os.Args) != 3 {
			fatalf("usage: schema pack <manifest.json>")
		}
		manifestPath := os.Args[2]
		data, err := os.ReadFile(manifestPath)
		if err != nil {
			fatalf("%v", err)
		}
		var m pack.Manifest
		if err := json.Unmarshal(data, &m); err != nil {
			fatalf("%s: %v", manifestPath, err)
		}
		baseDir := filepath.Dir(manifestPath)
		unit := loadUnit([]string{filepath.Join(baseDir, m.Unit)})
		enc := &pack.Encoder{Unit: unit}
		for _, out := range m.Outputs {
			bin, err := pack.BuildOutput(unit, enc, out, baseDir)
			if err != nil {
				fatalf("%v", err)
			}
			path := filepath.Join(baseDir, out.File)
			if err := os.WriteFile(path, bin, 0o644); err != nil {
				fatalf("%v", err)
			}
			fmt.Printf("wrote %s (%d bytes, protocol id 0x%016x, content hash 0x%016x)\n",
				path, len(bin), unit.ProtocolId, pack.Fnv64a(bin[20:]))
		}
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `usage:
  schema check    [dir|files...]
  schema generate [--lang cpp|cs|go|rust] [--cpp-message union|variant] [--out generated] [dir|files...]
  schema id       [dir|files...]
  schema fmt      [dir|files...]
  schema pack     <manifest.json>
  schema version

Every command formats the unit's schema files in place before processing them
(schemafmt — one style, no options); a file already in format is not touched.
pack is the data compiler: JSON instance files -> a versioned, hashed .bin,
per the manifest's ordered collections (the table layer's transition form).
`)
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "schema: "+format+"\n", args...)
	os.Exit(1)
}

// gatherPaths collects the unit's *.schema files (one directory by default —
// SPEC §3.2), and exits nonzero on collection errors.
func gatherPaths(args []string) []string {
	if len(args) == 0 {
		args = []string{"."}
	}
	var paths []string
	for _, arg := range args {
		info, err := os.Stat(arg)
		if err != nil {
			fatalf("%v", err)
		}
		if info.IsDir() {
			entries, err := os.ReadDir(arg)
			if err != nil {
				fatalf("%v", err)
			}
			for _, e := range entries {
				// *.schema means a nonempty basename before the extension — a
				// bare dotfile named ".schema" is not a schema file (SPEC §3.1)
				if !e.IsDir() && strings.HasSuffix(e.Name(), ".schema") && len(e.Name()) > len(".schema") {
					paths = append(paths, filepath.Join(arg, e.Name()))
				}
			}
		} else {
			name := filepath.Base(arg)
			if !strings.HasSuffix(name, ".schema") || len(name) == len(".schema") {
				fatalf("%s: only *.schema files form a unit (SPEC §3.1 — the extension rule is part of the hash procedure)", arg)
			}
			paths = append(paths, arg)
		}
	}
	if len(paths) == 0 {
		fatalf("no .schema files found (a unit with no schema files is an error — SPEC §4.6)")
	}
	return paths
}

// formatInPlace canonicalizes one file (SPEC §7.4), writing only when the
// bytes actually change, and returns the canonical bytes.
func formatInPlace(p string) []byte {
	data, err := os.ReadFile(p)
	if err != nil {
		fatalf("%v", err)
	}
	out, err := format.Format(p, data)
	if err != nil {
		fatalf("%v", err)
	}
	if string(out) != string(data) {
		if err := os.WriteFile(p, out, 0o644); err != nil {
			fatalf("%v", err)
		}
		fmt.Printf("formatted %s\n", p)
	}
	return out
}

// loadUnit gathers, FORMATS (every command formats before processing —
// schemafmt, SPEC §7.4), parses and checks the unit, and exits nonzero on
// any error. The protocol id is therefore always computed over canonical
// bytes: §3.1's raw-byte hash is a canonical-form hash structurally.
func loadUnit(args []string) *ir.Unit {
	paths := gatherPaths(args)

	seen := map[string]string{}
	var files []check.SourceFile
	var errs []error
	for _, p := range paths {
		name := filepath.Base(p)
		if prev, dup := seen[name]; dup {
			fatalf("duplicate basename %s (%s and %s) — basenames are the hash labels (SPEC §3.1)", name, prev, p)
		}
		seen[name] = p
		data := formatInPlace(p)
		f, perrs := parser.Parse(p, data)
		errs = append(errs, perrs...)
		if f != nil {
			files = append(files, check.SourceFile{
				Path:  p,
				Name:  name,
				Base:  strings.TrimSuffix(name, ".schema"),
				Bytes: data,
				AST:   f,
			})
		}
	}
	if len(errs) > 0 {
		report(errs)
	}
	u, cerrs := check.Unit(files)
	if len(cerrs) > 0 {
		report(cerrs)
	}
	return u
}

func report(errs []error) {
	for _, e := range errs {
		fmt.Fprintln(os.Stderr, e)
	}
	fmt.Fprintf(os.Stderr, "schema: %d error(s)\n", len(errs))
	os.Exit(1)
}
