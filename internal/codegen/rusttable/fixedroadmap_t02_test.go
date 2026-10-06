package rusttable

// THE ROADMAP'S COMPILED-PLANS TASKS ON THE RUST LEG (docs/roadmap.sexp node
// `fixed-tables`; docs/FIXED-FORM-ALGORITHM.md §5.2's COMPILE; SPEC-TABLES §3.4).
// One subtest per task id. The compiled plans are the static data COMPILE lays
// down per lineage entry at build time, never re-parsed or re-built at runtime.
//
// rust/R1 [verify]: COMPILE lays the lineage down as static data at build time,
// oldest first and the current layout last, from the lock. The emitted array is
// read back here in order, and its first entry's layout bytes are compared with
// the lock's own entry.
//
// rust/R2 [weak]: record_bytes is 8 + body: the lock stores the body, COMPILE
// adds the eight once, and no backend adds anything. The emitted constant, the
// emitted per-entry records and a written file's measure are all held to it.
//
// rust/R23 [weak]: the static data's member names and order — the lineage entry
// carries hash, layout, record, retired in that order; the report's layout_hash
// is its LAST member and zero on every path but the two layout refusals.
//
// rust/R25 [owed]: plan_too_large when the plan does not fit the caller's
// capacity. A load whose plan slice is one short refuses BY NAME and the same
// bytes read at capacity.
//
// rust/R26 [owed]: a known hash whose lineage entry would not build →
// layout_malformed / plan_too_large by that entry's own lane, never a throw.
// The forged lineage below carries BOTH lanes: a layout the build cannot parse
// and a layout whose plan does not fit the declared capacity. A file selecting
// either hash is refused with that entry's own name and no panic.
//
// rust/W14 [owed]: plan dst == offsetof/sizeof. In this port the plan works in
// the record image (fixedform.go's header: "the plan's DESTINATION is THIS
// BUILD'S OWN RECORD IMAGE"), so the identity plan is dst 0 (the body's
// offsetof) with size the body (its sizeof), and a compiled plan lands the
// older writer's fields at this build's own image offsets.
//
// THE FORGED LINEAGE IS HANDED IN AS DATA, which is the entry point §5.9 #1
// names (GenerateLineage) and the one thing the conformance corpus cannot do:
// every conformance unit has a lineage of one, so R26's own-lane mapping is
// reached only through a generator probe (the conformance row R26.rs says so).

