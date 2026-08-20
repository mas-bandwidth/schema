// The schema compiler CLI (SPEC §7).
//
//	schema check    [dir|files...]                 parse + typecheck; exit code for CI
//	schema generate --lang cpp --out <dir> [dir]   emit generated code
//	schema id       [dir|files...]                 print the protocol id
//
// Every command here is a few lines over the public API in
// github.com/mas-bandwidth/schema/compiler: this binary holds the CLI's
// policy — which flags exist, what gets printed, what the exit code is, where
// the bytes land — and none of the compiler. Anything it can do, an embedder
// can do.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/mas-bandwidth/schema/compiler"
	"github.com/mas-bandwidth/schema/ir"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	// The CLI's load policy: every command formats the unit's files in place
	// before processing them (schemafmt, SPEC §7.4), announcing the ones it
	// rewrote.
	c := compiler.New()
	c.FormatInPlace = true
	c.OnFormat = func(path string) { fmt.Printf("formatted %s\n", path) }

	switch os.Args[1] {
	// Accept every spelling a newcomer will try. `--version` costs nothing to
	// support and its absence reads as a broken tool.
	case "version", "--version", "-version", "-v":
		fmt.Println(compiler.UserAgent())
	case "help", "--help", "-help", "-h":
		usage()
	case "check":
		unit := loadUnit(c, os.Args[2:])
		fmt.Printf("ok: package %s, protocol id 0x%016x\n", unit.Package, unit.ProtocolId)
	case "id":
		unit := loadUnit(c, os.Args[2:])
		fmt.Printf("0x%016x\n", unit.ProtocolId)
	case "projection":
		// What the protocol id actually hashes. Printable on purpose: a
		// wire-affecting fact missing from this text is a fact the id ignores,
		// and that is a review question, not an implementation detail.
		unit := loadUnit(c, os.Args[2:])
		fmt.Print(ir.WireProjection(unit))
	case "fmt":
		// standalone formatting; check/generate/id already format before
		// processing, so this exists for editors and pre-commit hooks
		paths, err := compiler.GatherPaths(os.Args[2:])
		if err != nil {
			fail(err)
		}
		for _, p := range paths {
			_, rewrote, err := compiler.FormatFile(p)
			if err != nil {
				fail(err)
			}
			if rewrote {
				fmt.Printf("formatted %s\n", p)
			}
		}
	case "generate":
		fs := flag.NewFlagSet("generate", flag.ExitOnError)
		lang := fs.String("lang", "cpp", "target language (c, cpp, cs, go, js, rust)")
		out := fs.String("out", "generated", "output directory")
		cppMessage := fs.String("cpp-message", "union", "C++ message representation: union (default) or variant")
		_ = fs.Parse(os.Args[2:]) // ExitOnError: Parse never returns an error
		unit := loadUnit(c, fs.Args())
		files, err := c.Generate(unit, *lang, compiler.Options{"cpp-message": *cppMessage})
		if err != nil {
			fail(err)
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
		unit, outputs, err := c.Pack(os.Args[2])
		if err != nil {
			fail(err)
		}
		for _, o := range outputs {
			if err := os.WriteFile(o.File, o.Bytes, 0o644); err != nil {
				fatalf("%v", err)
			}
			fmt.Printf("wrote %s (%d bytes, protocol id 0x%016x, content hash 0x%016x)\n",
				o.File, len(o.Bytes), unit.ProtocolId, o.ContentHash)
		}
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `usage:
  schema check      [dir|files...]
  schema generate   [--lang c|cpp|cs|go|js|rust] [--cpp-message union|variant] [--out generated] [dir|files...]
  schema id         [dir|files...]
  schema projection [dir|files...]
  schema fmt        [dir|files...]
  schema pack       <manifest.json>
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

// fail reports a driver error and exits nonzero: a unit's diagnostics one per
// line with a count — there is no first error worth privileging when a file
// has twenty — and anything else as the single line it is.
func fail(err error) {
	if diags, ok := errors.AsType[compiler.Diagnostics](err); ok {
		for _, e := range diags {
			fmt.Fprintln(os.Stderr, e)
		}
		fmt.Fprintf(os.Stderr, "schema: %d error(s)\n", len(diags))
		os.Exit(1)
	}
	fatalf("%v", err)
}

// loadUnit gathers the unit's files and compiles them, exiting nonzero on any
// error.
func loadUnit(c *compiler.Compiler, args []string) *ir.Unit {
	paths, err := compiler.GatherPaths(args)
	if err != nil {
		fail(err)
	}
	u, err := c.Load(paths)
	if err != nil {
		fail(err)
	}
	return u
}
