package rusttable

// DESCRIPTORS ARE CONSTANTS (docs/PORTING.md M13): every reflection descriptor
// the Rust leg emits — the block form's TableBlockInfo, the cook's
// TableCookInfo, the view's TableTypeInfo/TableFieldInfo/TableUnitView and the
// card registry's TableDesc — is a plain `static` of `&'static` data, read
// without allocation, initialized with no lazy path and immutable. The two
// FACTORIES are named: a block field's `element` and a cook field's `record`
// column hold `fn() -> &'static …` so a record that reaches itself (a `*Node`
// in Node) stays a constant table; the function they name is an accessor that
// returns a function-local `static` and does nothing else. The one LazyLock in
// the output is the fixed form's per-lineage READ PLANS (`<T>_FIXED_PLANS`,
// docs/SPEC-TABLES.md §3.4 and docs/FIXED-FORM-ALGORITHM.md §5.9 #3), which
// are compiled from the lock's layouts and are not descriptors; a LazyLock is
// safely published, so even it is not the banned plain cache.

import (
	"fmt"
	"maps"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

var (
	// the one LazyLock the output may carry, and it is not a descriptor.
	rustPlanLazyLock = regexp.MustCompile(`^static [A-Z0-9_]+_FIXED_PLANS: std::sync::LazyLock<[A-Za-z0-9_]+FixedPlans> = std::sync::LazyLock::new\(\|\| \{$`)
	// a descriptor accessor: `pub fn x() -> &'static TableBlockInfo {` and kin.
	rustDescriptorAccessor = regexp.MustCompile(`^(\s*)pub fn ([a-z0-9_]+)\(\) -> &'static (TableBlockInfo|TableCookInfo|TableUnitView) \{$`)
	// a column holding a function pointer to a descriptor.
	rustFactoryColumn = regexp.MustCompile(`Option<fn\(\) -> &'static [A-Za-z0-9_]+>`)
)

// the publication forms a descriptor must never take: a lazy cell, a mutable
// static (the plain cache: read it, build on None, store back), interior
// mutability, or a lock.
var rustLazyTokens = []string{
	"OnceLock", "OnceCell", "LazyLock", "LazyCell", "lazy_static", "once_cell",
	"static mut ", "thread_local!", "RefCell<", "Mutex<", "RwLock<", "AtomicPtr",
}

// the two named factories, exactly as the runtimes declare them.
var rustNamedFactories = []string{
	"pub element: Option<fn() -> &'static TableBlockInfo>,",
	"pub record: Option<fn() -> &'static TableCookInfo>,",
}

// rustDescriptorFindings holds a unit's emitted Rust to M13 and returns one
// finding per violation, plus the counts of what it saw so a caller can refuse
// a vacuous scan.
func rustDescriptorFindings(files map[string][]byte) (findings []string, accessors map[string]int, registry, view bool) {
	accessors = map[string]int{}
	for _, name := range slices.Sorted(maps.Keys(files)) {
		lines := strings.Split(string(files[name]), "\n")
		for i, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "//") {
				continue
			}
			for _, tok := range rustLazyTokens {
				if strings.Contains(line, tok) && !rustPlanLazyLock.MatchString(line) {
					findings = append(findings, fmt.Sprintf("%s:%d: %q is a lazy or mutable publication: %s", name, i+1, tok, trimmed))
				}
			}
			if rustFactoryColumn.MatchString(line) && !slices.Contains(rustNamedFactories, trimmed) {
				findings = append(findings, fmt.Sprintf("%s:%d: a descriptor factory column that is not one of the two named ones: %s", name, i+1, trimmed))
			}
			if strings.HasPrefix(trimmed, "pub static TABLES: [TableDesc;") {
				registry = true
			}
			if strings.HasPrefix(trimmed, "static UNIT_VIEW: TableUnitView = TableUnitView {") {
				view = true
			}
			m := rustDescriptorAccessor.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			accessors[m[3]]++
			// the accessor's body runs to the brace at its own indent: it holds
			// function-local statics and returns a reference to one, and it
			// computes, allocates and branches on nothing
			end := -1
			for j := i + 1; j < len(lines); j++ {
				if lines[j] == m[1]+"}" {
					end = j
					break
				}
			}
			if end < 0 {
				findings = append(findings, fmt.Sprintf("%s:%d: %s() has no closing brace at its indent", name, i+1, m[2]))
				continue
			}
			body := lines[i+1 : end]
			last := strings.TrimSpace(body[len(body)-1])
			if !strings.HasPrefix(last, "&") || strings.ContainsAny(last[1:], " ();") {
				findings = append(findings, fmt.Sprintf("%s:%d: %s() does not end by returning a static: %q", name, i+1, m[2], last))
			}
			for _, b := range body {
				t := strings.TrimSpace(b)
				for _, bad := range []string{"let ", "if ", "match ", "Box::", "vec!", "Vec::", ".clone()", "unsafe"} {
					if strings.HasPrefix(t, bad) || strings.Contains(t, " "+bad) {
						findings = append(findings, fmt.Sprintf("%s:%d: %s() computes its descriptor (%q): %s", name, i+1, m[2], strings.TrimSpace(bad), t))
					}
				}
			}
			if m[3] != "TableUnitView" && !slices.ContainsFunc(body, func(b string) bool {
				return strings.HasPrefix(strings.TrimSpace(b), "static INFO: "+m[3]+" = "+m[3]+" {")
			}) {
				findings = append(findings, fmt.Sprintf("%s:%d: %s() returns no function-local `static INFO: %s`", name, i+1, m[2], m[3]))
			}
		}
	}
	return findings, accessors, registry, view
}

