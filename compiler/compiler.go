// Package compiler is schema's public driver: it turns *.schema paths into a
// checked unit ([ir.Unit]) and hands that unit to a registered generator. It
// is the whole compiler as a library — cmd/schema is its first client and
// imports nothing else from this module except [ir].
//
// The two steps are separate on purpose. [Compiler.Load] does the loud part
// (formatting, parsing, checking, the protocol id) and returns either a unit
// or the diagnostics; [Compiler.Generate] is pure — a unit in, the emitted
// file bytes out, nothing written; writing is the caller's, so an embedder can
// put the output in a build directory, a zip, or memory. Registration
// ([Compiler.Register]) is the only door into Generate, and the nine built-in
// backends come through it like anyone else's would.
//
// The whole surface is under semver from the release that first carries it.
package compiler

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/baseline"
	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/format"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/version"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// A Compiler is one driver instance: the generator registry plus the load
// policy the CLI and an embedder differ on. Use [New] — it returns a Compiler
// with the nine built-in generators registered.
//
// Register and the two policy fields configure a Compiler; do that before use
// and from one goroutine. Once configured it is read-only, and Load and
// Generate may run concurrently — with the caveat that FormatInPlace writes to
// the input tree, so concurrent loads must not name the same files.
type Compiler struct {
	// FormatInPlace canonicalizes every schema file on disk before parsing it
	// (schemafmt, SPEC §7.4), which is what every CLI command does. An
	// embedder that must not write to its input tree leaves this false; the
	// unit is identical either way — formatting never changes what a file
	// declares, and the protocol id depends only on the wire shape (SPEC
	// §3.1), never on source bytes.
	FormatInPlace bool

	// OnFormat, when set, is called with the path of each file FormatInPlace
	// actually rewrote. The CLI prints "formatted <path>"; a build tool might
	// stage the file, or fail the build.
	OnFormat func(path string)

	// TablesBaseline turns on the TABLES BASELINE check (SPEC-TABLES.md §18.2):
	// when a tables.baseline sits in the unit's directory, Load diffs the
	// unit's table closure against it and REFUSES the edits the table wire
	// cannot report — a changed specified default, a moved flags bit, a
	// changed field kind. No file means no check. The CLI sets it for `check`
	// and `generate`.
	TablesBaseline bool

	// OnWarn, when set, receives each non-fatal report a load produced — the
	// baseline's warn class (a shrunk bound, a removed enum variant or union
	// arm): data survives, something is lost, and the runtime read report
	// counts it. The CLI writes them to stderr.
	OnWarn func(msg string)

	gens      map[string]Generator // every accepted --lang spelling
	canonical []string             // one per registered generator, its first name
}

// New returns a driver with the built-in generators registered — c, cpp, cs
// (csharp), dart, elixir, go, java, js (javascript) and rust — and the load
// policy of a library: no writes to the input tree. Set
// [Compiler.FormatInPlace] for the CLI's behavior.
func New() *Compiler {
	c := &Compiler{}
	for _, g := range builtins() {
		if err := c.Register(g); err != nil {
			// The built-in set is fixed at compile time, so a failure here is
			// a defect in this package, not a caller's mistake.
			panic("compiler: registering a built-in generator: " + err.Error())
		}
	}
	return c
}

// Diagnostics is a compilation's error list — every parse and check error the
// unit produced, in report order, rather than the first one. Load returns it
// as the error when the unit does not compile; a caller that wants to print
// them all reaches it with errors.AsType.
type Diagnostics []error

// Error joins the diagnostics one per line.
func (d Diagnostics) Error() string {
	msgs := make([]string, 0, len(d))
	for _, e := range d {
		msgs = append(msgs, e.Error())
	}
	return strings.Join(msgs, "\n")
}

