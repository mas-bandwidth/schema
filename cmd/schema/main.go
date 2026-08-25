// The schema compiler CLI (SPEC §7).
//
//	schema check      [dir|files...]                 parse + typecheck; exit code for CI
//	schema generate   --lang cpp --out <dir> [dir]   emit generated code
//	schema id         [dir|files...]                 print the protocol id
//	schema projection [dir|files...]                 print the wire shape the id hashes
//	schema fmt        [dir|files...]                 canonicalize schema files in place
//	schema version                                   print the build identity
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
	// before processing them (schemafmt, SPEC §7.4). Success is silent —
	// --verbose announces the files a command rewrote or emitted; errors
	// always reach stderr.
	c := compiler.New()
	c.FormatInPlace = true
	verbose := false // set by each subcommand's --verbose flag before any file is loaded
	c.OnFormat = func(path string) {
		if verbose {
			fmt.Printf("formatted %s\n", path)
		}
	}

	switch os.Args[1] {
	// Accept every spelling a newcomer will try. `--version` costs nothing to
	// support and its absence reads as a broken tool.
	case "version", "--version", "-version", "-v":
		fmt.Println(compiler.UserAgent())
	case "help", "--help", "-help", "-h":
		usage()
	case "check":
		fs := flag.NewFlagSet("check", flag.ExitOnError)
		fs.BoolVar(&verbose, "verbose", false, "report the package and protocol id on success")
		_ = fs.Parse(os.Args[2:]) // ExitOnError: Parse never returns an error
		unit := loadUnit(c, fs.Args())
		if verbose {
			fmt.Printf("ok: package %s, protocol id 0x%016x\n", unit.Package, unit.ProtocolId)
		}
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
		fs := flag.NewFlagSet("fmt", flag.ExitOnError)
		fs.BoolVar(&verbose, "verbose", false, "list the files rewritten")
		migrate := fs.Bool("migrate", false, "one-shot migration: additionally accept the retired spellings ([ ... ] attribute blocks, [<= N] bounds) and rewrite them to the current grammar (SPEC §7.4)")
		_ = fs.Parse(os.Args[2:]) // ExitOnError: Parse never returns an error
		paths, err := compiler.GatherPaths(fs.Args())
		if err != nil {
			fail(err)
		}
		for _, p := range paths {
			var rewrote bool
			if *migrate {
				_, rewrote, err = compiler.MigrateFile(p)
			} else {
				_, rewrote, err = compiler.FormatFile(p)
			}
			if err != nil {
				fail(err)
			}
			if rewrote && verbose {
				fmt.Printf("formatted %s\n", p)
			}
		}
	case "generate":
		fs := flag.NewFlagSet("generate", flag.ExitOnError)
		lang := fs.String("lang", "cpp", "target language (c, cpp, cs, go, js, rust)")
		out := fs.String("out", "generated", "output directory")
		fs.BoolVar(&verbose, "verbose", false, "list the files emitted")
		_ = fs.Parse(os.Args[2:]) // ExitOnError: Parse never returns an error
		unit := loadUnit(c, fs.Args())
		files, err := c.Generate(unit, *lang, compiler.Options{})
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
			if verbose {
				fmt.Printf("wrote %s\n", path)
			}
		}
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `usage:
  schema check      [--verbose] [dir|files...]
  schema generate   [--lang c|cpp|cs|go|js|rust] [--out generated] [--verbose] [dir|files...]
  schema id         [dir|files...]
  schema projection [dir|files...]
  schema fmt        [--verbose] [dir|files...]
  schema version

Every command formats the unit's schema files in place before processing them
(schemafmt — one style, no options); a file already in format is not touched.
Success is silent: --verbose lists the files a command wrote or reformatted.
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
