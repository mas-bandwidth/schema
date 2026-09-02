// The schema compiler CLI (SPEC §7).
//
//	schema check      [dir|files...]                 parse + typecheck; exit code for CI
//	schema generate   --lang cpp --out <dir> [dir]   emit generated code
//	schema id         [dir|files...]                 print the protocol id
//	schema projection [dir|files...]                 print the wire shape the id hashes
//	schema tables-baseline [--update --reason "..."]  print or move the tables baseline
//	schema fmt        [dir|files...]                 canonicalize schema files in place
//	schema pack       --root T --out F <dir>         a directory tree becomes one table's wire bytes
//	schema unpack     --root T --in  F <dir>         the wire bytes become the tree again
//	schema version                                   print the build identity
//
// Every command here is a few lines over the public API in
// github.com/mas-bandwidth/schema/v2/compiler: this binary holds the CLI's
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

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/ir"
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
	// Warnings are never quiet and never gated on --verbose: a warning nobody
	// reads is a warning that does not exist. Today they come from the tables
	// baseline's warn class (SPEC-TABLES.md §18.2).
	c.OnWarn = func(msg string) { fmt.Fprintln(os.Stderr, "warning: "+msg) }

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
		c.TablesBaseline = true
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
	case "tables-baseline":
		// The TABLES BASELINE (SPEC-TABLES.md §18). Printing is the default:
		// the same canonical projection the committed file holds, so a
		// pipeline can diff without writing anything. --update moves the file
		// and records WHY in its history — and never without --reason,
		// because an intentional break that nobody declared is the failure
		// this file exists to prevent.
		//
		// Neither mode runs the baseline check on load: --update is the tool
		// for moving a baseline the check is refusing, and it cannot be
		// blocked by the refusal it exists to resolve.
		fs := flag.NewFlagSet("tables-baseline", flag.ExitOnError)
		update := fs.Bool("update", false, "rewrite the unit's tables.baseline and record the move in its history")
		reason := fs.String("reason", "", "why the baseline moves — mandatory with --update, and written into the file's history")
		fs.BoolVar(&verbose, "verbose", false, "name the file written")
		_ = fs.Parse(os.Args[2:]) // ExitOnError: Parse never returns an error
		paths, err := compiler.GatherPaths(fs.Args())
		if err != nil {
			fail(err)
		}
		u, err := c.Load(paths)
		if err != nil {
			fail(err)
		}
		if !*update {
			if *reason != "" {
				fatalf("--reason belongs with --update: it records why a baseline MOVED")
			}
			fmt.Print(compiler.TablesBaselineText(u))
			break
		}
		path, rewrote, err := compiler.UpdateTablesBaseline(u, paths, *reason)
		if err != nil {
			fail(err)
		}
		if verbose {
			if rewrote {
				fmt.Printf("wrote %s\n", path)
			} else {
				fmt.Printf("%s is already current\n", path)
			}
		}
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
		lang := fs.String("lang", "cpp", "target language (c, cpp, cs, dart, elixir, go, java, js, rust)")
		out := fs.String("out", "generated", "output directory")
		fs.BoolVar(&verbose, "verbose", false, "list the files emitted")
		_ = fs.Parse(os.Args[2:]) // ExitOnError: Parse never returns an error
		c.TablesBaseline = true
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
	case "pack":
		// SPEC-TABLES.md §17: the tree IS the table, the text in it is §16's,
		// and the output is §3's — no envelope of schema's own.
		fs := flag.NewFlagSet("pack", flag.ExitOnError)
		root := fs.String("root", "", "the root `table` the tree mirrors")
		out := fs.String("out", "", "file to write the root's wire bytes to")
		tolerate := fs.Bool("tolerate", false, "exit 0 even when the report is not silent (see below)")
		fs.BoolVar(&verbose, "verbose", false, "print the read report even when it is silent, and name what the walk passed over")
		_ = fs.Parse(os.Args[2:]) // ExitOnError: Parse never returns an error
		dir, rest := packTree("pack", fs.Args())
		if *root == "" || *out == "" {
			fatalf("pack needs --root <Table> and --out <file>")
		}
		unit := loadTree(c, rest)
		bytes, skipped, report, err := c.Pack(unit, *root, dir)
		if err != nil {
			fail(err)
		}
		// the report decides the exit code, and a run that exits nonzero must
		// leave NO output behind: a `.bin` newer than its prerequisites makes
		// the next `make` skip the rule and the failure vanish. The gate runs
		// first, and the file lands atomically after it.
		reportLine(report, verbose, *tolerate)
		if err := writeAtomic(*out, bytes); err != nil {
			fatalf("%v", err)
		}
		if verbose {
			fmt.Printf("packed %s: %d bytes\n", *out, len(bytes))
			for _, name := range skipped {
				fmt.Printf("passed over %s: a hidden file that is not JSON\n", name)
			}
		}
	case "unpack":
		fs := flag.NewFlagSet("unpack", flag.ExitOnError)
		root := fs.String("root", "", "the root `table` the bytes carry")
		in := fs.String("in", "", "file holding the root's wire bytes")
		oneFile := fs.Bool("one-file", false, "write the root as one <Root>.json instead of a tree of fields")
		tolerate := fs.Bool("tolerate", false, "exit 0 even when the report is not silent (see below)")
		fs.BoolVar(&verbose, "verbose", false, "print the read report even when it is silent")
		_ = fs.Parse(os.Args[2:]) // ExitOnError: Parse never returns an error
		dir, rest := packTree("unpack", fs.Args())
		if *root == "" || *in == "" {
			fatalf("unpack needs --root <Table> and --in <file>")
		}
		wire, err := os.ReadFile(*in)
		if err != nil {
			fatalf("%v", err)
		}
		unit := loadTree(c, rest)
		unpack := c.Unpack
		if *oneFile {
			unpack = c.UnpackOneFile
		}
		report, err := unpack(unit, *root, wire, dir)
		if err != nil {
			fail(err)
		}
		reportLine(report, verbose, *tolerate)
		if verbose {
			fmt.Printf("unpacked %s into %s\n", *in, dir)
		}
	default:
		usage()
		os.Exit(2)
	}
}

