package rusttable

import (
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestRustSkippedValueIsValidated runs every row of the shared skipped-value
// vectors through the Rust leg's reads (docs/SPEC-TABLES.md §4, §16.3;
// docs/rulings.json skipped-value-is-validated): the JSON rows through the
// reader root's text read, and the message rows through its batch load under
// the newer writer unit's announcement. A value the reader SKIPS is held to
// what a read of it would hold, and damage inside it refuses the whole read.
// The verdict is the contract; a refusal's name is the leg's own diagnostic.
func TestRustSkippedValueIsValidated(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "rustc (it compiles and runs the generated skipped-value probe)")

	if _, err := exec.LookPath("rustc"); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and rustc is not on PATH: %v", err)
		}
		t.Skipf("rustc is not on PATH: %v", err)
	}

	root := filepath.Join("..", "..", "..")
	dir := filepath.Join(root, "testdata", "wire", "skipped-value")
	manifest, err := os.ReadFile(filepath.Join(dir, "MANIFEST.tsv"))
	if err != nil {
		t.Fatal(err)
	}

	type row struct{ file, form, verdict string }
	var rows []row
	for i, line := range strings.Split(string(manifest), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 3 {
			t.Fatalf("%s:%d: a vector line is <file> TAB <form> TAB <verdict>, got %q", manifest, i+1, line)
		}
		if f[1] != "json" && f[1] != "message" {
			t.Fatalf("%s:%d: form %q, want json or message", manifest, i+1, f[1])
		}
		if f[2] != "read" && f[2] != "refused" {
			t.Fatalf("%s:%d: verdict %q, want read or refused", manifest, i+1, f[2])
		}
		rows = append(rows, row{f[0], f[1], f[2]})
	}
	if len(rows) == 0 {
		t.Fatalf("%s names no rows", manifest)
	}

	schemaBytes, err := os.ReadFile(filepath.Join(root, "test", "skipped-value", "reader", "Reader.schema"))
	if err != nil {
		t.Fatal(err)
	}
	u := unitFrom(t, string(schemaBytes))
	modules, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	annIdx := -1
	for i, r := range rows {
		if r.form == "message" && r.file == "announcement.bin" {
			annIdx = i
		}
	}
	if annIdx < 0 {
		t.Fatalf("%s names no announcement.bin message row", manifest)
	}
	announcement, err := os.ReadFile(filepath.Join(dir, "announcement.bin"))
	if err != nil {
		t.Fatal(err)
	}

	var body strings.Builder
	fmt.Fprintf(&body, "const ANN: &[u8] = &%s;\n", rustByteVector(announcement))
	body.WriteString(`
fn announce_verdict(wire: &[u8]) -> bool {
    let mut storage = vec![TableMessageEntry::default(); 8192];
    let mut vocab = TableVocabulary::new(&mut storage);
    announce_read(&mut vocab, wire, &mut TableReport::default())
}

fn message_verdict(wire: &[u8]) -> bool {
    let mut storage = vec![TableMessageEntry::default(); 8192];
    let mut vocab = TableVocabulary::new(&mut storage);
    let mut ar = TableReport::default();
    if !announce_read(&mut vocab, ANN, &mut ar) {
        return false;
    }
    let mut values = vec![root_message_default(); 1];
    let mut report = TableReport::default();
    let (count, ok) = root_load_messages(&mut values, &vocab, wire, &mut report);
    ok && count == 1 && !report.damaged && !report.refused
}

fn json_verdict(wire: &[u8]) -> bool {
    let mut report = TableReport::default();
    root_from_json_bytes(wire, &mut report).is_some() && !report.damaged && !report.refused
}

fn skip_row(name: &str, v: &TableVocabulary, e: &TableMessageEntry, parts: &[(u64, u32)]) {
    let mut buf = [0u8; 64];
    let mut w = TableBitWriter::new(&mut buf);
    for &(value, width) in parts { w.put(value, width); }
    let used = ((w.bits + 7) / 8) as usize;
    let mut r = TableBitReader::new(&buf[..used]);
    let ok = table_message_skip(&mut r, v, e, 0);
    println!("ROW\t{}\t{}", name, if ok { "read" } else { "refused" });
}

fn nested_row(name: &str, v: &TableVocabulary, first: u64, second: u64) {
    let mut buf = [0u8; 64];
    let mut w = TableBitWriter::new(&mut buf);
    w.put(first, v.ref_bits);
    w.put(second, v.ref_bits);
    w.put(0, v.ref_bits);
    let used = ((w.bits + 7) / 8) as usize;
    let mut r = TableBitReader::new(&buf[..used]);
    let body = TableMessageEntry { kind: 13, ..Default::default() };
    let ok = table_message_skip(&mut r, v, &body, 0);
    println!("ROW\t{}\t{}", name, if ok { "read" } else { "refused" });
}

fn ranged_entry(kind: u8, bits: u64, base_lo: u64, base_hi: u64) -> TableMessageEntry {
    let mut e = TableMessageEntry::default();
    e.kind = kind;
    e.shape.packing = 1;
    e.shape.bits = bits;
    e.shape.base_lo = base_lo;
    e.shape.base_hi = base_hi;
    e
}

fn ranged_array(kind: u8, bits: u64, base_lo: u64) -> TableMessageEntry {
    let mut e = TableMessageEntry::default();
    e.kind = 14;
    e.shape.min = 1;
    e.shape.max = 1;
    e.shape.elem = kind;
    e.element.packing = 1;
    e.element.bits = bits;
    e.element.base_lo = base_lo;
    e
}

fn keyed_names() -> TableMessageEntry {
    let mut e = TableMessageEntry::default();
    e.kind = 16;
    e.shape.max = 3;
    e.shape.elem = 0;
    e
}

fn main() {
`)
	for i, r := range rows {
		data, err := os.ReadFile(filepath.Join(dir, r.file))
		if err != nil {
			t.Fatalf("%s: %v", r.file, err)
		}
		var call string
		if r.form == "json" {
			call = "json_verdict"
		} else if i == annIdx {
			call = "announce_verdict"
		} else {
			call = "message_verdict"
		}
		fmt.Fprintf(&body, "    println!(\"%d {}\", if %s(&%s) { \"read\" } else { \"refused\" });\n",
			i, call, rustByteVector(data))
	}
	zero := ir.TableMessageShape{}
	announcements := []struct {
		name  string
		entry ir.TableVocabularyEntry
	}{
		{"announce-elem-12", ir.TableVocabularyEntry{Id: 1, Kind: uint8(ir.TableKindArray), Shape: ir.TableMessageShape{Min: 0, Max: 4, Elem: uint8(ir.TableKindString), Inner: &ir.TableMessageShape{Max: 8}}}},
		{"announce-elem-14", ir.TableVocabularyEntry{Id: 2, Kind: uint8(ir.TableKindArray), Shape: ir.TableMessageShape{Min: 0, Max: 4, Elem: uint8(ir.TableKindArray), Inner: &zero}}},
		{"announce-elem-16", ir.TableVocabularyEntry{Id: 3, Kind: uint8(ir.TableKindArray), Shape: ir.TableMessageShape{Min: 0, Max: 4, Elem: uint8(ir.TableKindKeyed), Inner: &zero}}},
		{"announce-keyed-elem-12", ir.TableVocabularyEntry{Id: 4, Kind: uint8(ir.TableKindKeyed), Shape: ir.TableMessageShape{Max: 3, Elem: uint8(ir.TableKindString), Inner: &ir.TableMessageShape{Max: 8}}}},
	}
	for _, a := range announcements {
		fmt.Fprintf(&body, "    println!(\"ROW\\t%s\\t{}\", if announce_verdict(&%s) { \"read\" } else { \"refused\" });\n",
			a.name, rustByteVector(testMessageAnnouncement(a.entry)))
	}
	body.WriteString(`
    let mut names_storage = [TableMessageEntry::default(); 3];
    names_storage[0].id = 100; names_storage[0].kind = 0;
    names_storage[1].id = 101; names_storage[1].kind = 0;
    names_storage[2].id = 102; names_storage[2].kind = 4;
    let mut names = TableVocabulary::new(&mut names_storage);
    names.count = 3;
    names.ref_bits = 2;
    names.announced = true;

    skip_row("ranged-i8-at-max", &names, &ranged_entry(2, 8, (-100i64) as u64, 0), &[(227, 8)]);
    skip_row("ranged-i8-over-max", &names, &ranged_entry(2, 8, (-100i64) as u64, 0), &[(228, 8)]);
    skip_row("ranged-u8-at-max", &names, &ranged_entry(6, 8, 200, 0), &[(55, 8)]);
    skip_row("ranged-u8-over-max", &names, &ranged_entry(6, 8, 200, 0), &[(56, 8)]);
    skip_row("ranged-u64-at-max", &names, &ranged_entry(9, 8, u64::MAX - 5, 0), &[(5, 8)]);
    skip_row("ranged-u64-over-max", &names, &ranged_entry(9, 8, u64::MAX - 5, 0), &[(6, 8)]);
    skip_row("ranged-i128-at-max", &names, &ranged_entry(18, 8, (-4i64) as u64, i64::MAX as u64), &[(3, 8)]);
    skip_row("ranged-i128-over-max", &names, &ranged_entry(18, 8, (-4i64) as u64, i64::MAX as u64), &[(4, 8)]);
    skip_row("ranged-element-at-max", &names, &ranged_array(2, 8, (-100i64) as u64), &[(227, 8)]);
    skip_row("ranged-element-over-max", &names, &ranged_array(2, 8, (-100i64) as u64), &[(228, 8)]);
    skip_row("keyed-zero-bit-distinct", &names, &keyed_names(), &[(2, 2), (1, 2), (2, 2)]);
    skip_row("keyed-zero-bit-repeat", &names, &keyed_names(), &[(2, 2), (1, 2), (1, 2)]);
    skip_row("keyed-zero-bit-payload-entry", &names, &keyed_names(), &[(2, 2), (1, 2), (3, 2)]);
    skip_row("keyed-zero-bit-above-count", &names, &keyed_names(), &[(1, 2), (0, 2)]);

    let mut dup_storage = [TableMessageEntry::default(); 3];
    dup_storage[0].id = 100; dup_storage[0].kind = 0;
    dup_storage[1].id = 101; dup_storage[1].kind = 0;
    dup_storage[2].id = 100; dup_storage[2].kind = 0;
    let mut dup = TableVocabulary::new(&mut dup_storage);
    dup.count = 3;
    dup.ref_bits = 2;
    dup.announced = true;
    nested_row("nested-id-two-refs-distinct", &dup, 1, 2);
    nested_row("nested-id-two-refs", &dup, 1, 3);

    let mut big_storage = vec![TableMessageEntry::default(); 4097];
    for i in 0..4097 { big_storage[i].id = (i + 1) as u64; big_storage[i].kind = 0; }
    let mut big = TableVocabulary::new(&mut big_storage);
    big.count = 4097;
    big.ref_bits = table_message_bits(4097);
    big.announced = true;
    nested_row("nested-id-slot-past-4096-distinct", &big, 4096, 4097);
    nested_row("nested-id-slot-past-4096", &big, 4096, 4096);
`)
	body.WriteString("}\n")

	build := t.TempDir()
	layoutRustUnit(t, build, "probe", modules)
	main := "#![allow(dead_code, unused_imports)]\nmod probe;\nuse probe::*;\n\n" + body.String()
	if err := os.WriteFile(filepath.Join(build, "main.rs"), []byte(main), 0o600); err != nil {
		t.Fatal(err)
	}
	args := []string{"--edition", "2024"}
	if cc, err := exec.LookPath("cc"); err == nil {
		args = append(args, "-C", "linker="+cc)
	}
	args = append(args, "-o", "run", "main.rs")
	cmd := exec.Command("rustc", args...)
	cmd.Dir = build
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the probe did not build: %v\n%s", err, out)
	}
	out, err := exec.Command(filepath.Join(build, "run")).CombinedOutput()
	if err != nil {
		t.Fatalf("the probe did not run: %v\n%s", err, out)
	}

	got := map[int]string{}
	extra := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		if strings.HasPrefix(line, "ROW\t") {
			f := strings.Split(line, "\t")
			if len(f) != 3 {
				t.Fatalf("the probe answered %q", line)
			}
			extra[f[1]] = f[2]
			continue
		}
		var index int
		var verdict string
		if _, err := fmt.Sscanf(line, "%d %s", &index, &verdict); err != nil {
			t.Fatalf("the probe answered %q: %v", line, err)
		}
		got[index] = verdict
	}
	if len(got) != len(rows) {
		t.Fatalf("the probe answered %d rows of %d:\n%s", len(got), len(rows), out)
	}
	for i, r := range rows {
		if got[i] != r.verdict {
			t.Errorf("%s (%s): verdict %s, want %s", r.file, r.form, got[i], r.verdict)
		}
	}
	for name, want := range rustSkippedShapeVerdicts {
		if extra[name] != want {
			t.Errorf("%s: verdict %s, want %s", name, extra[name], want)
		}
	}
}

