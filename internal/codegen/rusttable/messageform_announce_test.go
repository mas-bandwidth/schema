package rusttable

// TestMessageFormAnnounceRust holds the MESSAGE FORM's announcement surface
// (docs/SPEC-TABLES.md §3.3) to the Rust leg. The message form is a batch of
// bitpacked bodies under one announced vocabulary; the announcement is an
// ordinary form-1 FILE the compiler settles byte for byte, so a backend emits
// it as a compile-time constant byte array and its length rather than as a
// walk. The bytes are the C++ reference's, pinned by
// testdata/conformance/tables/MANIFEST.txt's `connection` rows.
//
// One subtest per clause of STEP 2. Clause 2.1 compares the emitted constant
// byte for byte to the manifest's file; clauses 2.2 and 2.3 are proved by a
// real Rust binary this test BUILDS AND RUNS with rustc alone (the announcement
// runtime is self-contained, exactly as the fixed form's is), so a clause reads
// the running binary's own verdict line and never a string search.

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestMessageFormAnnounceRust(t *testing.T) {
	t.Parallel()
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt")
	conns := manifestConnections(t, manifest)
	units := map[string][]string{}
	for _, line := range readManifest(t, manifest) {
		fields := strings.Fields(line)
		if len(fields) >= 3 && fields[0] == "unit" {
			units[fields[1]] = fields[2:]
		}
	}

	loaded := map[string]*announceUnit{}
	for _, conn := range conns {
		if conn.unit != "backenddemo" && conn.unit != "vocabdemo" && conn.unit != "vocab9demo" {
			continue
		}
		paths, ok := units[conn.unit]
		if !ok {
			t.Fatalf("manifest names no unit %q", conn.unit)
		}
		u := generateUnit(t, root, paths)
		out, err := Generate(u)
		if err != nil {
			t.Fatalf("generate %s: %v", conn.unit, err)
		}
		want, err := os.ReadFile(filepath.Join(root, conn.path))
		if err != nil {
			t.Fatal(err)
		}
		loaded[conn.unit] = &announceUnit{
			buildVersion: conn.buildVersion,
			announcement: want,
			entries:      ir.TableVocabulary(u),
			files:        out,
		}
	}
	if len(loaded) != 3 {
		t.Fatalf("the manifest does not name all three units")
	}

	// 2.1 "Announce and AnnounceMeasure are a compile-time constant byte array
	// and its length, equal byte for byte to the manifest's announcement file."
	t.Run("2.1_announcement_is_a_compile_time_constant", func(t *testing.T) {
		t.Parallel()
		for _, name := range []string{"backenddemo", "vocabdemo", "vocab9demo"} {
			name := name
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				module, ok := loaded[name].files[MessageRuntimeModule+".rs"]
				if !ok {
					t.Fatalf("no %s.rs; the announcement surface is absent", MessageRuntimeModule)
				}
				got := announcementBytes(t, string(module))
				if !bytes.Equal(got, loaded[name].announcement) {
					t.Fatalf("the announcement differs from the reference: %d bytes vs %d", len(got), len(loaded[name].announcement))
				}
			})
		}
	})

	// 2.2 "AnnounceRead accepts each of those files and fills TableVocabulary
	// with the entries the page names, the build version kept" and 2.3 "the
	// refusals second_announcement (and refusal is terminal) and
	// vocabulary_too_large (both bounds), and the two strict checks answered
	// malformed" are proved by the running reader over the pinned bytes and
	// their forgeries.
	for _, name := range []string{"backenddemo", "vocabdemo", "vocab9demo"} {
		name := name
		out := runAnnounceProbe(t, loaded[name])
		for _, id := range []string{
			"measure",
			"read",
			"second_announcement",
			"second_announcement_terminal",
			"vocabulary_too_large_bytes",
			"vocabulary_too_large_entries",
			"malformed_missing_version",
			"malformed_missing_vocabulary",
			"malformed_noncanonical",
			"unknown_scalar",
			"unknown_array",
			"unknown_table",
		} {
			id := id
			t.Run(name+"/"+id, func(t *testing.T) {
				t.Parallel()
				if line := probeLine(out, id); line != "ANNOUNCE "+id+" ok" {
					t.Fatalf("the rust leg did not hold the announcement surface: %s", line)
				}
			})
		}
	}
}

// announceUnit is one unit's pinned announcement facts and generated modules.
type announceUnit struct {
	buildVersion uint64
	announcement []byte
	entries      []ir.TableVocabularyEntry
	files        map[string][]byte
}

type manifestConnection struct {
	unit         string
	buildVersion uint64
	path         string
}