import (
	"bytes"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/rust"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// t02BigVariants is the writer enum's variant count in the forged lineage. The
// reader's remap pool is 1024 + 32 * its own block entries; one variant leaves
// that at 1120, so 2000 overflows it and the entry's plan comes back None —
// plan_too_large by name (§5.2's "the entries or their tables do not fit the
// declared capacity: REFUSE plan_too_large").
const t02BigVariants = 2000

// ---- the probes -------------------------------------------------------------

// t02PlansProbes is the rust source of the first crate: the real
// VOLD_field_append / VNEW_field_append lineage, one older entry handed in.
func t02PlansProbes(corpus string) string {
	return fmt.Sprintf(`
/// R1 (§5.2): COMPILE lays the lineage down OLDEST FIRST with this build's own
/// layout LAST. The array is static data, so its order is read directly, and
/// entry 0 must carry the lock's record size and the lock's layout bytes.
#[test]
fn v_t02_r1_lineage_order() {
    assert_eq!(LINEAGE_FIXED_LINEAGE.len(), 2, "the lock handed in one older entry and the build appends its own last");
    assert_eq!(LINEAGE_FIXED_OWN, LINEAGE_FIXED_LINEAGE.len() - 1, "this build's own layout is LAST (§5.2)");
    assert_eq!(LINEAGE_FIXED_LINEAGE[LINEAGE_FIXED_OWN].hash, LINEAGE_FIXED_HASH, "the last entry is this build's own hash");
    assert_eq!(LINEAGE_FIXED_LINEAGE[LINEAGE_FIXED_OWN].layout, &LINEAGE_FIXED_BLOCK[..], "the last entry is this build's own block");
    assert_ne!(LINEAGE_FIXED_LINEAGE[0].hash, LINEAGE_FIXED_HASH, "entry 0 is the lock's older layout, not this build's");
    assert_eq!(LINEAGE_FIXED_LINEAGE[0].record, 20, "the lock's entry stores 8 + its 12-byte body");
}

/// R2 (§5.2): "the lock stores the BODY; the eight hash bytes are added HERE,
/// once, and never again by a backend". The constant and the file's own
/// measure are both held to it.
#[test]
fn v_t02_r2_record_bytes() {
    assert_eq!(LINEAGE_FIXED_RECORD_BYTES, 8 + LINEAGE_FIXED_BODY_BYTES, "record_bytes is the body plus the hash, added once");
    assert_eq!(LINEAGE_FIXED_LINEAGE[LINEAGE_FIXED_OWN].record, LINEAGE_FIXED_RECORD_BYTES, "the own entry's record is that same 8 + body");
    assert_eq!(LINEAGE_FIXED_LINEAGE[0].record, 20, "the lock's older entry is 8 + its 12-byte body");
    let mine = std::fs::read(%[1]q).expect("the reference's new_field_append.bin");
    assert_eq!(
        mine.len(),
        TABLE_FIXED_HEADER_BYTES + 4 + LINEAGE_FIXED_BLOCK.len() + LINEAGE_FIXED_RECORD_BYTES,
        "the file measures header + layout + one record, the record being 8 + body"
    );
}

/// W14 (§7/§3.4): "the plan's destinations equal its own offsetof and sizeof".
/// In the image domain the identity plan is one Copy at dst 0 (the body's
/// offsetof) of size body (its sizeof), and a compiled plan lands the older
/// writer's fields at THIS build's own image offsets.
#[test]
fn v_t02_w14_plan_dst_offsets() {
    assert_eq!(LINEAGE_FIXED_PLAN.len(), 1, "the identity plan coalesces the whole body to one entry");
    let p = &LINEAGE_FIXED_PLAN[0];
    assert_eq!(
        (p.src, p.dst, p.size as usize),
        (0u32, 0u32, LINEAGE_FIXED_BODY_BYTES),
        "identity plan dst == offsetof(body) == 0 and size == sizeof(body)"
    );
    assert_eq!(p.op, TableFixedOp::Copy, "the identity plan is one Copy");
    let old = std::fs::read(%[2]q).expect("the reference's old_field_append.bin");
    let mut values = vec![LineageRow::default(); 1];
    let mut plan = vec![TableFixedEntry::default(); 512];
    let mut remap = vec![0u16; 4096];
    let mut report = TableFixedReport::default();
    let n = LOADFN(&mut values, &old, &mut plan, &mut remap, &mut report);
    assert_eq!(n, Some(1), "the older writer's file reads through the compiled plan: {:?}", report);
    assert_eq!((values[0].x, values[0].y, values[0].z), (11, 22, 33), "the older writer's values land at their own image offsets");
    assert_eq!(values[0].w, 77, "the field the writer did not carry keeps its declared default");
}

/// R25 (§5.9 #45): "the plan does not fit the caller's capacity | plan_too_large
/// | the name". A capacity of zero refuses by name; the same bytes read at a
/// capacity that holds the plan.
#[test]
fn v_t02_r25_plan_too_large() {
    let data = std::fs::read(%[1]q).expect("the reference's new_field_append.bin");
    let mut values = vec![LineageRow::default(); 1];
    let mut remap = vec![0u16; 4096];
    let mut report = TableFixedReport::default();
    let mut plan: Vec<TableFixedEntry> = Vec::new(); // capacity 0; the plan is one entry
    let n = LOADFN(&mut values, &data, &mut plan, &mut remap, &mut report);
    assert!(n.is_none(), "a plan past the caller's capacity refuses: {:?}", report);
    assert_eq!(report.reason.name(), "plan_too_large", "the name is the contract: {:?}", report);
    assert!(report.refused && !report.malformed, "a refusal by name never sets malformed: {:?}", report);
    let mut plan = vec![TableFixedEntry::default(); 1];
    let mut report = TableFixedReport::default();
    assert_eq!(LOADFN(&mut values, &data, &mut plan, &mut remap, &mut report), Some(1), "the same file reads at capacity 1: {:?}", report);
}

/// R23 (§5.2, §5.9 #15): "the file's hash lands on the report as layout_hash,
/// last on the report and zero on every other path". The two layout refusals
/// carry it; layout_malformed and plan_too_large do not.
#[test]
fn v_t02_r23_layout_hash_zero() {
    let data = std::fs::read(%[1]q).expect("the reference's new_field_append.bin");
    let mut plan = vec![TableFixedEntry::default(); 512];
    let mut remap = vec![0u16; 4096];
    let mut newer = data.clone();
    newer[TABLE_FIXED_HASH_AT..TABLE_FIXED_HASH_AT + 8].copy_from_slice(&0xDEAD_BEEF_CAFE_F00Du64.to_le_bytes());
    let mut values = vec![LineageRow::default(); 1];
    let mut report = TableFixedReport::default();
    assert!(LOADFN(&mut values, &newer, &mut plan, &mut remap, &mut report).is_none());
    assert_eq!(report.reason.name(), "layout_newer");
    assert_eq!(report.layout_hash, 0xDEAD_BEEF_CAFE_F00D, "layout_newer carries the file's hash");
    let mut lie = data.clone();
    lie[TABLE_FIXED_HEADER_BYTES + 4] ^= 0xFF;
    let mut report = TableFixedReport::default();
    assert!(LOADFN(&mut values, &lie, &mut plan, &mut remap, &mut report).is_none());
    assert_eq!(report.reason.name(), "layout_malformed");
    assert_eq!(report.layout_hash, 0, "layout_malformed reports no hash");
    let mut empty: Vec<TableFixedEntry> = Vec::new();
    let mut report = TableFixedReport::default();
    assert!(LOADFN(&mut values, &data, &mut empty, &mut remap, &mut report).is_none());
    assert_eq!(report.reason.name(), "plan_too_large");
    assert_eq!(report.layout_hash, 0, "plan_too_large reports no hash");
}
`, filepath.Join(corpus, "new_field_append.bin"), filepath.Join(corpus, "old_field_append.bin"))
}

// t02UnbuildableProbes is the rust source of the second crate: a lineage whose
// entries the build cannot turn into plans, one lane each.
func t02UnbuildableProbes() string {
	return `
/// R26 (§5.9 #36): a known hash whose lineage entry would not parse is
/// layout_malformed BY THAT ENTRY'S OWN LANE, never a throw.
#[test]
fn v_t02_r26_layout_malformed_lane() {
    let layout: &[u8] = CONSTPREFIX_FIXED_LINEAGE[0].layout;
    let mut data = vec![0u8; TABLE_FIXED_HEADER_BYTES + 4 + layout.len()];
    data[0] = TABLE_FIXED_FORM;
    data[TABLE_FIXED_HASH_AT..TABLE_FIXED_HASH_AT + 8]
        .copy_from_slice(&CONSTPREFIX_FIXED_LINEAGE[0].hash.to_le_bytes());
    data[TABLE_FIXED_HEADER_BYTES..TABLE_FIXED_HEADER_BYTES + 4]
        .copy_from_slice(&(layout.len() as u32).to_le_bytes());
    data[TABLE_FIXED_HEADER_BYTES + 4..].copy_from_slice(layout);
    let mut values = vec![EnumLineageRow::default(); 1];
    let mut plan = vec![TableFixedEntry::default(); 512];
    let mut remap = vec![0u16; 4096];
    let mut report = TableFixedReport::default();
    let n = LOADFN(&mut values, &data, &mut plan, &mut remap, &mut report);
    assert!(n.is_none(), "the entry would not build: refused, not thrown: {:?}", report);
    assert_eq!(report.reason.name(), "layout_malformed", "the entry's own lane carries the name: {:?}", report);
    assert!(report.refused && !report.malformed, "a refusal by name never sets malformed: {:?}", report);
    assert_eq!(report.layout_hash, 0, "the lane carries no hash");
}

/// R26 (§5.9 #36): a known hash whose entry parses but whose plan does not fit
/// the declared capacity is plan_too_large BY THAT ENTRY'S OWN LANE, never a
/// throw. The writer enum's many variants overflow the reader's remap pool, so
/// the LazyLock records the refusal on the lane and LOAD reports it when a file
/// selects that hash.
#[test]
fn v_t02_r26_plan_too_large_lane() {
    let layout: &[u8] = CONSTPREFIX_FIXED_LINEAGE[1].layout;
    let mut data = vec![0u8; TABLE_FIXED_HEADER_BYTES + 4 + layout.len()];
    data[0] = TABLE_FIXED_FORM;
    data[TABLE_FIXED_HASH_AT..TABLE_FIXED_HASH_AT + 8]
        .copy_from_slice(&CONSTPREFIX_FIXED_LINEAGE[1].hash.to_le_bytes());
    data[TABLE_FIXED_HEADER_BYTES..TABLE_FIXED_HEADER_BYTES + 4]
        .copy_from_slice(&(layout.len() as u32).to_le_bytes());
    data[TABLE_FIXED_HEADER_BYTES + 4..].copy_from_slice(layout);
    let mut values = vec![EnumLineageRow::default(); 1];
    let mut plan = vec![TableFixedEntry::default(); 4096];
    let mut remap = vec![0u16; 16384];
    let mut report = TableFixedReport::default();
    let n = LOADFN(&mut values, &data, &mut plan, &mut remap, &mut report);
    assert!(n.is_none(), "the entry's plan would not build: refused, not thrown: {:?}", report);
    assert_eq!(report.reason.name(), "plan_too_large", "the entry's own lane carries the name: {:?}", report);
    assert!(report.refused && !report.malformed, "a refusal by name never sets malformed: {:?}", report);
    assert_eq!(report.layout_hash, 0, "the lane carries no hash");
}
`
}

// t02EnumSchema declares one fixed table whose single field is an enum of
// `variants` variants. The ordinal's storage width grows with the count, and
// the remap pool the plan compiler reserves from is the reader's own, so a
// reader of one variant and a writer of many is the shape R26's plan_too_large
// lane is made of.
func t02EnumSchema(pkg string, variants int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "package %s\n\nenum Color {\n", pkg)
	for i := 0; i < variants; i++ {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "V%d", i)
		if i%8 == 7 {
			b.WriteString("\n")
		}
	}
	b.WriteString("\n}\n\nfixed table EnumLineage\n{\n    color Color\n}\n")
	return b.String()
}