// packTree splits pack's positional arguments: the FIRST is the tree, and
// anything after it names the unit's schema files. With nothing after it the
// unit is the working directory, which is where a schema tree usually sits
// beside its declarations.
func packTree(verb string, args []string) (string, []string) {
	if len(args) == 0 {
		fatalf("%s needs the directory the tree lives in", verb)
	}
	rest := args[1:]
	if len(rest) == 0 {
		rest = []string{"."}
	}
	return args[0], rest
}

// loadTree loads the unit for pack and unpack WITHOUT the CLI's format-in-place
// policy. Every other command formats the unit's sources because formatting is
// part of what it is doing to them; these two are pointed at a config tree and
// only READ the declarations, and a verb that assembles Config.bin rewriting
// the schema sources beside it is a surprise nobody asked for.
func loadTree(c *compiler.Compiler, args []string) *ir.Unit {
	format := c.FormatInPlace
	c.FormatInPlace = false
	defer func() { c.FormatInPlace = format }()
	return loadUnit(c, args)
}

// writeAtomic writes through a temporary beside the destination and renames it
// into place, so a run that never reaches this line leaves nothing at all and a
// half-written file is never observable.
func writeAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	// the temporary is removed on every failing path, so a refused run leaves
	// the directory exactly as it found it
	write := func() error {
		if _, err := tmp.Write(data); err != nil {
			_ = tmp.Close()
			return err
		}
		if err := tmp.Close(); err != nil {
			return err
		}
		if err := os.Chmod(name, 0o644); err != nil {
			return err
		}
		return os.Rename(name, path)
	}
	if err := write(); err != nil {
		_ = os.Remove(name)
		return err
	}
	return nil
}

// reportLine prints what a pack or unpack counted and decides the exit code. A
// silent report — the data matched the schema exactly — says nothing unless
// --verbose asks. Anything counted is printed and EXITS NONZERO: a value that
// was skipped, renamed away or cut down is a thing a build pipeline has to be
// able to fail on, and §17's "reported, never fatal" is about the walk not
// stopping, not about the tool's exit code. --tolerate is the way to say the
// report is expected.
func reportLine(r compiler.TableReport, verbose, tolerate bool) {
	if r.Silent() {
		if verbose {
			fmt.Println("report: silent — the data matched the schema exactly")
		}
		return
	}
	fmt.Printf("report: unknown %d, kind_mismatch %d, clamped %d, duplicate %d, malformed %v\n",
		r.Unknown, r.KindMismatch, r.Clamped, r.Duplicate, r.Malformed)
	if tolerate {
		return
	}
	fmt.Fprintln(os.Stderr, "schema: the report is not silent — pass --tolerate to accept it")
	os.Exit(1)
}

func usage() {
	fmt.Fprintf(os.Stderr, `usage:
  schema check      [--verbose] [dir|files...]
  schema generate   [--lang c|cpp|cs|dart|elixir|go|java|js|rust] [--out generated] [--verbose] [dir|files...]
  schema id         [dir|files...]
  schema projection [dir|files...]
  schema tables-baseline [--update --reason "..."] [--verbose] [dir|files...]
  schema fmt        [--verbose] [dir|files...]
  schema pack       --root <Table> --out <file> [--tolerate] [--verbose] <tree-dir> [dir|files...]
  schema unpack     --root <Table> --in  <file> [--one-file] [--tolerate] [--verbose] <tree-dir> [dir|files...]
  schema version

Every command formats the unit's schema files in place before processing them
(schemafmt — one style, no options); a file already in format is not touched.
pack and unpack are the exception — they read the declarations and never write
to them. Success is silent: --verbose lists the files a command wrote or
reformatted. pack and unpack exit nonzero when their read report is not
silent; --tolerate accepts it.
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