// manifestConnections reads only the `connection` rows this card's surface
// answers: the unit whose vocabulary the announcement carries, the build
// version that keys it, and the file that pins its bytes.
func manifestConnections(t *testing.T, path string) []manifestConnection {
	t.Helper()
	var out []manifestConnection
	for _, line := range readManifest(t, path) {
		fields := strings.Fields(line)
		if len(fields) >= 5 && fields[0] == "connection" {
			version, err := strconv.ParseUint(fields[3], 0, 64)
			if err != nil {
				t.Fatalf("connection row %q: bad build version: %v", line, err)
			}
			out = append(out, manifestConnection{unit: fields[2], buildVersion: version, path: fields[4]})
		}
	}
	if len(out) == 0 {
		t.Fatalf("%s carries no connection rows", path)
	}
	return out
}

func readManifest(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(string(data), "\n")
}

// generateUnit parses one unit's source paths and runs the checker.
func generateUnit(t *testing.T, root string, paths []string) *ir.Unit {
	t.Helper()
	var files []check.SourceFile
	for _, p := range paths {
		abs := filepath.Join(root, p)
		info, err := os.Stat(abs)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		if info.IsDir() {
			entries, err := os.ReadDir(abs)
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range entries {
				if !e.IsDir() && strings.HasSuffix(e.Name(), ".schema") && len(e.Name()) > len(".schema") {
					names = append(names, filepath.Join(abs, e.Name()))
				}
			}
		} else {
			names = append(names, abs)
		}
		for _, name := range names {
			data, err := os.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			f, perrs := parser.Parse(name, data)
			if len(perrs) > 0 {
				t.Fatalf("parse %s: %v", name, perrs[0])
			}
			files = append(files, check.SourceFile{
				Path: name, Name: filepath.Base(name), Base: strings.TrimSuffix(filepath.Base(name), ".schema"),
				Bytes: data, AST: f,
			})
		}
	}
	u, cerrs := check.Unit(files)
	if len(cerrs) > 0 {
		t.Fatalf("check %v: %v", paths, cerrs[0])
	}
	return u
}

var announceArray = regexp.MustCompile(`TABLE_ANNOUNCEMENT: \[u8; (\d+)\] = \[([^\]]*)\]`)

// announcementBytes reads the emitted constant back, so the comparison is
// against the bytes the compiler would place in the crate, not a copy.
func announcementBytes(t *testing.T, module string) []byte {
	t.Helper()
	m := announceArray.FindStringSubmatch(module)
	if m == nil {
		t.Fatal("no TABLE_ANNOUNCEMENT array in the emitted module")
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		t.Fatal(err)
	}
	var out []byte
	for _, tok := range strings.Split(m[2], ",") {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		v, err := strconv.ParseUint(strings.TrimPrefix(tok, "0x"), 16, 8)
		if err != nil {
			t.Fatalf("bad byte literal %q: %v", tok, err)
		}
		out = append(out, byte(v))
	}
	if len(out) != n {
		t.Fatalf("the array length says %d, the literals number %d", n, len(out))
	}
	return out
}

// ---- the probe --------------------------------------------------------------

// runAnnounceProbe writes one unit's announcement runtime beside a crate root,
// builds it with rustc and runs it once. The probe is a real Rust binary:
// announce_read runs over the pinned bytes and their forgeries, and each clause
// reads its own verdict line.
func runAnnounceProbe(t *testing.T, u *announceUnit) string {
	t.Helper()
	if _, err := exec.LookPath("rustc"); err != nil {
		t.Fatalf("rustc is not on PATH: %v", err)
	}
	root := t.TempDir()
	for _, module := range []string{BuildVersionModule + ".rs", MessageRuntimeModule + ".rs"} {
		data, ok := u.files[module]
		if !ok {
			t.Fatalf("no %s in the generated unit", module)
		}
		if err := os.WriteFile(filepath.Join(root, module), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "main.rs"), []byte(announceProbe(u)), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(root, "announceprobe")
	cmd := exec.Command("rustc", "--edition", "2024", "-O", "-o", bin, "main.rs")
	cmd.Dir = root
	cmd.Env = os.Environ()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the probe did not build: %v\n%s", err, out)
	}
	run := exec.Command(bin)
	run.Dir = root
	out, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("the probe did not run: %v\n%s", err, out)
	}
	return string(out)
}