// rustSkippedShapeVerdicts holds the shapes the shared vectors cannot carry: a
// ranged value's offset over its kind's domain, as a field and as a walked
// array element, and a keyed array whose slots carry no bits but whose keys
// still resolve. It also holds one id announced at two slots and a lawful
// reference past the old 4096 cap.
var rustSkippedShapeVerdicts = map[string]string{
	"ranged-i8-at-max":                  "read",
	"ranged-i8-over-max":                "refused",
	"ranged-u8-at-max":                  "read",
	"ranged-u8-over-max":                "refused",
	"ranged-u64-at-max":                 "read",
	"ranged-u64-over-max":               "refused",
	"ranged-i128-at-max":                "read",
	"ranged-i128-over-max":              "refused",
	"ranged-element-at-max":             "read",
	"ranged-element-over-max":           "refused",
	"keyed-zero-bit-distinct":           "read",
	"keyed-zero-bit-repeat":             "refused",
	"keyed-zero-bit-payload-entry":      "refused",
	"keyed-zero-bit-above-count":        "refused",
	"nested-id-two-refs-distinct":       "read",
	"nested-id-two-refs":                "refused",
	"nested-id-slot-past-4096-distinct": "read",
	"nested-id-slot-past-4096":          "refused",
	"announce-elem-12":                  "read",
	"announce-elem-14":                  "refused",
	"announce-elem-16":                  "refused",
	"announce-keyed-elem-12":            "refused",
}

// testMessageAnnouncement frames one vocabulary entry as the announcement a
// connection carries, in the same form messageAnnouncement writes: a form-1
// file whose body holds the build version and the vocabulary under their
// reserved ids. These entries are a few bytes, so every length here is one
// LEB128 byte.
func testMessageAnnouncement(entry ir.TableVocabularyEntry) []byte {
	vocabulary := ir.TableVocabularyBytes([]ir.TableVocabularyEntry{entry})
	body := []byte{uint8(ir.TableKindU8), byte(len(vocabulary))}
	body = append(body, vocabulary...)
	out := []byte{ir.TableWireForm, 1, uint8(ir.TableKindU64)}
	out = append(out, make([]byte, 8)...)
	out = append(out, 2, uint8(ir.TableKindArray), byte(len(body)))
	out = append(out, body...)
	out = append(out, 0)
	out = binary.LittleEndian.AppendUint64(out, ir.TableBuildVersionWireId)
	out = binary.LittleEndian.AppendUint64(out, ir.TableMessageVocabularyWireId)
	return binary.LittleEndian.AppendUint64(out, 2)
}
