package rusttable

import (
	"bytes"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// update rewrites the committed retained golden (docs/SPEC-TABLES.md §6.6) from
// the Go leg's own engine. The test itself never produces the golden; it only
// reads it back and compares the Rust retained save byte-for-byte.
var update = flag.Bool("update", false, "rewrite the Go-produced retained golden")

// Every rule this file pins is proved to BITE (docs/SPEC-TABLES.md §6.6, §8):
// sabotaging retain_capture to return None reds
// TestCardConstructsRetainUnknownFields ("the retained load's counters are
// wrong: LOAD unknown=4 retained=0 retain_lost=4"), sabotaging the
// bytes.capacity() check reds TestRetentionCapacityIsALoss ("a buffer one byte
// short kept or mis-counted records: LOAD unknown=4 retained=2
// retain_lost=2"), and a driver whose save drops the retained tail diverges
// under cards-rust-retain-fuzz ("the leg saves 621 bytes, the oracle 633").

// cardDescriptors pins docs/SPEC-TABLES.md §8.1, §8.3: the generated card
// descriptors name every field of tables/cards — table, field, kind, shape and
// bound, in declaration order — and the unit registry lists every card table.
// The vocabulary below is WRITTEN IN THE TEST, not derived from the descriptors:
// a field added to Card.schema without a matching row here makes this test
// fail, which is the point of pinning rather than echoing the emitter.
func TestCardDescriptorsNameEveryField(t *testing.T) {
	t.Parallel()

	pinned := []pinnedTable{
		{"Card", []pinnedField{
			{"id", 12, "value", 64},
			{"sha", 12, "value", 40},
			{"tier", 30, "value", 0},
			{"repo", 12, "value", 96},
			{"_base", 12, "value", 96},
			{"kind", 12, "value", 24},
			{"priority", 30, "value", 0},
			{"stream", 12, "value", 32},
			{"paths", 14, "list", -1},
			{"_new", 14, "list", -1},
			{"shared", 14, "list", -1},
			{"needs", 14, "list", -1},
			{"test", 12, "value", 192},
			{"done", 17, "blob", -1},
			{"deadline_minutes", 4, "value", 0},
			{"source", 17, "blob", -1},
			{"epoch", 4, "value", 0},
			{"column", 30, "value", 0},
			{"holder", 12, "value", 32},
			{"attempt", 4, "value", 0},
			{"head", 12, "value", 40},
			{"branch", 12, "value", 128},
			{"body", 17, "blob", -1},
			{"extra", 14, "list", -1},
			{"consumers", 14, "list", -1},
		}},
		{"CardID", []pinnedField{
			{"s", 12, "value", 64},
		}},
		{"Consumer", []pinnedField{
			{"kind", 12, "value", 16},
			{"who", 12, "value", 32},
			{"route", 12, "value", 48},
			{"model", 12, "value", 64},
			{"started", 5, "value", 0},
			{"ended", 5, "value", 0},
			{"outcome", 12, "value", 16},
			{"line", 12, "value", 200},
			{"cost_micro_usd", 5, "value", 0},
		}},
		{"KeyValue", []pinnedField{
			{"key", 12, "value", 32},
			{"value", 17, "blob", -1},
		}},
		{"PathText", []pinnedField{
			{"s", 12, "value", 192},
		}},
	}

	src, err := os.ReadFile("../../../tables/cards/Card.schema")
	if err != nil {
		t.Fatal(err)
	}
	got := descriptorsFrom(t, string(src))
	if !equalDescriptors(got, pinned) {
		t.Fatalf("the generated descriptors differ from the pinned vocabulary:\n got:  %s\n want: %s",
			describeDescriptors(got), describeDescriptors(pinned))
	}

	// A planted edit bites: a field added to the declaration without a matching
	// vocabulary row must turn this comparison red, or the pin watches nothing.
	planted := strings.Replace(string(src), "body             *string",
		"body             *string\n    future           string(8)", 1)
	got = descriptorsFrom(t, planted)
	if equalDescriptors(got, pinned) {
		t.Fatal("adding a field to the declaration without updating the vocabulary did not fail")
	}
}

type pinnedField struct {
	name  string
	kind  int
	shape string
	bound int64
}

type pinnedTable struct {
	name   string
	fields []pinnedField
}

var (
	fieldRe = regexp.MustCompile(`TableFieldDesc \{ name: "([^"]+)", kind: (\d+), shape: (SHAPE_[A-Z]+), bound: (-?\d+) \}`)
	tableRe = regexp.MustCompile(`TableDesc \{ name: "([^"]+)", num_fields: (\d+), fields: &\w+ \}`)
)

// descriptorsFrom generates the card descriptor module for one unit's source
// and parses it back into an ordered (table, fields) list — the same shape the
// pinned vocabulary takes, so the comparison is against data, not a substring.
func descriptorsFrom(t *testing.T, src string) []pinnedTable {
	t.Helper()
	f, perrs := parser.Parse("Card.schema", []byte(src))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "Card.schema", Name: "Card.schema", Base: "Card", Bytes: []byte(src), AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	modules, err := cardVariableModules(u)
	if err != nil {
		t.Fatalf("card descriptors were refused: %v", err)
	}
	body := string(modules[VariableDescriptorModule+".rs"])
	if body == "" {
		t.Fatalf("the card descriptors emitted no %s; got %v", VariableDescriptorModule+".rs", keys(modules))
	}

	flat := make([]pinnedField, 0)
	for _, m := range fieldRe.FindAllStringSubmatch(body, -1) {
		kind, _ := strconv.Atoi(m[2])
		bound, _ := strconv.ParseInt(m[4], 10, 64)
		flat = append(flat, pinnedField{name: m[1], kind: kind, shape: shapeName(m[3]), bound: bound})
	}
	var out []pinnedTable
	at := 0
	for _, m := range tableRe.FindAllStringSubmatch(body, -1) {
		n, _ := strconv.Atoi(m[2])
		fields := append([]pinnedField(nil), flat[at:at+n]...)
		out = append(out, pinnedTable{name: m[1], fields: fields})
		at += n
	}
	if at != len(flat) || len(out) == 0 {
		t.Fatalf("the descriptor module did not parse whole: %d fields, %d tables", len(flat), len(out))
	}
	return out
}

func shapeName(spelling string) string {
	switch spelling {
	case "SHAPE_VALUE":
		return "value"
	case "SHAPE_LIST":
		return "list"
	case "SHAPE_BLOB":
		return "blob"
	}
	return spelling
}

func equalDescriptors(a, b []pinnedTable) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].name != b[i].name || len(a[i].fields) != len(b[i].fields) {
			return false
		}
		for j := range a[i].fields {
			if a[i].fields[j] != b[i].fields[j] {
				return false
			}
		}
	}
	return true
}