// announceProbe is the crate root: the pinned announcement bytes and their
// forgeries embedded, and one verdict line per clause of STEP 2.
func announceProbe(u *announceUnit) string {
	var b strings.Builder
	b.WriteString("mod build_version;\nmod message_runtime;\npub use build_version::*;\npub use message_runtime::*;\n\n")
	b.WriteString("fn say(id: &str, ok: bool, why: &str) {\n")
	b.WriteString("    if ok { println!(\"ANNOUNCE {id} ok\"); } else { println!(\"ANNOUNCE {id} BAD {why}\"); }\n")
	b.WriteString("}\n\n")

	fmt.Fprintf(&b, "const ANNOUNCE: [u8; %d] = [%s];\n", len(u.announcement), rustBytes(u.announcement))
	fmt.Fprintf(&b, "const EXPECTED_BUILD: u64 = 0x%016x;\n", u.buildVersion)
	fmt.Fprintf(&b, "const EXPECTED_COUNT: usize = %d;\n", len(u.entries))
	var ids, kinds []string
	for _, e := range u.entries {
		ids = append(ids, fmt.Sprintf("0x%016x", e.Id))
		kinds = append(kinds, strconv.Itoa(int(e.Kind)))
	}
	fmt.Fprintf(&b, "const EXPECTED_IDS: [u64; %d] = [%s];\n", len(u.entries), strings.Join(ids, ", "))
	fmt.Fprintf(&b, "const EXPECTED_KINDS: [u8; %d] = [%s];\n\n", len(u.entries), strings.Join(kinds, ", "))

	// THE TWO STRICT CHECKS (§3.3), each answered malformed: a body with the
	// build version field gone, and one with the vocabulary field gone.
	missingVersion := append([]byte{u.announcement[0]}, u.announcement[11:]...)
	missingVocab := append(append([]byte{}, u.announcement[:11]...), 0x00)
	missingVocab = append(missingVocab, u.announcement[len(u.announcement)-24:]...)
	fmt.Fprintf(&b, "const MISSING_VERSION: [u8; %d] = [%s];\n", len(missingVersion), rustBytes(missingVersion))
	fmt.Fprintf(&b, "const MISSING_VOCAB: [u8; %d] = [%s];\n\n", len(missingVocab), rustBytes(missingVocab))

	// 2.3: "Every other field of its body is ordinary and tolerant, so an
	// unknown one is skipped and counted" (§3.3) — the announcement can gain a
	// field in a later minor without a lockstep redeploy. Three forgeries each
	// add ONE unknown field of a different framing to the pinned announcement:
	// a fixed-width scalar, an array, and a nested table body.
	unknownScalar := forgedWithField(u.announcement, 9, []byte{0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88}, 0x1111111111111111)
	unknownArray := forgedWithField(u.announcement, 14, []byte{0x05, 0x06, 0x03, 0xaa, 0xbb, 0xcc}, 0x2222222222222222)
	unknownTable := forgedWithField(u.announcement, 13, []byte{0x01, 0x00}, 0x3333333333333333)
	fmt.Fprintf(&b, "const UNKNOWN_SCALAR: [u8; %d] = [%s];\n", len(unknownScalar), rustBytes(unknownScalar))
	fmt.Fprintf(&b, "const UNKNOWN_ARRAY: [u8; %d] = [%s];\n", len(unknownArray), rustBytes(unknownArray))
	fmt.Fprintf(&b, "const UNKNOWN_TABLE: [u8; %d] = [%s];\n\n", len(unknownTable), rustBytes(unknownTable))

	// 2.3: "An encoding past ten bytes, or a tenth byte with a bit above the
	// 64th value bit, is malformed on the same rule" (docs/SPEC-TABLES.md:3707):
	// a minimal announcement whose vocabulary element length is a ten-byte LEB
	// with its tenth byte above 0x01.
	nonCanonical := forgedNonCanonicalLength()
	fmt.Fprintf(&b, "const NONCANONICAL: [u8; %d] = [%s];\n\n", len(nonCanonical), rustBytes(nonCanonical))

	b.WriteString("fn main() {\n")
	// 2.1: the constant and its length.
	b.WriteString("    say(\"measure\", announce_measure() == ANNOUNCE.len() && TABLE_ANNOUNCEMENT == ANNOUNCE, \"the constant or its measure differs\");\n")
	// 2.2: the read fills the vocabulary the page names, the build version kept.
	b.WriteString("    {\n        let mut storage = vec![TableMessageEntry::default(); 4096];\n")
	b.WriteString("        let mut vocab = TableVocabulary::new(&mut storage);\n        let mut report = TableReport::default();\n")
	b.WriteString("        let ok = announce_read(&mut vocab, &ANNOUNCE, &mut report);\n")
	b.WriteString("        say(\"read\", ok && !report.refused && !report.malformed && vocab.count == EXPECTED_COUNT && vocab.build_version == EXPECTED_BUILD && vocab.announced && (0..vocab.count).all(|i| vocab.entries[i].id == EXPECTED_IDS[i] && vocab.entries[i].kind == EXPECTED_KINDS[i]), \"the announcement did not fill the vocabulary the page names\");\n    }\n")
	// 2.3: a second announcement is refused by name.
	b.WriteString("    {\n        let mut storage = vec![TableMessageEntry::default(); 4096];\n")
	b.WriteString("        let mut vocab = TableVocabulary::new(&mut storage);\n        let mut report = TableReport::default();\n")
	b.WriteString("        let _ = announce_read(&mut vocab, &ANNOUNCE, &mut report);\n")
	b.WriteString("        let mut report2 = TableReport::default();\n        let ok = announce_read(&mut vocab, &ANNOUNCE, &mut report2);\n")
	b.WriteString("        say(\"second_announcement\", !ok && report2.refused && report2.reason == TableReason::SecondAnnouncement && !report2.malformed, \"a second announcement is not refused by name\");\n    }\n")
	// 2.3: refusal is terminal — a refused FIRST announcement carries none for
	// the connection's life.
	b.WriteString("    {\n        let mut storage = vec![TableMessageEntry::default(); 4096];\n")
	b.WriteString("        let mut vocab = TableVocabulary::new(&mut storage);\n        vocab.max_bytes = 1;\n        let mut report = TableReport::default();\n")
	b.WriteString("        let _ = announce_read(&mut vocab, &ANNOUNCE, &mut report);\n")
	b.WriteString("        let mut report2 = TableReport::default();\n        let ok = announce_read(&mut vocab, &ANNOUNCE, &mut report2);\n")
	b.WriteString("        say(\"second_announcement_terminal\", !ok && report2.refused && report2.reason == TableReason::SecondAnnouncement, \"a refused first announcement is not terminal\");\n    }\n")
	// 2.3: vocabulary_too_large, the byte bound.
	b.WriteString("    {\n        let mut storage = vec![TableMessageEntry::default(); 4096];\n")
	b.WriteString("        let mut vocab = TableVocabulary::new(&mut storage);\n        vocab.max_bytes = 16;\n        let mut report = TableReport::default();\n")
	b.WriteString("        let ok = announce_read(&mut vocab, &ANNOUNCE, &mut report);\n")
	b.WriteString("        say(\"vocabulary_too_large_bytes\", !ok && report.refused && report.reason == TableReason::VocabularyTooLarge, \"a vocabulary past the byte bound is not refused by name\");\n    }\n")
	// 2.3: vocabulary_too_large, the entry bound.
	b.WriteString("    {\n        let mut storage = vec![TableMessageEntry::default(); 4];\n")
	b.WriteString("        let mut vocab = TableVocabulary::new(&mut storage);\n        let mut report = TableReport::default();\n")
	b.WriteString("        let ok = announce_read(&mut vocab, &ANNOUNCE, &mut report);\n")
	b.WriteString("        say(\"vocabulary_too_large_entries\", !ok && report.refused && report.reason == TableReason::VocabularyTooLarge, \"an entry count past the caller's capacity is not refused by name\");\n    }\n")
	// 2.3: the two strict checks, malformed and never a refusal.
	b.WriteString("    {\n        let mut storage = vec![TableMessageEntry::default(); 4096];\n")
	b.WriteString("        let mut vocab = TableVocabulary::new(&mut storage);\n        let mut report = TableReport::default();\n")
	b.WriteString("        let ok = announce_read(&mut vocab, &MISSING_VERSION, &mut report);\n")
	b.WriteString("        say(\"malformed_missing_version\", !ok && report.malformed && !report.refused, \"a body without the build version is not malformed\");\n    }\n")
	b.WriteString("    {\n        let mut storage = vec![TableMessageEntry::default(); 4096];\n")
	b.WriteString("        let mut vocab = TableVocabulary::new(&mut storage);\n        let mut report = TableReport::default();\n")
	b.WriteString("        let ok = announce_read(&mut vocab, &MISSING_VOCAB, &mut report);\n")
	b.WriteString("        say(\"malformed_missing_vocabulary\", !ok && report.malformed && !report.refused, \"a body without the vocabulary is not malformed\");\n    }\n")
	// 2.3: a non-canonical ten-byte LEB is malformed, never a refusal.
	b.WriteString("    {\n        let mut storage = vec![TableMessageEntry::default(); 4096];\n")
	b.WriteString("        let mut vocab = TableVocabulary::new(&mut storage);\n        let mut report = TableReport::default();\n")
	b.WriteString("        let ok = announce_read(&mut vocab, &NONCANONICAL, &mut report);\n")
	b.WriteString("        say(\"malformed_noncanonical\", !ok && report.malformed && !report.refused, \"a non-canonical ten-byte LEB is not malformed\");\n    }\n")
	// 2.3: an unknown field is skipped and counted, the two strict checks kept:
	// the read still fills the vocabulary the page names, the build version kept.
	for _, c := range []struct{ id, cnst, why string }{
		{"unknown_scalar", "UNKNOWN_SCALAR", "an unknown scalar field is not skipped tolerantly"},
		{"unknown_array", "UNKNOWN_ARRAY", "an unknown array field is not skipped tolerantly"},
		{"unknown_table", "UNKNOWN_TABLE", "an unknown table field is not skipped tolerantly"},
	} {
		fmt.Fprintf(&b, "    {\n        let mut storage = vec![TableMessageEntry::default(); 4096];\n")
		fmt.Fprintf(&b, "        let mut vocab = TableVocabulary::new(&mut storage);\n        let mut report = TableReport::default();\n")
		fmt.Fprintf(&b, "        let ok = announce_read(&mut vocab, &%s, &mut report);\n", c.cnst)
		fmt.Fprintf(&b, "        say(\"%s\", ok && !report.refused && !report.malformed && vocab.count == EXPECTED_COUNT && vocab.build_version == EXPECTED_BUILD && report.unknown == 1, \"%s\");\n    }\n", c.id, c.why)
	}
	b.WriteString("}\n")
	return b.String()
}