// ---- the harness ------------------------------------------------------------

// t02Run writes TWO probe crates — the real lineage pair and the forged,
// unbuildable lineage — into ONE workspace and runs one `cargo test`, for the
// two-minute rule the package's versioning harness states. Every subtest reads
// its own rows out of that output.
func t02Run(t *testing.T) string {
	t.Helper()
	corpus := versionCorpus(t)
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	serialize, err := filepath.Abs(filepath.Join(repo, "..", "serialize.rs"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(serialize); err != nil {
		t.Fatalf("the Rust runtime is not beside this tree at %s: %v", serialize, err)
	}
	root := t.TempDir()

	older := versionUnit(t, versionSchema(t, "VOLD_field_append"))
	u := versionUnit(t, versionSchema(t, "VNEW_field_append"))
	lineageReal := map[string][]FixedLineageEntry{"Lineage": {t02Entry(t, older, "Lineage")}}
	t02WriteCrate(t, root, serialize, "probe_t02_plans", u, lineageReal, t02PlansProbes(corpus))

	big := versionUnit(t, t02EnumSchema("probe_big", t02BigVariants))
	reader := versionUnit(t, t02EnumSchema("probe_enum", 1))
	lineageForged := map[string][]FixedLineageEntry{
		"EnumLineage": {
			{Wire: 0x1111_1111_1111_1111, Layout: []byte{0x00}, Record: 9},
			t02Entry(t, big, "EnumLineage"),
		},
	}
	t02WriteCrate(t, root, serialize, "probe_t02_unbuildable", reader, lineageForged, t02UnbuildableProbes())

	ws := `# Generated by internal/codegen/rusttable/fixedroadmap_t02_test.go. Not tracked.
[workspace]
resolver = "3"
members = ["probe_t02_plans", "probe_t02_unbuildable"]

[profile.dev]
debug = 0
`
	if err := os.WriteFile(filepath.Join(root, "Cargo.toml"), []byte(ws), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("cargo", "test", "--workspace", "--no-fail-fast")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "CARGO_TARGET_DIR="+filepath.Join(repo, "build", "rust-versioning-target"))
	out, err := cmd.CombinedOutput()
	if err != nil && !strings.Contains(string(out), "test result: FAILED") {
		t.Fatalf("the t02 plan probe workspace did not build: %v\n%s", err, versionTail(string(out)))
	}
	return string(out)
}

// t02WriteCrate generates ONE probe crate for the reader unit with the lineage
// map handed to the emitter verbatim. versionWriteCrate cannot carry a forged
// entry — it takes schema base names — so R26 needs this path.
func t02WriteCrate(t *testing.T, root, serialize, name string, u *ir.Unit, lineage map[string][]FixedLineageEntry, src string) {
	t.Helper()
	table := versionRootName(t, u)

	files, err := rust.Generate(u)
	if err != nil {
		t.Fatalf("%s: rust.Generate: %v", name, err)
	}
	tables, err := GenerateLineage(u, lineage)
	if err != nil {
		t.Fatalf("%s: GenerateLineage: %v", name, err)
	}
	maps.Copy(files, tables)
	lib, err := rust.Lib(u, Modules(tables))
	if err != nil {
		t.Fatalf("%s: rust.Lib: %v", name, err)
	}
	files["lib.rs"] = append(lib, []byte("\n#[cfg(test)]\nmod t02_probe;\n")...)
	files["t02_probe.rs"] = []byte(strings.NewReplacer(
		"ROWTYPE", table+"Row",
		"LOADFN", ir.RustSnake(table)+"_fixed_load",
		"CONSTPREFIX", ir.RustConstName(table),
	).Replace(versionProbeSource(src)))

	dir := filepath.Join(root, name, "src")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	for fname, data := range files {
		if strings.Contains(fname, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, fname), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	manifest := fmt.Sprintf(`# Generated by internal/codegen/rusttable/fixedroadmap_t02_test.go. Not tracked.
[package]
name = %q
version = "0.0.0"
edition = "2024"

[features]
default = []
block = []
cook = []

[dependencies]
serialize = { package = "serialize-official", path = %q }
`, name, serialize)
	if err := os.WriteFile(filepath.Join(root, name, "Cargo.toml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
}

// ---- the Go-side assertions -------------------------------------------------

// t02Entry is one table's locked entry as the test's lock states it.
func t02Entry(t *testing.T, u *ir.Unit, name string) FixedLineageEntry {
	t.Helper()
	e, ok := FixedLineageOf(u, name)
	if !ok {
		t.Fatalf("no fixed lineage entry for %s", name)
	}
	return e
}

// t02Fixed is the emitted fixed module of a unit (the one *_fixed.rs the table
// backend produced).
func t02Fixed(t *testing.T, tables map[string][]byte) string {
	t.Helper()
	for name, data := range tables {
		if strings.HasSuffix(name, "_fixed.rs") {
			return string(data)
		}
	}
	t.Fatalf("no *_fixed.rs module was emitted")
	return ""
}

// t02HashConsts reads the lineage hashes IN THE ORDER THE ARRAY DECLARES THEM.
func t02HashConsts(text string) []uint64 {
	var out []uint64
	for _, m := range regexp.MustCompile(`(?m)^\s*hash: 0x([0-9a-fA-F]{16}),`).FindAllStringSubmatch(text, -1) {
		n, _ := strconv.ParseUint(m[1], 16, 64)
		out = append(out, n)
	}
	return out
}

// t02RecordConsts reads every lineage entry's record size in order.
func t02RecordConsts(text string) []int {
	var out []int
	for _, m := range regexp.MustCompile(`(?m)^\s*record: (\d+),`).FindAllStringSubmatch(text, -1) {
		n, _ := strconv.Atoi(m[1])
		out = append(out, n)
	}
	return out
}

// t02IntConst reads one emitted `pub const NAME: usize = N;`.
func t02IntConst(t *testing.T, text, name string) int {
	t.Helper()
	m := regexp.MustCompile(`pub const ` + regexp.QuoteMeta(name) + `: usize = (\d+);`).FindStringSubmatch(text)
	if m == nil {
		t.Fatalf("%s is not emitted", name)
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// t02ByteArray reads one emitted `pub const NAME: [u8; N] = [ ... ];`.
func t02ByteArray(t *testing.T, text, name string) []byte {
	t.Helper()
	at := strings.Index(text, "pub const "+name+": [u8; ")
	if at < 0 {
		t.Fatalf("%s is not emitted", name)
	}
	rest := text[at:]
	open := strings.Index(rest, "= [")
	if open < 0 {
		t.Fatalf("%s has no array literal", name)
	}
	rest = rest[open+3:]
	closeAt := strings.Index(rest, "];")
	if closeAt < 0 {
		t.Fatalf("%s's array literal does not close", name)
	}
	var out []byte
	for _, f := range strings.Fields(rest[:closeAt]) {
		f = strings.TrimSuffix(f, ",")
		if f == "" {
			continue
		}
		n, err := strconv.ParseUint(f, 0, 8)
		if err != nil {
			t.Fatalf("%s: byte %q: %v", name, f, err)
		}
		out = append(out, byte(n))
	}
	return out
}

// t02StructFields reads a rust struct's `pub` members in declaration order.
func t02StructFields(text, name string) []string {
	at := strings.Index(text, "pub struct "+name+" {")
	if at < 0 {
		return nil
	}
	rest := text[at:]
	end := strings.Index(rest, "\n}")
	if end < 0 {
		return nil
	}
	var out []string
	for _, m := range regexp.MustCompile(`(?m)^\s*pub ([A-Za-z_][A-Za-z0-9_]*):`).FindAllStringSubmatch(rest[:end], -1) {
		out = append(out, m[1])
	}
	return out
}

// t02R1FromLock holds rust/R1 to the lock: the emitted array is the lock's
// entry first and this build's own last, and the older entry's layout bytes are
// the lock's own bytes verbatim.
func t02R1FromLock(t *testing.T) {
	t.Helper()
	older := versionUnit(t, versionSchema(t, "VOLD_field_append"))
	u := versionUnit(t, versionSchema(t, "VNEW_field_append"))
	oldE := t02Entry(t, older, "Lineage")
	ownE := t02Entry(t, u, "Lineage")
	tables, err := GenerateLineage(u, map[string][]FixedLineageEntry{"Lineage": {oldE}})
	if err != nil {
		t.Fatal(err)
	}
	text := t02Fixed(t, tables)
	want := []uint64{oldE.Wire, ownE.Wire}
	if got := t02HashConsts(text); !slices.Equal(got, want) {
		t.Errorf("the lineage array is %016x, want OLDEST FIRST with the current layout last: %016x", got, want)
	}
	if got := t02IntConst(t, text, "LINEAGE_FIXED_OWN"); got != len(want)-1 {
		t.Errorf("_FIXED_OWN is %d, want the last index %d", got, len(want)-1)
	}
	if got := t02ByteArray(t, text, "LINEAGE_FIXED_LAYOUT_0"); !bytes.Equal(got, oldE.Layout) {
		t.Errorf("entry 0's layout is not the lock's bytes verbatim: got %d bytes, want %d", len(got), len(oldE.Layout))
	}
	if got := t02IntConst(t, text, "LINEAGE_FIXED_BODY_BYTES"); got != int(ir.TableFixedTypeBytes(versionRoot(t, u))) {
		t.Errorf("_FIXED_BODY_BYTES is %d, want the root's declared body", got)
	}
}

// t02R2RecordBytes holds rust/R2 to the two sites the eight may be added at
// once: the emitted constant and each entry's own record.
func t02R2RecordBytes(t *testing.T) {
	t.Helper()
	older := versionUnit(t, versionSchema(t, "VOLD_field_append"))
	u := versionUnit(t, versionSchema(t, "VNEW_field_append"))
	oldE := t02Entry(t, older, "Lineage")
	ownE := t02Entry(t, u, "Lineage")
	tables, err := GenerateLineage(u, map[string][]FixedLineageEntry{"Lineage": {oldE}})
	if err != nil {
		t.Fatal(err)
	}
	text := t02Fixed(t, tables)
	if !strings.Contains(text, "LINEAGE_FIXED_RECORD_BYTES: usize = 8 + LINEAGE_FIXED_BODY_BYTES;") {
		t.Errorf("record_bytes is not emitted as the body plus the eight, once")
	}
	body := t02IntConst(t, text, "LINEAGE_FIXED_BODY_BYTES")
	if body != int(ir.TableFixedTypeBytes(versionRoot(t, u))) {
		t.Errorf("body is %d, want the root's declared body", body)
	}
	recs := t02RecordConsts(text)
	if len(recs) != 2 || recs[0] != int(oldE.Record) || recs[1] != int(ownE.Record) {
		t.Errorf("the entries' records are %v, want the lock's %d and the own %d", recs, oldE.Record, ownE.Record)
	}
	if recs[1] != 8+body {
		t.Errorf("the own record is %d, want 8 + %d", recs[1], body)
	}
}

// t02R23Members holds rust/R23's static-data clause to the runtime's own
// carriers: the entry's four members in order, and the report's layout_hash
// last, zeroed by every refusal but the two that report a hash.
func t02R23Members(t *testing.T) {
	t.Helper()
	runtime := string(fixedRuntimeBody)
	if got := t02StructFields(runtime, "TableFixedKnown"); !slices.Equal(got, []string{"hash", "layout", "record", "retired"}) {
		t.Errorf("TableFixedKnown's members are %v, want [hash layout record retired]", got)
	}
	report := t02StructFields(runtime, "TableFixedReport")
	if len(report) == 0 || report[len(report)-1] != "layout_hash" {
		t.Errorf("TableFixedReport's members end in %v, want layout_hash last", report)
	}
	if !strings.Contains(runtime, "self.layout_hash = 0;") {
		t.Errorf("a refuse-by-name does not zero the report's layout_hash")
	}
	if got := strings.Count(runtime, "self.layout_hash = hash;"); got != 1 {
		t.Errorf("layout_hash is set at %d sites, want exactly the one layout refusal", got)
	}
}

// ---- the card's test --------------------------------------------------------

func TestFixedRoadmapT02Plans(t *testing.T) {
	t.Parallel()
	out := t02Run(t)
	tasks := []struct {
		id    string
		tests []string
		check func(t *testing.T)
	}{
		{id: "rust/R1", tests: []string{"v_t02_r1_lineage_order"}, check: t02R1FromLock},
		{id: "rust/R2", tests: []string{"v_t02_r2_record_bytes"}, check: t02R2RecordBytes},
		{id: "rust/R23", tests: []string{"v_t02_r23_layout_hash_zero"}, check: t02R23Members},
		{id: "rust/R25", tests: []string{"v_t02_r25_plan_too_large"}},
		{id: "rust/R26", tests: []string{"v_t02_r26_layout_malformed_lane", "v_t02_r26_plan_too_large_lane"}},
		{id: "rust/W14", tests: []string{"v_t02_w14_plan_dst_offsets"}},
	}
	for _, task := range tasks {
		task := task
		t.Run(task.id, func(t *testing.T) {
			t.Parallel()
			for _, name := range task.tests {
				versionAssert(t, out, name)
			}
			if task.check != nil {
				task.check(t)
			}
		})
	}
}
