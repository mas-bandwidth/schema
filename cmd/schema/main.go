// The schema compiler CLI (SPEC §7).
//
//	schema check      [dir|files...]                 parse + typecheck; exit code for CI
//	schema generate   --lang cpp --out <dir> [dir]   emit generated code
//	schema id         [dir|files...]                 print the protocol id
//	schema projection [dir|files...]                 print the wire shape the id hashes
//	schema build-version [--facts] [dir|files...]    print the build version, or the cook projection it hashes
//	schema tables-baseline [--update --reason "..."]  print or move the tables baseline
//	schema fmt        [dir|files...]                 canonicalize schema files in place — the ONLY command that writes one
//	schema pack       --root T --out F <dir>         a directory tree becomes one table's wire bytes
//	schema unpack     --root T --in  F <dir>         the wire bytes become the tree again
//	schema cook       --root T --in F --out C        the wire becomes the cooked form (§7)
//	schema cook-check <file.cook>                    validate an untrusted cook, offline
//	schema uncook     --root T --in C --out F        the cook becomes the wire again
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
	"strings"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	// The CLI's load policy: `fmt` is the ONLY command that writes a schema
	// file. Every other command reads the unit as it sits on disk and leaves
	// it byte for byte alone, so a read-only checkout, a sandboxed build, a
	// concurrent generation and an editor integration all work, and a command
	// that answers a question never edits the source it answered about.
	// Formatting changes no answer — the unit a canonical file declares is the
	// unit its unformatted twin declares, and the protocol id depends only on
	// the wire shape (SPEC §3.1) — so nothing here needs the repair to be on
	// disk. Success is silent; --verbose announces the files a command emitted,
	// and errors always reach stderr.
	c := compiler.New()
	verbose := false // set by each subcommand's --verbose flag before any file is loaded
	// Warnings are never quiet and never gated on --verbose: a warning nobody
	// reads is a warning that does not exist. Today they come from the tables
	// baseline's warn class (docs/SPEC-TABLES.md §18.2).
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
		paths, err := compiler.GatherPaths(fs.Args())
		if err != nil {
			fail(err)
		}
		unit, err := c.Load(paths)
		if err != nil {
			fail(err)
		}
		// THE NO-BASELINE NOTICE (docs/SPEC-TABLES.md §18.1). The baseline is
		// opt-in, so a unit that declares tables and never committed one is
		// unguarded against every edit §4.1 marks silent — and nothing else in
		// the tool would ever mention it. One line, on stderr, and the exit
		// code is untouched: it is a nudge, not a gate, and committing a
		// baseline silences it.
		if notice := compiler.TablesBaselineNudge(unit, paths); notice != "" {
			fmt.Fprintln(os.Stderr, "notice: "+notice)
		}
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
	case "build-version":
		// THE BUILD VERSION (docs/SPEC-TABLES.md §20.7): everything cooked or
		// blocked is keyed by it, and the store's tuple is (asset hash, build
		// version). --facts prints the COOK PROJECTION it hashes, in the
		// tradition of `schema projection`: the facts are printable, readable
		// and diffable, or a fact missing from them is invisible.
		fs := flag.NewFlagSet("build-version", flag.ExitOnError)
		facts := fs.Bool("facts", false, "print the cook projection the build version hashes")
		_ = fs.Parse(os.Args[2:]) // ExitOnError: Parse never returns an error
		unit := loadUnit(c, fs.Args())
		if *facts {
			fmt.Print(ir.CookProjection(unit))
			break
		}
		fmt.Printf("0x%016x\n", ir.BuildVersion(unit))
	case "tables-baseline":
		// The TABLES BASELINE (docs/SPEC-TABLES.md §18). Printing is the default:
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
		// THE ONE WRITER (SPEC §7.4). Every other command reads the unit and
		// leaves it alone, so canonicalizing a tree is this command's job and
		// nobody else's: editors, pre-commit hooks and `make fmt` run it, and a
		// pipeline that wants the repair asks for it here.
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
		lang := fs.String("lang", "cpp", "target language ("+strings.Join(c.Targets(), ", ")+")")
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
		// docs/SPEC-TABLES.md §17: the tree IS the table, the text in it is §16's,
		// and the output is §3's — no envelope of schema's own.
		fs := flag.NewFlagSet("pack", flag.ExitOnError)
		root := fs.String("root", "", "the root `table` the tree mirrors")
		out := fs.String("out", "", "file to write the root's wire bytes to")
		tolerate := fs.Bool("tolerate", false, "exit 0 even when the report is not silent (see below)")
		// THE MESSAGE FORM (docs/SPEC-TABLES.md §3.3): the same tree and the
		// same text, written as the form byte and the root body alone. Its ids
		// live in the CONNECTION's announced table, so `--announce <file>`
		// writes that announcement beside the message — a receiver that has
		// not read one holds no table and refuses every message on the wire.
		message := fs.Bool("message", false, "write the `message` form (docs/SPEC-TABLES.md §3.3) instead of the file form")
		announce := fs.String("announce", "", "with --message, also write the unit's announcement to this `file`")
		fs.BoolVar(&verbose, "verbose", false, "print the read report even when it is silent, and name what the walk passed over")
		_ = fs.Parse(os.Args[2:]) // ExitOnError: Parse never returns an error
		dir, rest := packTree("pack", fs.Args())
		if *root == "" || *out == "" {
			fatalf("pack needs --root <Table> and --out <file>")
		}
		if *announce != "" && !*message {
			fatalf("--announce is the message form's own: pass --message beside it (docs/SPEC-TABLES.md §3.3)")
		}
		unit := loadUnit(c, rest)
		pack := c.Pack
		if *message {
			pack = c.PackMessage
		}
		bytes, skipped, report, err := pack(unit, *root, dir)
		if err != nil {
			fail(err)
		}
		if *announce != "" {
			if err := writeAtomic(*announce, c.Announce(unit)); err != nil {
				fatalf("%v", err)
			}
			if verbose {
				fmt.Printf("announced %s: %d bytes\n", *announce, len(c.Announce(unit)))
			}
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
		// A MESSAGE'S TABLE IS SOMEWHERE ELSE (docs/SPEC-TABLES.md §3.3), so
		// reading one takes the announcement that carried it: a message stored
		// on its own is not readable, and this flag is where the other half
		// comes from.
		announce := fs.String("announce", "", "read --in as the `message` form against the announcement in this file (docs/SPEC-TABLES.md §3.3)")
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
		unit := loadUnit(c, rest)
		var report compiler.TableReport
		if *announce != "" {
			announcement, aerr := os.ReadFile(*announce)
			if aerr != nil {
				fatalf("%v", aerr)
			}
			report, err = c.UnpackMessage(unit, *root, announcement, wire, dir, *oneFile)
		} else {
			unpack := c.Unpack
			if *oneFile {
				unpack = c.UnpackOneFile
			}
			report, err = unpack(unit, *root, wire, dir)
		}
		if err != nil {
			fail(err)
		}
		reportLine(report, verbose, *tolerate)
		if verbose {
			fmt.Printf("unpacked %s into %s\n", *in, dir)
		}
	case "cook":
		// docs/SPEC-TABLES.md §7: the wire in, the COOKED FORM out — the header, the
		// region written verbatim with the root at its base, and the node
		// directory beside it for the tool. TOOLING BUILDS; THE GAME POINTS.
		fs := flag.NewFlagSet("cook", flag.ExitOnError)
		root := fs.String("root", "", "the root `table` the bytes carry")
		in := fs.String("in", "", "the wire file to cook, or a directory tree to pack and then cook")
		out := fs.String("out", "", "file to write the cook to")
		byteOrder := fs.String("byte-order", "little", "the byte order the cook is produced in: little or big")
		attribution := fs.String("attribution", "", "write the attribution part to this file instead of into the cook, which then carries data alone")
		tolerate := fs.Bool("tolerate", false, "exit 0 even when the wire report is not silent")
		fs.BoolVar(&verbose, "verbose", false, "print the cook's header facts and the wire report")
		_ = fs.Parse(os.Args[2:]) // ExitOnError: Parse never returns an error
		if *root == "" || *in == "" || *out == "" {
			fatalf("cook needs --root <Table>, --in <file|dir> and --out <file>")
		}
		var opts compiler.CookOptions
		switch *byteOrder {
		case "little":
		case "big":
			opts.Big = true
		default:
			fatalf("--byte-order takes little or big, not %q", *byteOrder)
		}
		unit := loadUnit(c, fs.Args())
		wire, packed := cookInput(c, unit, *root, *in, verbose, *tolerate)
		bytes, cooked, report, err := c.Cook(unit, *root, wire, opts)
		if err != nil {
			fail(err)
		}
		// TWO READS, TWO REPORTS, EACH NAMED. `cook --in <dir>` packs the tree
		// and then cooks the wire, so it reads twice and reports twice — and
		// two identical unlabelled lines read as the cook having read one
		// input twice (#521 G-12). With a wire file for --in there is one read
		// and the line stays unlabelled.
		stage := ""
		if packed {
			stage = "cook"
		}
		reportLineStage(report, verbose, *tolerate, stage)
		if *attribution != "" {
			// the attribution is SEPARABLE (§6.3): a caller may place the two
			// parts together or apart, and a build that ships no tooling need
			// not carry the directory at all — the header then records its
			// length as zero and the file is just data (§7)
			data, side, err := compiler.CookSplitAttribution(bytes)
			if err != nil {
				fail(err)
			}
			if err := writeAtomic(*attribution, side); err != nil {
				fatalf("%v", err)
			}
			bytes = data
		}
		if err := writeAtomic(*out, bytes); err != nil {
			fatalf("%v", err)
		}
		if verbose {
			fmt.Printf("cooked %s: %d bytes, build version 0x%016x, %s-endian, root %s, %d nodes, %d data bytes, %d attribution bytes\n",
				*out, len(bytes), cooked.BuildVersion, cooked.ByteOrder, cooked.Root, cooked.Nodes, cooked.DataBytes, cooked.AttribBytes)
			if *attribution != "" {
				fmt.Printf("wrote %s: %d attribution bytes, and the cook carries data alone\n", *attribution, cooked.AttribBytes)
			}
		}
	case "cook-check":
		// THE OFFLINE VALIDATOR (§7): a person's decision to run, never a
		// parameter on a load. The runtime keeps one `Open` — match the header
		// and point — and this is where a doubted file is checked, DATA against
		// ATTRIBUTION, following no reference and decoding no value.
		fs := flag.NewFlagSet("cook-check", flag.ExitOnError)
		root := fs.String("root", "", "the root `table` the cook must carry")
		attribution := fs.String("attribution", "", "read the attribution part from this file, for a cook that carries data alone")
		fs.BoolVar(&verbose, "verbose", false, "print what the check proved")
		_ = fs.Parse(os.Args[2:]) // ExitOnError: Parse never returns an error
		args := fs.Args()
		if len(args) == 0 {
			fatalf("cook-check needs the cooked file")
		}
		file, err := os.ReadFile(args[0])
		if err != nil {
			fatalf("%v", err)
		}
		if *attribution != "" {
			side, err := os.ReadFile(*attribution)
			if err != nil {
				fatalf("%v", err)
			}
			if file, err = compiler.CookJoinAttribution(file, side); err != nil {
				fail(err)
			}
		}
		rest := args[1:]
		if len(rest) == 0 {
			rest = []string{"."}
		}
		unit := loadUnit(c, rest)
		res, err := c.CookCheck(unit, *root, file)
		if err != nil {
			fail(err)
		}
		if verbose {
			fmt.Printf("ok: build version 0x%016x, %s-endian, root %s, %d nodes, %d data bytes, %d attribution bytes, %d reference slots\n",
				res.BuildVersion, res.ByteOrder, res.Root, res.Nodes, res.DataBytes, res.AttribBytes, res.Pointers)
		}
	case "uncook":
		// The cook back onto the TOLERANT WIRE, which is the format of record
		// (§7). It is the round-trip proof and a tool's path; a runtime points
		// at a cook where it lies and never does this.
		fs := flag.NewFlagSet("uncook", flag.ExitOnError)
		root := fs.String("root", "", "the root `table` the cook carries")
		in := fs.String("in", "", "the cooked file")
		out := fs.String("out", "", "file to write the root's wire bytes to")
		attribution := fs.String("attribution", "", "read the attribution part from this file, for a cook that carries data alone")
		fs.BoolVar(&verbose, "verbose", false, "name the file written")
		_ = fs.Parse(os.Args[2:]) // ExitOnError: Parse never returns an error
		if *root == "" || *in == "" || *out == "" {
			fatalf("uncook needs --root <Table>, --in <file> and --out <file>")
		}
		file, err := os.ReadFile(*in)
		if err != nil {
			fatalf("%v", err)
		}
		if *attribution != "" {
			side, err := os.ReadFile(*attribution)
			if err != nil {
				fatalf("%v", err)
			}
			if file, err = compiler.CookJoinAttribution(file, side); err != nil {
				fail(err)
			}
		}
		unit := loadUnit(c, fs.Args())
		wire, err := c.Uncook(unit, *root, file)
		if err != nil {
			fail(err)
		}
		if err := writeAtomic(*out, wire); err != nil {
			fatalf("%v", err)
		}
		if verbose {
			fmt.Printf("uncooked %s into %s: %d wire bytes\n", *in, *out, len(wire))
		}
	default:
		usage()
		os.Exit(2)
	}
}