func probeLine(out, id string) string {
	prefix := "ANNOUNCE " + id + " "
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, prefix) {
			return l
		}
	}
	return fmt.Sprintf("[no ANNOUNCE line for %s in the probe's output: %s]", id, strings.TrimSpace(out))
}

func rustBytes(b []byte) string {
	var s strings.Builder
	for i, v := range b {
		if i > 0 {
			s.WriteString(",")
		}
		fmt.Fprintf(&s, "0x%02x", v)
	}
	return s.String()
}

// forgedWithField appends one unknown field to a pinned announcement: a third
// trailer entry carrying the field's id and a third body field of the given
// kind and payload, so the tolerant skip meets a field of an id it does not
// know. The two strict fields stay exactly where they were.
func forgedWithField(announcement []byte, kind byte, payload []byte, id uint64) []byte {
	count := binary.LittleEndian.Uint64(announcement[len(announcement)-8:])
	end := len(announcement) - 8 - int(count)*8
	out := []byte{announcement[0]}
	out = append(out, announcement[1:end-1]...)
	out = append(out, 0x03, kind)
	out = append(out, payload...)
	out = append(out, 0x00)
	out = append(out, announcement[end:len(announcement)-8]...)
	out = binary.LittleEndian.AppendUint64(out, id)
	return binary.LittleEndian.AppendUint64(out, count+1)
}