// rustDescriptorCorpus is the generated Rust of units that reach every
// descriptor the leg emits: blocks and cooks over pointers and by-value
// nesting (a record that reaches itself names the factory), the card
// registry, the view and a fixed root (the plans' LazyLock).
func rustDescriptorCorpus(t *testing.T) map[string][]byte {
	t.Helper()
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	all := map[string][]byte{}
	add := func(label string, u *ir.Unit) {
		out, err := Generate(u)
		if err != nil {
			t.Fatalf("%s: generate: %v", label, err)
		}
		for name, data := range out {
			all[label+"/"+name] = data
		}
	}
	add("probe", unitFrom(t, pointered))
	for _, dir := range []string{"tables/examples", "tables/cards", "tables/pointers", "tables/maps", "tables/arms"} {
		add(dir, vcLoadUnit(t, repo, dir))
	}
	// a fixed root with an older generation in its lineage: the build that
	// emits the per-lineage plans and so the one named LazyLock
	older, ok := FixedLineageOf(unitFrom(t, "package probe\n\nfixed table Lineage\n{\n    x int32\n}\n"), "Lineage")
	if !ok {
		t.Fatal("no lineage entry for the older generation")
	}
	own := unitFrom(t, "package probe\n\nfixed table Lineage\n{\n    x int32\n    y int32\n}\n")
	out, err := GenerateLineage(own, map[string][]FixedLineageEntry{"Lineage": {older}})
	if err != nil {
		t.Fatalf("lineage: generate: %v", err)
	}
	for name, data := range out {
		all["lineage/"+name] = data
	}
	return all
}

// TestRustDescriptorsAreConstants is M13 for the Rust leg.
func TestRustDescriptorsAreConstants(t *testing.T) {
	files := rustDescriptorCorpus(t)
	findings, accessors, registry, view := rustDescriptorFindings(files)
	for _, f := range findings {
		t.Error(f)
	}
	// a scan that saw nothing proves nothing
	for _, kind := range []string{"TableBlockInfo", "TableCookInfo", "TableUnitView"} {
		if accessors[kind] == 0 {
			t.Errorf("the scan found no %s accessor: the corpus, not the emitter, is what moved", kind)
		}
	}
	if !registry {
		t.Error("the scan found no card registry (pub static TABLES)")
	}
	if !view {
		t.Error("the scan found no unit view (static UNIT_VIEW)")
	}
	lazy, factories := 0, map[string]bool{}
	for _, data := range files {
		for line := range strings.SplitSeq(string(data), "\n") {
			if rustPlanLazyLock.MatchString(line) {
				lazy++
			}
			for _, f := range rustNamedFactories {
				if strings.TrimSpace(line) == f {
					factories[f] = true
				}
			}
		}
	}
	if lazy == 0 {
		t.Error("the corpus emitted no fixed-form plans: the one named LazyLock is not exercised")
	}
	for _, f := range rustNamedFactories {
		if !factories[f] {
			t.Errorf("the named factory column %q is not in the output", f)
		}
	}
	t.Logf("%d files; accessors %v; %d plan LazyLocks", len(files), accessors, lazy)
}

// TestRustDescriptorsAreConstantsGoesRed is the scan's reversed witness: each
// banned publication planted into a copy of the real output must be named.
func TestRustDescriptorsAreConstantsGoesRed(t *testing.T) {
	files := rustDescriptorCorpus(t)
	if f, _, _, _ := rustDescriptorFindings(files); len(f) != 0 {
		t.Fatalf("the clean output is not clean: %v", f)
	}
	plant := func(label, want string, edit func(string) string) {
		t.Helper()
		copied := maps.Clone(files)
		moved := false
		for name, data := range copied {
			if after := edit(string(data)); after != string(data) {
				copied[name] = []byte(after)
				moved = true
				break
			}
		}
		if !moved {
			t.Fatalf("%s: the plant applied nowhere", label)
		}
		got, _, _, _ := rustDescriptorFindings(copied)
		if !slices.ContainsFunc(got, func(f string) bool { return strings.Contains(f, want) }) {
			t.Errorf("%s: no finding names %q; got %v", label, want, got)
		}
	}
	plant("a block descriptor behind a OnceLock", "OnceLock", func(s string) string {
		return strings.Replace(s, "static INFO: TableBlockInfo = TableBlockInfo {",
			"static CELL: std::sync::OnceLock<TableBlockInfo> = std::sync::OnceLock::new();\n    static INFO: TableBlockInfo = TableBlockInfo {", 1)
	})
	plant("the plain cache on a cook descriptor", "static mut ", func(s string) string {
		return strings.Replace(s, "    static INFO: TableCookInfo = TableCookInfo {",
			"    static mut CACHE: Option<&'static TableCookInfo> = None;\n    static INFO: TableCookInfo = TableCookInfo {", 1)
	})
	plant("a descriptor built by a LazyLock", "LazyLock", func(s string) string {
		return strings.Replace(s, "pub static TABLES: [TableDesc;", "pub static TABLES_LAZY: std::sync::LazyLock<u8> = std::sync::LazyLock::new(|| 0);\npub static TABLES: [TableDesc;", 1)
	})
	plant("an accessor that computes its descriptor", "computes its descriptor", func(s string) string {
		return strings.Replace(s, "    static INFO: TableCookInfo = TableCookInfo {", "    let built = Vec::<u8>::new();\n    static INFO: TableCookInfo = TableCookInfo {", 1)
	})
	plant("a third factory column", "not one of the two named ones", func(s string) string {
		return strings.Replace(s, "pub record: Option<fn() -> &'static TableCookInfo>,", "pub record: Option<fn() -> &'static TableCookInfo>,\n    pub other: Option<fn() -> &'static TableBlockInfo>,", 1)
	})
}