func describeDescriptors(tables []pinnedTable) string {
	var b strings.Builder
	for _, tb := range tables {
		b.WriteString(tb.name)
		b.WriteString(":[")
		for i, f := range tb.fields {
			if i > 0 {
				b.WriteString(" ")
			}
			b.WriteString(f.name)
			b.WriteString("/")
			b.WriteString(strconv.Itoa(f.kind))
			b.WriteString("/")
			b.WriteString(f.shape)
			b.WriteString("/")
			b.WriteString(strconv.FormatInt(f.bound, 10))
		}
		b.WriteString("]")
	}
	return b.String()
}

// retainBinary compiles the generated variable-wire modules for one schema file
// beside a probe main, and returns the binary's path. The schema's single table
// is named Card, so the codec module is probe_table.rs (the file base is Probe).
func retainBinary(t *testing.T, schemaPath, probe string) string {
	t.Helper()
	if _, err := exec.LookPath("rustc"); err != nil {
		t.Skipf("rustc is not on PATH: %v", err)
	}
	src, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatal(err)
	}
	modules, err := Generate(unitFrom(t, string(src)))
	if err != nil {
		t.Fatalf("generate %s: %v", schemaPath, err)
	}
	root := t.TempDir()
	for _, name := range []string{"variable_runtime.rs", "probe_table.rs"} {
		data, ok := modules[name]
		if !ok {
			t.Fatalf("missing module %s; got %v", name, keys(modules))
		}
		if err := os.WriteFile(filepath.Join(root, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "main.rs"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(root, "probe")
	cmd := exec.Command("rustc", "--edition", "2024", "-O", "-o", bin, "main.rs")
	cmd.Dir = root
	cmd.Env = os.Environ()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the card codec did not build: %v\n%s", err, out)
	}
	return bin
}

func testdataCards(t *testing.T, name string) string {
	t.Helper()
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(repo, "internal", "codegen", "rusttable", "testdata", "cards", name)
}

const retainProducerProbe = `
mod variable_runtime;
mod probe_table;
use probe_table::*;
use variable_runtime::*;

fn main() {
    let path = std::env::args().nth(1).unwrap();
    let v = Card { id: "x".into(), n: 7, extra: "hello".into(), future_i: 42, tier: Tier::Flash, blob: Some("world".into()) };
    let wire = card_save(&v);
    std::fs::write(&path, &wire).unwrap();
    println!("PRODUCED {}", wire.len());
}
`

// retainEnumProducerProbe saves ONLY the newer-only enum variant (kind 30), so
// a retained load of it isolates the enum-variant exclusion the capacity cases
// would otherwise drown out (docs/SPEC-TABLES.md §6.6).
const retainEnumProducerProbe = `
mod variable_runtime;
mod probe_table;
use probe_table::*;
use variable_runtime::*;

fn main() {
    let path = std::env::args().nth(1).unwrap();
    let v = Card { id: "x".into(), n: 7, extra: String::new(), future_i: 0, tier: Tier::Flash, blob: None };
    let wire = card_save(&v);
    std::fs::write(&path, &wire).unwrap();
    println!("PRODUCED {}", wire.len());
}
`

const retainRoundTripProbe = `
mod variable_runtime;
mod probe_table;
use probe_table::*;
use variable_runtime::*;

fn main() {
    let wire_path = std::env::args().nth(1).unwrap();
    let out_path = std::env::args().nth(2).unwrap();
    let byte_cap: usize = std::env::args().nth(3).unwrap().parse().unwrap();
    let id_cap: usize = std::env::args().nth(4).unwrap().parse().unwrap();
    let wire = std::fs::read(&wire_path).unwrap();
    let mut report = TableReport::default();
    let mut retain = TableRetain { bytes: Vec::with_capacity(byte_cap), ids: Vec::with_capacity(id_cap) };
    let v = card_load_retain(&wire, &mut retain, &mut report).unwrap();
    println!("LOAD unknown={} retained={} retain_lost={}", report.unknown, report.retained, report.retain_lost);
    let saved = card_save_retain(&v, &mut retain, &mut report);
    println!("SAVE retained={} retain_lost={} len={}", report.retained, report.retain_lost, saved.len());
    std::fs::write(&out_path, &saved).unwrap();
}
`

const retainVerifyProbe = `
mod variable_runtime;
mod probe_table;
use probe_table::*;
use variable_runtime::*;

fn main() {
    let path = std::env::args().nth(1).unwrap();
    let wire = std::fs::read(&path).unwrap();
    let mut report = TableReport::default();
    let v = card_load(&wire, &mut report).unwrap();
    assert_eq!(v.id, "x");
    assert_eq!(v.n, 7);
    assert_eq!(v.future_i, 42);
    assert_eq!(v.extra, "hello");
    assert_eq!(v.tier, Tier::None);
    assert_eq!(v.blob, None);
    println!("VERIFIED");
}
`

// goRetainedSave is THE GO LEG's retained save of one input wire
// (docs/SPEC-TABLES.md §6.6): internal/tablewire is the compiler's own Go
// engine — the same reading the wire-fuzz oracle and the Go conformance driver
// agree with byte-for-byte on every retained row — and it is the evidence the
// Rust retained save is compared against. It is never the Rust code under test.
func goRetainedSave(t *testing.T, input []byte) []byte {
	t.Helper()
	olderSrc, err := os.ReadFile(testdataCards(t, "older.schema"))
	if err != nil {
		t.Fatal(err)
	}
	model := tabletext.NewModel(unitFrom(t, string(olderSrc)))
	value := model.New(model.Lookup("Card"))
	store := &tablewire.Retain{Capacity: 1 << 20, IdCapacity: 1 << 17}
	var report tabletext.Report
	if ok, err := tablewire.DecodeRetain(model, value, input, store, &report); !ok || err != nil {
		t.Fatalf("the Go leg's retained load refused the input: ok=%v err=%v %+v", ok, err, report)
	}
	out, err := tablewire.EncodeRetain(model, value, store, &report)
	if err != nil {
		t.Fatalf("the Go leg's retained save refused: %v", err)
	}
	return out
}

// cardsRetainGolden returns the committed retained golden, and under -update
// rewrites it from the Go leg's engine above. The test reads it back and never
// produces it.
func cardsRetainGolden(t *testing.T, input []byte) []byte {
	t.Helper()
	goldenPath := testdataCards(t, "retained.golden")
	if !*update {
		b, err := os.ReadFile(goldenPath)
		if err != nil {
			t.Fatalf("missing retained golden %s (regenerate: go test ./internal/codegen/rusttable -run TestCardConstructsRetainUnknownFields -update): %v", goldenPath, err)
		}
		return b
	}
	b := goRetainedSave(t, input)
	if err := os.WriteFile(goldenPath, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return b
}

// TestCardConstructsRetainUnknownFields pins docs/SPEC-TABLES.md §6.6: the
// older declaration's retained load keeps the newer declaration's unknown
// scalar and string fields, re-emits them in a retained save, and the newer
// declaration rereads every retained value equal. The retained bytes equal the
// Go leg's retained save of the same input, committed as evidence under
// testdata/cards/retained.golden. The unknown *string field is kind 17 — a node
// index — so it is the excluded class and is reported as loss, never retained
// silently, and the same for the unknown enum variant the newer declaration
// spells (kind 30), which §6.6 names an exclusion of its own.
func TestCardConstructsRetainUnknownFields(t *testing.T) {
	t.Parallel()

	producer := retainBinary(t, testdataCards(t, "newer.schema"), retainProducerProbe)
	roundTrip := retainBinary(t, testdataCards(t, "older.schema"), retainRoundTripProbe)
	verifier := retainBinary(t, testdataCards(t, "newer.schema"), retainVerifyProbe)

	dir := t.TempDir()
	wire := filepath.Join(dir, "populated.wire")
	rewritten := filepath.Join(dir, "rewritten.wire")

	out, err := exec.Command(producer, wire).CombinedOutput()
	if err != nil {
		t.Fatalf("the newer declaration did not save: %v\n%s", err, out)
	}

	input, err := os.ReadFile(wire)
	if err != nil {
		t.Fatal(err)
	}

	out, err = exec.Command(roundTrip, wire, rewritten, "1048576", "131072").CombinedOutput()
	if err != nil {
		t.Fatalf("the older declaration's retained round trip did not run: %v\n%s", err, out)
	}
	text := string(out)
	if !strings.Contains(text, "LOAD unknown=4 retained=2 retain_lost=2") {
		t.Errorf("the retained load's counters are wrong:\n%s", text)
	}
	if !strings.Contains(text, "SAVE retained=2 retain_lost=2") {
		t.Errorf("the retained save's counters are wrong:\n%s", text)
	}

	// THE RETAINED SAVE IS BYTE-EQUAL TO THE GO LEG'S (docs/SPEC-TABLES.md §6.6):
	// the retained data is re-emitted in the same canonical order the Go backend
	// writes, so a retained save is byte-equal across legs. The evidence is the
	// committed golden, produced by internal/tablewire under -update and never by
	// this test.
	rewrittenBytes, err := os.ReadFile(rewritten)
	if err != nil {
		t.Fatal(err)
	}
	if want := cardsRetainGolden(t, input); !bytes.Equal(rewrittenBytes, want) {
		t.Errorf("the retained save diverges from the Go leg's: got %d bytes, want %d bytes, first difference at byte %d",
			len(rewrittenBytes), len(want), firstDiff(rewrittenBytes, want))
	}

	out, err = exec.Command(verifier, rewritten).CombinedOutput()
	if err != nil || !strings.Contains(string(out), "VERIFIED") {
		t.Errorf("the newer declaration did not reread the retained values equal: %v\n%s", err, out)
	}
}

func firstDiff(a, b []byte) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return i
		}
	}
	if len(a) != len(b) {
		return min(len(a), len(b))
	}
	return -1
}