// forgedNonCanonicalLength is a minimal announcement whose only defect is one
// non-canonical LEB128: the vocabulary field's element length is spelled in
// ten bytes — nine 0x80 continuation bytes and a final 0x02 — so the tenth
// byte carries a bit above the 64th value bit. "An encoding past ten bytes, or
// a tenth byte with a bit above the 64th value bit, is malformed on the same
// rule" (docs/SPEC-TABLES.md:3707). Both strict fields are present and valid,
// so a reader that drops the tenth byte's high bit accepts a zero-entry
// vocabulary; a reader that holds the rule answers malformed.
func forgedNonCanonicalLength() []byte {
	out := []byte{0x01}                                                           // the form byte
	out = append(out, 0x01, 0x09)                                                 // the build version: reference 1, kind 9
	out = binary.LittleEndian.AppendUint64(out, 0x1234)                           // its eight bytes
	out = append(out, 0x02, 0x0e, 0x0b)                                           // the vocabulary: reference 2, kind 14, framed length 11
	out = append(out, 0x06)                                                       // element kind 6
	out = append(out, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x02) // the non-canonical ten-byte length
	out = append(out, 0x00)                                                       // the body terminator
	out = binary.LittleEndian.AppendUint64(out, 0xffff_ffff_ffff_fffe)            // slot 1: the build version id
	out = binary.LittleEndian.AppendUint64(out, 0xffff_ffff_ffff_fffd)            // slot 2: the vocabulary id
	return binary.LittleEndian.AppendUint64(out, 2)                               // the entry count
}
