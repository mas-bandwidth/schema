// The schema compiler CLI (SPEC §7).
//
//	schema check    [dir|files...]                 parse + typecheck; exit code for CI
//	schema generate --lang cpp --out <dir> [dir]   emit generated code
//	schema id       [dir|files...]                 print the protocol id
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/internal/check"
	"github.com/mas-bandwidth/schema/internal/codegen/cpp"
	"github.com/mas-bandwidth/schema/internal/ir"
	"github.com/mas-bandwidth/schema/internal/parser"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "check":
		unit := loadUnit(os.Args[2:])
		fmt.Printf("ok: package %s, protocol id 0x%016x\n", unit.Package, unit.ProtocolId)
	case "id":
		unit := loadUnit(os.Args[2:])
		fmt.Printf("0x%016x\n", unit.ProtocolId)
	case "generate":
		fs := flag.NewFlagSet("generate", flag.ExitOnError)
		lang := fs.String("lang", "cpp", "comma-separated target languages (cpp, cs, go, rust)")
		out := fs.String("out", "generated", "output directory")
		cppMessage := fs.String("cpp-message", "union", "C++ message representation: union (default) or variant")
		fs.Parse(os.Args[2:])
		for _, l := range strings.Split(*lang, ",") {
			if l != "cpp" {
				fatalf("target %q is not implemented yet — cpp is the first target", l)
			}
		}
		unit := loadUnit(fs.Args())
		if err := os.MkdirAll(*out, 0o755); err != nil {
			fatalf("%v", err)
		}
		files, err := cpp.Generate(unit, cpp.Options{MessageRepr: *cppMessage})
		if err != nil {
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
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `usage:
  schema check    [dir|files...]
  schema generate [--lang cpp] [--out generated] [dir|files...]
  schema id       [dir|files...]
`)
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "schema: "+format+"\n", args...)
	os.Exit(1)
}

// loadUnit gathers the unit's *.schema files (one directory by default —
// SPEC §3.2), parses and checks them, and exits nonzero on any error.
func loadUnit(args []string) *ir.Unit {
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

	seen := map[string]string{}
	var files []check.SourceFile
	var errs []error
	for _, p := range paths {
		name := filepath.Base(p)
		if prev, dup := seen[name]; dup {
			fatalf("duplicate basename %s (%s and %s) — basenames are the hash labels (SPEC §3.1)", name, prev, p)
		}
		seen[name] = p
		data, err := os.ReadFile(p)
		if err != nil {
			fatalf("%v", err)
		}
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