// Load gathers the given *.schema files into one compilation unit (SPEC §3.2):
// it formats each file when FormatInPlace is set, parses it, and checks the
// unit — name resolution, constant folding, the shape rules, and the protocol
// id. The paths are the unit's files in the order [GatherPaths] returns them;
// the checker sorts them by basename itself, so the unit is deterministic
// whatever order the paths arrive in.
//
// A unit that does not compile returns [Diagnostics] holding every parse and
// check error. Everything else — an unreadable file, a file the formatter
// refuses, two files with the same basename — returns a single error, because
// there is nothing more to report after it.
func (c *Compiler) Load(paths []string) (*ir.Unit, error) {
	seen := map[string]string{}
	var files []check.SourceFile
	var diags Diagnostics
	for _, p := range paths {
		name := filepath.Base(p)
		if prev, dup := seen[name]; dup {
			return nil, fmt.Errorf("duplicate basename %s (%s and %s) — a unit's files are identified by basename, and generated files are named by it; rename one", name, prev, p)
		}
		seen[name] = p
		data, err := c.read(p)
		if err != nil {
			return nil, err
		}
		f, perrs := parser.Parse(p, data)
		diags = append(diags, perrs...)
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
	if len(diags) > 0 {
		return nil, diags
	}
	u, cerrs := check.Unit(files)
	if len(cerrs) > 0 {
		return nil, Diagnostics(cerrs)
	}
	if c.TablesBaseline {
		warns, berrs := baseline.Check(u, paths)
		for _, w := range warns {
			if c.OnWarn != nil {
				c.OnWarn(w)
			}
		}
		if len(berrs) > 0 {
			return nil, Diagnostics(berrs)
		}
	}
	return u, nil
}

// read returns the bytes the unit is compiled from: the canonical form when
// FormatInPlace is set (rewriting the file and announcing it), the file as it
// sits otherwise.
func (c *Compiler) read(path string) ([]byte, error) {
	if !c.FormatInPlace {
		return os.ReadFile(path)
	}
	data, rewrote, err := FormatFile(path)
	if err != nil {
		return nil, err
	}
	if rewrote && c.OnFormat != nil {
		c.OnFormat(path)
	}
	return data, nil
}

// GatherPaths collects one unit's *.schema files from the given arguments:
// directories contribute their schema files, a named file contributes itself,
// and no arguments means the working directory (one directory is one unit —
// SPEC §3.2). The order is the caller's argument order; the checker sorts by
// basename itself.
func GatherPaths(args []string) ([]string, error) {
	if len(args) == 0 {
		args = []string{"."}
	}
	var paths []string
	for _, arg := range args {
		info, err := os.Stat(arg)
		if err != nil {
			return nil, err
		}
		if info.IsDir() {
			entries, err := os.ReadDir(arg)
			if err != nil {
				return nil, err
			}
			for _, e := range entries {
				// *.schema means a nonempty basename before the extension — a
				// bare dotfile named ".schema" is not a schema file
				if !e.IsDir() && strings.HasSuffix(e.Name(), ".schema") && len(e.Name()) > len(".schema") {
					paths = append(paths, filepath.Join(arg, e.Name()))
				}
			}
		} else {
			name := filepath.Base(arg)
			if !strings.HasSuffix(name, ".schema") || len(name) == len(".schema") {
				return nil, fmt.Errorf("%s: only *.schema files form a unit (SPEC §3.2)", arg)
			}
			paths = append(paths, arg)
		}
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("no .schema files found (a unit with no schema files is an error — SPEC §4.6)")
	}
	return paths, nil
}

// Format canonicalizes one schema file's bytes and returns them (SPEC §7.4).
// It touches no files: path names the source only, for diagnostics. A drift
// gate that must not repair what it measures uses this; a tool that wants the
// repair uses [FormatFile].
func Format(path string, src []byte) ([]byte, error) {
	return format.Format(path, src)
}

// FormatFile canonicalizes one schema file in place (SPEC §7.4), writing only
// when the bytes actually change. It returns the canonical bytes and whether
// the file on disk was rewritten.
func FormatFile(path string) (canonical []byte, rewrote bool, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false, err
	}
	out, err := format.Format(path, data)
	if err != nil {
		return nil, false, err
	}
	if string(out) == string(data) {
		return out, false, nil
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		return nil, false, err
	}
	return out, true, nil
}

// MigrateFile is the ONE-SHOT migration mode (SPEC §7.4 rule 3b): it
// additionally accepts the retired spellings — the trailing [ ... ]
// attribute block (default after the attributes) and the [<= N] bound —
// rewrites them into the current grammar, formats, and writes the file back
// when the bytes change. Plain formatting refuses retired spellings exactly
// as the compiler does.
func MigrateFile(path string) (canonical []byte, rewrote bool, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false, err
	}
	out, err := format.Migrate(path, data)
	if err != nil {
		return nil, false, err
	}
	if string(out) == string(data) {
		return out, false, nil
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		return nil, false, err
	}
	return out, true, nil
}

// UserAgent reports which build of the compiler is running, as the one-line
// form `schema version` prints and the form to quote in a bug report: the
// linker stamp if the build carried one, otherwise the module version or a
// VCS revision. Deliberately absent from generated output — generated code
// carries the protocol id, which is what actually governs compatibility (see
// VERSIONING.md).
func UserAgent() string { return version.UserAgent() }