// cookInput is `cook --in`: a wire file, or a DIRECTORY TREE, which is packed
// first through the existing engine (§17) so one command covers the pipeline
// the owner describes — tooling does the build, then cooks to the rad binary
// format, and the game just points at it and works.
// It answers whether it PACKED, which is whether the command read twice: the
// caller labels its own report when it did.
func cookInput(c *compiler.Compiler, unit *ir.Unit, root, in string, verbose, tolerate bool) ([]byte, bool) {
	info, err := os.Stat(in)
	if err != nil {
		fatalf("%v", err)
	}
	if !info.IsDir() {
		wire, err := os.ReadFile(in)
		if err != nil {
			fatalf("%v", err)
		}
		return wire, false
	}
	wire, skipped, report, err := c.Pack(unit, root, in)
	if err != nil {
		fail(err)
	}
	reportLineStage(report, verbose, tolerate, "pack")
	if verbose {
		for _, name := range skipped {
			fmt.Printf("passed over %s: a hidden file that is not JSON\n", name)
		}
	}
	return wire, true
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
	reportLineStage(r, verbose, tolerate, "")
}

// reportLineStage is reportLine with the READ named. A command that reads once
// leaves the stage empty and prints "report:"; `cook --in <dir>` packs and then
// cooks, so it names "pack" and "cook" and the two lines are told apart.
func reportLineStage(r compiler.TableReport, verbose, tolerate bool, stage string) {
	label := "report"
	if stage != "" {
		label = "report (" + stage + ")"
	}
	if r.Silent() {
		if verbose {
			fmt.Println(label + ": silent — the data matched the schema exactly")
		}
		return
	}
	fmt.Printf("%s: unknown %d, kind_mismatch %d, clamped %d, duplicate %d, malformed %v\n",
		label, r.Unknown, r.KindMismatch, r.Clamped, r.Duplicate, r.Malformed)
	if tolerate {
		return
	}
	fmt.Fprintln(os.Stderr, "schema: the report is not silent — pass --tolerate to accept it")
	os.Exit(1)
}

