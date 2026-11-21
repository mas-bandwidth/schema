package check

import (
	"go/build"
	"io"
	"maps"
	"slices"
	"sort"
	"strings"
)

// ---- names every target can express (SPEC §4.6, File-name safety and
// Platform-name safety) ----
//
// A unit `schema check` accepts generates code that builds and loads in every
// target (Rowan's ruling, 2026-11-21, on the name-shadowing survey: strict
// wins, the same in every language). A schema file's basename names a module,
// an import, a class file or an include guard in the generated code, and a
// declaration's name is a top-level symbol beside the names the generated code
// spells unqualified. The checks below refuse, for every target, each name
// one target cannot express, and the diagnostic names that target. The
// corpus that proves them is testdata/name-buildability: every row's verdict
// is held by TestNameBuildabilityCorpus here, and every `builds` row is
// generated and built in all 18 targets by the nightly tools/namebuild.

// checkFileNames refuses a schema file's basename that some target cannot
// spell as a module, an import or a file (SPEC §4.6, File-name safety), and
// two basenames of one unit that differ only by case. A basename the C
// standard-header rule already refuses (checkCHeaderFileName) takes that one
// diagnostic and no other.
func (c *checker) checkFileNames() {
	folded := map[string]string{}
	for _, f := range c.files {
		if !cStandardHeaders[strings.ToLower(f.Base)+".h"] {
			if why := basenameRefusal(f.Base); why != "" {
				c.errf(f.AST.PkgPos, "schema file %s: %s; rename the file (SPEC §4.6)", f.Name, why)
			}
		}
		key := strings.ToLower(f.Base)
		if prev, ok := folded[key]; ok {
			c.errf(f.AST.PkgPos, "schema files %s.schema and %s.schema differ only by case, and a case-insensitive file system (macOS, Windows) holds one file for the pair: C and C# write one header or source file for both, Go refuses the package, and Luau, moo, Python, Ruby and Rust refuse the unit; rename one (SPEC §4.6)",
				prev, f.Base)
			continue
		}
		folded[key] = f.Base
	}
}

// basenameRefusal returns why base cannot be a schema file's basename, naming
// the targets that cannot express it, or "" when every target can.
func basenameRefusal(base string) string {
	if !isIdentifier(base) {
		return base + " is not an identifier ([A-Za-z_][A-Za-z0-9_]*), and the basename is an include guard in C, a module in Elixir, Lua, Luau, moo, Python, Ruby and Rust, and an import in Goose and Lobster"
	}
	if base[0] == '_' {
		return base + " starts with an underscore: the go tool leaves a file whose name starts with _ out of the package (Go), and the generated Python package reserves the names (Python)"
	}
	if langs := basenameReservedLangs(base); len(langs) > 0 {
		return base + " is a reserved word in " + listReservedLangs(langs) + ", and the basename names a module, an import or a file the generated code refers to by name"
	}
	if why := hiddenBasename(base); why != "" {
		return why
	}
	if why := goConstraintBasename(base); why != "" {
		return why
	}
	return ""
}

// isIdentifier reports whether s is a schema identifier (SPEC §4.1:
// [A-Za-z_][A-Za-z0-9_]*).
func isIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		b := s[i]
		letter := b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
		if !letter && (i == 0 || b < '0' || b > '9') {
			return false
		}
	}
	return true
}

// basenameFoldingLangs are the languages that lowercase a basename into the
// module name they spell (Rust: `Enum.schema` is `mod enum`), so a basename
// is refused when its lowercase spelling is one of their reserved words.
// Lobster and Goose import a module by the basename as written (`import .Enum`
// builds in both: the survey's b_Enum, b_Struct, b_Type and b_Self rows), so
// they take the exact spelling alone.
var basenameFoldingLangs = []string{"Rust"}

// basenameReservedLangs returns the languages that reserve base as written,
// and those of basenameFoldingLangs that reserve its lowercase spelling.
func basenameReservedLangs(base string) []string {
	langs := slices.Clone(targetReserved[base])
	if low := strings.ToLower(base); low != base {
		for _, l := range basenameFoldingLangs {
			if slices.Contains(targetReserved[low], l) && !slices.Contains(langs, l) {
				langs = append(langs, l)
			}
		}
	}
	sort.Strings(langs)
	return langs
}

