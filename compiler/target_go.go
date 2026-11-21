// One built-in target, registered from its own init so a target is one file
// and a new one adds a file (docs/CONTRIBUTING.md, "Adding a language").
package compiler

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/gotable"
	"github.com/mas-bandwidth/schema/v2/internal/goname"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// goTarget emits Go.
type goTarget struct{}

func (goTarget) Names() []string { return []string{"go"} }

// Generate is the NO-LINEAGE case: a unit whose lock the caller did not read,
// or that has none. Every fixed table then carries the single entry it can
// always compute — its own (docs/FIXED-FORM-ALGORITHM.md §5.9 #1).
func (t goTarget) Generate(u *ir.Unit, opts Options) (map[string][]byte, error) {
	return t.GenerateLineage(u, opts, nil)
}

// GenerateLineage is the second entry point of §5.9 #1: the driver read the
// unit's lock once (compiler/lineage.go) and hands the lineage down as DATA, so
// gotable opens no file and a build ships a reader for every layout the lock
// records.
func (goTarget) GenerateLineage(u *ir.Unit, opts Options, lineage *FixedLineage) (map[string][]byte, error) {
	// the shared runtime (docs/SPEC-TABLES.md §8.4): absent, empty or "shared"
	// is the default and imports tables/goruntime instead of emitting the
	// table-independent machinery; "standalone" is the named option and changes
	// no byte from every build before the option existed
	rt, err := gotable.ParseRuntime(opts["runtime"])
	if err != nil {
		return nil, err
	}
	if err := refuseUnported(u, "go"); err != nil {
		return nil, err
	}
	// the surface list (docs/SPEC-TABLES.md §8.8): the default full set is
	// byte-identical to today's output
	surfaces, err := gotable.ParseSurfaces(opts["surface"])
	if err != nil {
		return nil, err
	}
	files, err := golang.Generate(u)
	if err != nil {
		return nil, err
	}
	// units that declare tables ALSO get <Base>Table.go per file — the
	// TABLE-wire codecs and the reflection descriptors (SPEC-TABLES.md); a
	// table-free unit's output is byte-identical to what the packet emitter
	// alone produces
	tables, err := gotable.GenerateRuntimeSurface(u, goTableLineage(u, lineage), surfaces, rt)
	if err != nil {
		return nil, err
	}
	for name, data := range tables {
		if _, dup := files[name]; dup {
			return nil, fmt.Errorf("generated file %s is claimed twice — a schema file named <X>.schema beside <X minus Table>.schema with tables collides on the Table source; rename one file (SPEC-TABLES.md)", name)
		}
		files[name] = data
	}
	if err := refuseGoConstraintNames(u, files); err != nil {
		return nil, err
	}
	// every identifier takes Go's initialisms, across the packet and table
	// files together (internal/goname)
	if err := goname.RespellFiles(files); err != nil {
		return nil, fmt.Errorf("respelling the generated Go: %w", err)
	}
	return files, nil
}

// refuseGoConstraintNames refuses an output file name the go tool reads as a
// build constraint rather than as a name: a leading `_` or `.` drops the file
// from the package, a `_test` suffix makes it test-only, and a `_<goos>`,
// `_<goarch>` or `_<goos>_<goarch>` suffix limits it to that platform (the go
// command's "Build constraints", file names). Every output is named from a
// schema file's basename (<base>.go, <base>Table.go, ...), so the refusal
// names the schema file to rename.
func refuseGoConstraintNames(u *ir.Unit, files map[string][]byte) error {
	for _, name := range slices.Sorted(maps.Keys(files)) {
		why := goConstraintName(name)
		if why == "" {
			continue
		}
		// the schema file it is named from: the longest basename it starts with
		base := ""
		for _, f := range u.Files {
			if strings.HasPrefix(name, f.Base) && len(f.Base) > len(base) {
				base = f.Base
			}
		}
		if base == "" {
			return fmt.Errorf("the Go target writes %s, and the go tool reads %s in a file name as a build constraint — a compiler bug, not a schema error", name, why)
		}
		return fmt.Errorf("schema file %s.schema: the Go target writes %s, and the go tool reads %s in a file name as a build constraint, so that code would not build into the package; rename the schema file", base, name, why)
	}
	return nil
}

// goConstraintName answers what about a .go file name the go tool reads as a
// build constraint, or "" for a plain name. It follows go/build: the leading
// `_` or `.`, then the name up to its first `.`, with everything before its
// first `_` ignored and the `_`-separated parts matched exactly.
func goConstraintName(name string) string {
	if strings.HasPrefix(name, "_") || strings.HasPrefix(name, ".") {
		return "a leading " + name[:1]
	}
	stem, _, _ := strings.Cut(name, ".")
	i := strings.Index(stem, "_")
	if i < 0 {
		return ""
	}
	parts := strings.Split(stem[i:], "_")
	n := len(parts)
	if parts[n-1] == "test" {
		return "the _test suffix"
	}
	if n >= 3 && goKnownOS[parts[n-2]] && goKnownArch[parts[n-1]] {
		return "the _" + parts[n-2] + "_" + parts[n-1] + " suffix"
	}
	if goKnownOS[parts[n-1]] || goKnownArch[parts[n-1]] {
		return "the _" + parts[n-1] + " suffix"
	}
	return ""
}

// goKnownOS and goKnownArch are the go tool's file-name GOOS and GOARCH lists
// (internal/syslist), including the names it reserves without supporting.
var goKnownOS = setOf("aix", "android", "darwin", "dragonfly", "freebsd", "hurd", "illumos", "ios", "js", "linux", "nacl", "netbsd", "openbsd", "plan9", "solaris", "wasip1", "windows", "zos")

var goKnownArch = setOf("386", "amd64", "amd64p32", "arm", "armbe", "arm64", "arm64be", "loong64", "mips", "mipsle", "mips64", "mips64le", "mips64p32", "mips64p32le", "ppc", "ppc64", "ppc64le", "riscv", "riscv64", "s390", "s390x", "sparc", "sparc64", "wasm")

func setOf(names ...string) map[string]bool {
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = true
	}
	return m
}

// goTableLineage is the lock's lineage in the Go backend's own spelling. Nil in,
// nil out: a unit with no lock hands nothing.
func goTableLineage(u *ir.Unit, lineage *FixedLineage) map[string][]gotable.FixedLineageEntry {
	if lineage == nil {
		return nil
	}
	out := map[string][]gotable.FixedLineageEntry{}
	for _, st := range ir.TableFixedRoots(u) {
		entries := lineage.Entries(st.Name)
		if len(entries) == 0 {
			continue
		}
		for _, e := range entries {
			out[st.Name] = append(out[st.Name], gotable.FixedLineageEntry{
				Wire:    e.Wire,
				Layout:  e.Layout,
				Record:  e.Record,
				Retired: e.Retired,
				Reason:  e.Reason,
				Ranges:  ir.TableFixedWriterRanges(e.Writer),
			})
		}
	}
	return out
}

func init() {
	registerBuiltin(goTarget{}, true, true, true, true)
	registerOptionalArrayCarrier("go")
	registerListCarrier("go")
	registerMapCarrier("go")
	valueDefaultTargets = append(valueDefaultTargets, "go")
	wasRowTargets = append(wasRowTargets, "go")
	registerPacketValueDefaultCarrier("go")
}