// TestRetentionCapacityIsALoss pins docs/SPEC-TABLES.md §6.6: a retention buffer
// one byte short of the record, or a retained-id list one entry short, drops
// the record whole and counts one retain_lost, and the input bytes are never
// changed. The retained load still reports the fields it kept. It also proves
// the enum-variant exclusion: an unknown enum variant in a KNOWN field is
// reported as retain_lost — not merely unknown — so it blocks any lossless
// claim, and the input bytes stay unchanged through every case.
func TestRetentionCapacityIsALoss(t *testing.T) {
	t.Parallel()

	producer := retainBinary(t, testdataCards(t, "newer.schema"), retainProducerProbe)
	roundTrip := retainBinary(t, testdataCards(t, "older.schema"), retainRoundTripProbe)

	dir := t.TempDir()
	wire := filepath.Join(dir, "populated.wire")
	if out, err := exec.Command(producer, wire).CombinedOutput(); err != nil {
		t.Fatalf("the newer declaration did not save: %v\n%s", err, out)
	}
	before, err := os.ReadFile(wire)
	if err != nil {
		t.Fatal(err)
	}

	// One byte short: the whole record is dropped, not truncated.
	short := filepath.Join(dir, "short.wire")
	out, err := exec.Command(roundTrip, wire, short, "3", "131072").CombinedOutput()
	if err != nil {
		t.Fatalf("the retained round trip did not run: %v\n%s", err, out)
	}
	text := string(out)
	if !strings.Contains(text, "retained=0") || !strings.Contains(text, "retain_lost=4") {
		t.Errorf("a buffer one byte short kept or mis-counted records:\n%s", text)
	}

	// One id short at the save: the retained record's id cannot be interned, so
	// the record is dropped at the save and counted one retain_lost.
	noIds := filepath.Join(dir, "noids.wire")
	out, err = exec.Command(roundTrip, wire, noIds, "1048576", "0").CombinedOutput()
	if err != nil {
		t.Fatalf("the retained round trip did not run: %v\n%s", err, out)
	}
	text = string(out)
	if !strings.Contains(text, "LOAD unknown=4 retained=2 retain_lost=2") {
		t.Errorf("an id list one entry short changed the load counters:\n%s", text)
	}
	if !strings.Contains(text, "SAVE retained=2 retain_lost=4") {
		t.Errorf("the id list overflow was not reported as loss:\n%s", text)
	}

	// THE ENUM-VARIANT EXCLUSION (docs/SPEC-TABLES.md §6.6): a field the older
	// declaration KNOWS — `tier` — carrying a variant it does not (the
	// newer-only `flash`) counts one unknown AND one retain_lost, and nothing
	// retained. §6.6 names unknown enum variants an exclusion, so the variant is
	// never retained silently and the report blocks a lossless claim.
	enumProducer := retainBinary(t, testdataCards(t, "newer.schema"), retainEnumProducerProbe)
	enumWire := filepath.Join(dir, "enum.wire")
	if out, err := exec.Command(enumProducer, enumWire).CombinedOutput(); err != nil {
		t.Fatalf("the enum producer did not save: %v\n%s", err, out)
	}
	enumBefore, err := os.ReadFile(enumWire)
	if err != nil {
		t.Fatal(err)
	}
	enumOut := filepath.Join(dir, "enum-out.wire")
	out, err = exec.Command(roundTrip, enumWire, enumOut, "1048576", "131072").CombinedOutput()
	if err != nil {
		t.Fatalf("the enum round trip did not run: %v\n%s", err, out)
	}
	text = string(out)
	if !strings.Contains(text, "LOAD unknown=1 retained=0 retain_lost=1") {
		t.Errorf("the unknown enum variant was not counted a loss:\n%s", text)
	}
	if !strings.Contains(text, "SAVE retained=0 retain_lost=1") {
		t.Errorf("the unknown enum variant did not stay a loss at the save:\n%s", text)
	}

	after, err := os.ReadFile(wire)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Errorf("the input bytes changed across the retained round trips")
	}
	enumAfter, err := os.ReadFile(enumWire)
	if err != nil {
		t.Fatal(err)
	}
	if string(enumBefore) != string(enumAfter) {
		t.Errorf("the enum input bytes changed across the retained round trip")
	}
}