// hiddenName is one target's reason a basename is taken: its own module,
// its standard library, or the runtime it emits beside the generated modules.
type hiddenName struct {
	fold bool   // matched ignoring case (a case-insensitive file system, or a target that lowercases the basename)
	why  string // the targets that cannot express it and the mechanism; {} is the basename
}

// hiddenBasenames are the basenames that hide a standard module or the
// runtime in some target, refused for every target (SPEC §4.6), keyed by the
// spelling they match (lowercase where the entry folds). Each entry is the
// survey's measured failure or an emitter's own refusal at generate, lifted
// here so `schema check` refuses what `schema generate` would.
var hiddenBasenames = func() map[string][]hiddenName {
	m := map[string][]hiddenName{}
	put := func(fold bool, why string, names ...string) {
		for _, n := range names {
			if fold {
				n = strings.ToLower(n)
			}
			m[n] = append(m[n], hiddenName{fold: fold, why: why})
		}
	}
	put(false, `require("{}") answers with Lua's standard library, so the generated module is unreachable (Lua)`,
		"coroutine", "debug", "io", "os", "package", "table", "utf8", "string", "math")
	put(true, "Rust lowercases the basename into a module of the crate, and that module is the crate root, the binary entry module or a crate the generated code names (Rust)",
		"core", "std", "main", "lib")
	put(false, "Goose imports the module by its basename into a namespace where {} is the standard library, the entry point or a scalar type (Goose)",
		"std", "main", "bool", "i8", "i16", "i32", "i64", "u8", "u16", "u32", "u64", "f32", "f64")
	put(false, `require("./{}") resolves to the directory's own module, not the generated file (Luau, moo)`, "init")
	put(true, "the generated header would hide the runtime's serialize.h (C, C++), the module would hide the runtime crate (Rust), and a runtime module of that name is emitted beside the generated ones (JavaScript, Lua, Luau, moo, Goose, Lobster, Ruby)",
		"serialize")
	put(true, "the C emitter writes its shared runtime header SchemaRuntime.h beside the generated headers (C)", "SchemaRuntime")
	put(true, "the Elixir emitter writes its support module PacketRuntime.ex beside the generated modules (Elixir)", "PacketRuntime")
	put(true, "the Dart emitter writes its support library Schema.dart beside the generated libraries (Dart)", "Schema")
	put(true, "the Dart and Java emitters write their 128-bit support libraries Int128 and UInt128 beside a 128-bit unit's code (Dart, Java)", "Int128", "UInt128")
	put(true, "the Lobster emitter writes its runtime module descriptors.lobster beside the generated modules (Lobster)", "descriptors")
	put(false, "the generated Python package's __init__.py exports {}, which a module of that name would shadow (Python)", "InvalidValue", "PROTOCOL_ID", "Refused")
	return m
}()

// hiddenPrefixes are basename prefixes a runtime emits whole families under,
// matched ignoring case: a runtime that grows a file must not find a name
// that was free yesterday taken today.
var hiddenPrefixes = []struct{ prefix, why string }{
	{"serialize_", "the serialize runtime emits its modules as serialize_<part> beside the generated ones (Lua, Luau, moo, Goose, Lobster)"},
	{"schema_", "the runtime emits its modules as schema_<name> beside the generated ones (Squirrel, Luau, Lobster, Goose)"},
}

// hiddenBasename returns why base hides a standard module or the runtime in
// some target, or "".
func hiddenBasename(base string) string {
	var whys []string
	for _, h := range hiddenBasenames[base] {
		whys = append(whys, h.why)
	}
	if low := strings.ToLower(base); low != base {
		for _, h := range hiddenBasenames[low] {
			if h.fold {
				whys = append(whys, h.why)
			}
		}
	}
	for _, p := range hiddenPrefixes {
		if len(base) > len(p.prefix) && strings.EqualFold(base[:len(p.prefix)], p.prefix) {
			whys = append(whys, p.why)
		}
	}
	if len(whys) == 0 {
		return ""
	}
	for i, w := range whys {
		whys[i] = strings.ReplaceAll(w, "{}", base)
	}
	return base + " hides a module the generated code needs: " + strings.Join(whys, "; ")
}