func usage() {
	fmt.Fprintf(os.Stderr, `usage:
  schema check      [--verbose] [dir|files...]
  schema generate   [--lang <target>] [--out generated] [--verbose] [dir|files...]
  schema id         [dir|files...]
  schema projection [dir|files...]
  schema build-version [--facts] [dir|files...]
  schema tables-baseline [--update --reason "..."] [--verbose] [dir|files...]
  schema fmt        [--verbose] [dir|files...]
  schema pack       --root <Table> --out <file> [--tolerate] [--verbose] <tree-dir> [dir|files...]
  schema unpack     --root <Table> --in  <file> [--one-file] [--tolerate] [--verbose] <tree-dir> [dir|files...]
  schema cook       --root <Table> --in  <file|tree-dir> --out <file> [--byte-order little|big] [--attribution <file>] [--tolerate] [--verbose] [dir|files...]
  schema cook-check [--root <Table>] [--attribution <file>] [--verbose] <file.cook> [dir|files...]
  schema uncook     --root <Table> --in  <file.cook> --out <file> [--attribution <file>] [--verbose] [dir|files...]
  schema version

fmt is the only command that writes a schema file (schemafmt — one style, no
options); a file already in format is not touched. Every other command reads
the unit as it sits on disk and leaves it alone, so a read-only tree works and
a check never edits what it checked. Success is silent: --verbose lists the
files a command wrote. pack and unpack exit nonzero when their read report is
not silent; --tolerate accepts it.
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