// goConstraintBasename returns why the go tool would read the Go file named
// for base (<base>.go) as a build constraint rather than as a name, or "".
// It asks go/build itself, under a GOOS and GOARCH no suffix names, as the Go
// target's own refusal does (compiler/target_go.go): a `_test` suffix makes
// the file test-only, and a `_<goos>`, `_<goarch>` or `_<goos>_<goarch>`
// suffix limits it to that platform.
func goConstraintBasename(base string) string {
	name := base + ".go"
	if strings.HasSuffix(name, "_test.go") {
		return "the Go target writes " + name + ", and the go tool reads the _test suffix as a build constraint, so the code would not build into the package (Go)"
	}
	ctxt := build.Default
	ctxt.GOOS, ctxt.GOARCH = "schemanoos", "schemanoarch"
	ctxt.BuildTags, ctxt.ToolTags, ctxt.ReleaseTags = nil, nil, nil
	ctxt.CgoEnabled = false
	ctxt.JoinPath = func(elem ...string) string { return strings.Join(elem, "/") }
	ctxt.OpenFile = func(string) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader("package p\n")), nil
	}
	if ok, err := ctxt.MatchFile(".", name); err == nil && !ok {
		return "the Go target writes " + name + ", and the go tool reads a GOOS or GOARCH suffix in a file name as a build constraint, so the code would not build into the package (Go)"
	}
	return ""
}

// platformNames are declaration names a target's generated code spells
// unqualified and cannot qualify, refused for every target (SPEC §4.6,
// Platform-name safety): the value is the targets and the mechanism. A name
// the reserved-word table or the claimed-name registry already refuses is
// not repeated here.
var platformNames = func() map[string]string {
	m := map[string]string{}
	put := func(why string, names ...string) {
		for _, n := range names {
			if prev, ok := m[n]; ok {
				m[n] = prev + "; and " + why
				continue
			}
			m[n] = why
		}
	}
	put("a primitive type or crate the generated Rust names unqualified (Rust)",
		"u8", "u16", "u32", "u64", "u128", "usize", "i8", "i16", "i32", "i64", "i128", "isize", "f32", "f64", "str", "std", "core")
	put("a scalar type of the namespace every generated Goose module declares into (Goose)",
		"i8", "i16", "i32", "i64", "u8", "u16", "u32", "u64", "f32", "f64")
	put("a dart:typed_data class the generated Dart imports unprefixed (Dart)",
		"Int8List", "Uint8List", "Uint8ClampedList", "Int16List", "Uint16List", "Int32List", "Uint32List",
		"Int64List", "Uint64List", "Float32List", "Float64List", "Float32x4List", "Int32x4List", "Float64x2List")
	put("a package the generated Go imports, and a package-scope declaration of that name collides with the import (Go)",
		"atomic", "base64", "binary", "bytes", "errors", "fmt", "goruntime", "math", "slices", "strconv", "sync", "time", "utf8")
	put("the buffer library the generated code calls unqualified (Luau, moo)", "buffer")
	put("System.Diagnostics.Debug, which the generated C# calls unqualified (C#)", "Debug")
	put("the 128-bit support library the Dart and Java emitters write beside a 128-bit unit (Dart, Java)", "Int128", "UInt128")
	put("the support module the Elixir emitter writes beside the unit, Package.PacketRuntime (Elixir)", "PacketRuntime")
	return m
}()

// checkPlatformNames refuses a declaration named like a platformNames entry
// (SPEC §4.6, Platform-name safety). A name the reserved-word check already
// refused takes that diagnostic alone.
func (c *checker) checkPlatformNames() {
	for _, name := range slices.Sorted(maps.Keys(c.astDecls)) {
		why, ok := platformNames[name]
		if !ok || len(targetReserved[name]) > 0 {
			continue
		}
		d := c.astDecls[name]
		c.errf(d.DeclPos(), "declaration name %q is %s — rename at the source (SPEC §4.6)", name, why)
	}
}
