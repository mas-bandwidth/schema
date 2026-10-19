package jstable

import (
	"encoding/binary"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/js"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestMessageFormAnnounceJavaScript holds the ANNOUNCEMENT half of the message
// form (docs/SPEC-TABLES.md §3.3) on the JavaScript leg, one subtest per
// clause, against the announcement files the tables manifest pins. The bytes
// ARE the C++ reference's; the manifest names the units.
//
// EVERY CLAUSE IS ASSERTED OVER THE EMITTED MODULE, not a Go model of it: the
// generated tree is written and run under the leg's own node (jsNode), the way
// fixedversioning_test.go runs its columns. A surface the leg does not emit, or
// emits broken, turns a subtest red here.
func TestMessageFormAnnounceJavaScript(t *testing.T) {
	t.Parallel()

	type unitCase struct {
		name string
		path string
	}
	units := []unitCase{
		{"backenddemo", "../../../tables/backend/Backend.schema"},
		{"vocabdemo", "../../../tables/vocab/Vocab.schema"},
		{"vocab9demo", "../../../tables/vocab9/Vocab9.schema"},
	}
	announcementOf := manifestAnnouncements(t)

	t.Run("constant_bytes", func(t *testing.T) {
		t.Parallel()
		// "THE WHOLE ANNOUNCEMENT IS A COMPILE-TIME CONSTANT OF THE UNIT ...
		// a backend emits `Announce` and `AnnounceMeasure` as a constant byte
		// array and its length rather than as a walk." (docs/SPEC-TABLES.md §3.3)
		for _, uc := range units {
			t.Run(uc.name, func(t *testing.T) {
				t.Parallel()
				u := loadUnit(t, uc.path)
				src := homeModule(t, u)
				for _, want := range []string{"export function Announce(", "export function AnnounceMeasure(", "export class TableVocabulary"} {
					if !strings.Contains(src, want) {
						t.Fatalf("the emitted unit does not carry %q", want)
					}
				}
				got := emittedAnnouncement(t, src)
				want := announcementOf[uc.name]
				if len(want) == 0 {
					t.Fatalf("the manifest carries no announcement file for %s", uc.name)
				}
				if string(got) != string(want) {
					t.Fatalf("Announce bytes = %d, the manifest's %s = %d — they disagree", len(got), uc.name, len(want))
				}
				// The length AnnounceMeasure answers IS the constant array's
				// length, not a walk's — "a constant byte array and its length
				// rather than as a walk" (docs/SPEC-TABLES.md §3.3).
				if !strings.Contains(src, "export function Announce(buffer) {") ||
					!strings.Contains(src, "export function AnnounceMeasure() { return TableMessageAnnouncement.length; }") {
					t.Fatal("Announce and AnnounceMeasure must ride as the constant array and its length")
				}
			})
		}
	})

	t.Run("announce_read_fills", func(t *testing.T) {
		t.Parallel()
		// "The receiver resolves once. It reads the announcement, resolves
		// every entry against its own descriptors ... THE ANNOUNCEMENT READS
		// TOLERANTLY ... The vocabulary is a pure function of the build
		// version." (docs/SPEC-TABLES.md §3.3)
		node := jsNode(t)
		for _, uc := range units {
			t.Run(uc.name, func(t *testing.T) {
				t.Parallel()
				u := loadUnit(t, uc.path)
				entries := ir.TableVocabulary(u)
				var want strings.Builder
				want.WriteString("const want = [\n")
				for _, e := range entries {
					fmt.Fprintf(&want, "  [BigInt(\"0x%016x\"), %d],\n", e.Id, e.Kind)
				}
				want.WriteString("];\n")
				probe := fmt.Sprintf(`
const data = new Uint8Array(readFileSync("./ann.bin"));
const v = new TableVocabulary();
v.Init(new Array(%[1]d));
const report = {};
if (!AnnounceRead(v, data, report)) { fail("AnnounceRead refused a well-formed announcement: " + JSON.stringify(report)); }
if (!v.Announced || v.Refused) { fail("the announcement did not set the vocabulary: " + JSON.stringify(report)); }
if (v.Count !== %[1]d) { fail("resolved " + v.Count + " entries, the unit's closure names %[1]d"); }
				if (v.BuildVersion !== BigInt("0x%[2]s")) { fail("build version = 0x" + v.BuildVersion.toString(16)); }
				if (v.RefBits !== %[3]d) { fail("reference width = " + v.RefBits + ", the closure needs %[3]d"); }
				%[4]s
				for (let i = 0; i < want.length; i++) {
				  if (v.Entries[i].Id !== want[i][0] || v.Entries[i].Shape.Kind !== want[i][1]) {
				    fail("entry " + i + " = id 0x" + v.Entries[i].Id.toString(16) + " kind " + v.Entries[i].Shape.Kind +
				      ", the page names id 0x" + want[i][0].toString(16) + " kind " + want[i][1]);
				  }
				}
				`, len(entries), fmt.Sprintf("%016x", ir.BuildVersion(u)), ir.TableMessageRefBits(len(entries)), want.String())
				out, err := jsRunAnnounceProbe(t, node, u, map[string][]byte{"ann.bin": announcementOf[uc.name]}, probe)
				if err != nil {
					t.Fatalf("AnnounceRead did not fill %s: %v\n%s", uc.name, err, out)
				}
			})
		}
	})

	t.Run("second_announcement", func(t *testing.T) {
		t.Parallel()
		// "NO RE-ANNOUNCEMENT, EVER ... A second is refused by name and does
		// not replace or amend anything." and "REFUSAL IS TERMINAL, and a
		// refused FIRST announcement is the one the connection gets ... every
		// announcement after it is refused as `second_announcement`."
		// (docs/SPEC-TABLES.md §3.3)
		node := jsNode(t)
		u := loadUnit(t, units[0].path)
		n := len(ir.TableVocabulary(u))
		probe := fmt.Sprintf(`
const data = new Uint8Array(readFileSync("./ann.bin"));
const v = new TableVocabulary();
v.Init(new Array(%[1]d));
const first = {};
if (!AnnounceRead(v, data, first)) { fail("first announcement: " + JSON.stringify(first)); }
const before = v.Count;
const second = {};
if (AnnounceRead(v, data, second) || second.Verdict !== 1 || second.Reason !== "second_announcement" || second.Malformed) {
  fail("a second announcement was not refused by name: " + JSON.stringify(second));
}
if (v.Count !== before) { fail("a second announcement replaced the vocabulary"); }
const w = new TableVocabulary();
w.Init(new Array(%[1]d));
const rejected = {};
if (AnnounceRead(w, new Uint8Array([9, 9, 9]), rejected) || rejected.Verdict !== 1 || rejected.Reason !== "newer_form" || w.Announced) {
  fail("a refused first announcement was not refused: " + JSON.stringify(rejected));
}
const later = {};
if (AnnounceRead(w, data, later) || later.Reason !== "second_announcement" || w.Announced) {
  fail("an announcement after a refused first is not second_announcement: " + JSON.stringify(later));
}
`, n)
		out, err := jsRunAnnounceProbe(t, node, u, map[string][]byte{"ann.bin": announcementOf["backenddemo"]}, probe)
		if err != nil {
			t.Fatalf("second_announcement: %v\n%s", err, out)
		}
	})

	t.Run("vocabulary_too_large", func(t *testing.T) {
		t.Parallel()
		// "`vocabulary_too_large` covers two bounds: ... THE ENTRY BOUND IS
		// THAT CAPACITY, and an announcement above it is refused by name
		// `vocabulary_too_large` before an entry is touched" and the byte
		// bound's "64 KiB does not move" (docs/SPEC-TABLES.md §3.3)
		node := jsNode(t)
		u := loadUnit(t, units[0].path)
		probe := `
const data = new Uint8Array(readFileSync("./ann.bin"));
const entries = new TableVocabulary();
entries.Init(new Array(1));
const report = {};
if (AnnounceRead(entries, data, report) || report.Verdict !== 1 || report.Reason !== "vocabulary_too_large" || report.Malformed || entries.Announced) {
  fail("entry count bound: " + JSON.stringify(report));
}
const bytes = new TableVocabulary();
bytes.Init(new Array(64));
bytes.MaxBytes = 4;
const byteReport = {};
if (AnnounceRead(bytes, data, byteReport) || byteReport.Verdict !== 1 || byteReport.Reason !== "vocabulary_too_large" || byteReport.Malformed || bytes.Announced) {
  fail("vocabulary byte bound: " + JSON.stringify(byteReport));
}
`
		out, err := jsRunAnnounceProbe(t, node, u, map[string][]byte{"ann.bin": announcementOf["backenddemo"]}, probe)
		if err != nil {
			t.Fatalf("vocabulary_too_large: %v\n%s", err, out)
		}
	})

	t.Run("announcement_strict_checks", func(t *testing.T) {
		t.Parallel()
		// "THE ANNOUNCEMENT READS TOLERANTLY, WITH EXACTLY TWO STRICT CHECKS:
		// the build version present, exactly once, kind `9`, eight bytes wide,
		// and the vocabulary present, exactly once, kind `14`, element kind
		// `6` ... A FAILED STRICT CHECK IS MALFORMED, not a refusal ... The
		// build version is kept the moment it is read." (docs/SPEC-TABLES.md §3.3)
		node := jsNode(t)
		u := loadUnit(t, units[0].path)
		const version = 0x1122334455667788
		twice := append(versionField(version), versionField(version)...)
		twice = append(twice, vocabField(6, nil)...)
		short := append([]byte{1, 9}, 0, 0, 0, 0)
		cases := []struct {
			name string
			data []byte
		}{
			{"build version missing", announcement(vocabField(6, nil))},
			{"build version twice", announcement(twice)},
			{"build version not kind 9", announcement(append([]byte{1, 5, 0, 0, 0, 0, 0, 0, 0, 0}, vocabField(6, nil)...))},
			{"build version fewer than eight bytes", announcement(short)},
			{"vocabulary missing", announcement(versionField(version))},
			{"vocabulary not kind 14", announcement(append(versionField(version), vocabFieldBadKind(13)...))},
			{"vocabulary element not kind 6", announcement(append(versionField(version), vocabField(9, nil)...))},
		}
		extra := map[string][]byte{"keep.bin": announcement(append(versionField(version), 2, 13))}
		var list strings.Builder
		list.WriteString("const cases = [\n")
		for i, tc := range cases {
			file := fmt.Sprintf("case%d.bin", i)
			extra[file] = tc.data
			fmt.Fprintf(&list, "  [%q, %q],\n", tc.name, file)
		}
		list.WriteString("];\n")
		probe := fmt.Sprintf(`
%[1]s
for (const [name, path] of cases) {
  const v = new TableVocabulary();
  v.Init(new Array(8));
  const report = {};
  if (AnnounceRead(v, new Uint8Array(readFileSync("./" + path)), report) || !report.Malformed || report.Verdict === 1 || v.Announced) {
    fail(name + ": want malformed and not a refusal, got " + JSON.stringify(report));
  }
}
const keep = new TableVocabulary();
keep.Init(new Array(8));
const keepReport = {};
if (AnnounceRead(keep, new Uint8Array(readFileSync("./keep.bin")), keepReport) || !keepReport.Malformed ||
    keep.BuildVersion !== BigInt("0x%[2]s") || keep.Announced) {
  fail("the refused announcement did not keep the build version: " + JSON.stringify(keepReport) + " version=0x" + keep.BuildVersion.toString(16));
}
`, list.String(), fmt.Sprintf("%016x", uint64(version)))
		out, err := jsRunAnnounceProbe(t, node, u, extra, probe)
		if err != nil {
			t.Fatalf("announcement_strict_checks: %v\n%s", err, out)
		}
	})
}

// jsRunAnnounceProbe writes the unit's whole generated tree — the packet
// modules and the table modules — plus `extra` files into one directory and
// runs `probe` there with the leg's own node. `probe` imports the announcement
// surface from the emitted home module and calls `fail(why)` on any breach; the
// process exits non-zero, and the combined output names the clause.
func jsRunAnnounceProbe(t *testing.T, node string, u *ir.Unit, extra map[string][]byte, probe string) ([]byte, error) {
	t.Helper()
	files, err := js.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	home := ""
	for name, data := range tables {
		if strings.Contains(string(data), "TableMessageAnnouncement = new Uint8Array(") {
			home = name
		}
	}
	if home == "" {
		t.Fatal("the generated unit carries no announcement surface")
	}
	dir := t.TempDir()
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for name, data := range extra {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	src := fmt.Sprintf(`// one generated probe over the emitted announcement surface (docs/SPEC-TABLES.md §3.3).
import { readFileSync } from "node:fs";
import { TableVocabulary, Announce, AnnounceMeasure, TableMessageAnnouncement, AnnounceRead } from "./%s";
let failures = 0;
function fail(why) { console.error("FAIL: " + why); failures++; }
if (AnnounceMeasure() !== TableMessageAnnouncement.length) { fail("AnnounceMeasure is not the constant array's length"); }
%s
if (failures !== 0) { process.exit(1); }
console.log("ok");
`, home, probe)
	if err := os.WriteFile(filepath.Join(dir, "probe.mjs"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, filepath.Join(dir, "probe.mjs"))
	cmd.Dir = dir
	return cmd.CombinedOutput()
}

// manifestAnnouncements reads the tables manifest's `connection` lines and
// returns each unit's announcement file bytes, keyed by unit (docs/
// SPEC-TABLES.md §3.3; testdata/conformance/tables/MANIFEST.txt).
func manifestAnnouncements(t *testing.T) map[string][]byte {
	t.Helper()
	data, err := os.ReadFile("../../../testdata/conformance/tables/MANIFEST.txt")
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	out := map[string][]byte{}
	for line := range strings.SplitSeq(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) != 5 || f[0] != "connection" {
			continue
		}
		wire, err := os.ReadFile("../../../" + f[4])
		if err != nil {
			t.Fatalf("read announcement %s: %v", f[4], err)
		}
		out[f[2]] = wire
	}
	return out
}

// homeModule returns the unit's runtime home module — the one that carries the
// shared runtime (docs/SPEC-TABLES.md §19.2), found by the announcement surface
// it now declares.
func homeModule(t *testing.T, u *ir.Unit) string {
	t.Helper()
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	for _, b := range files {
		if strings.Contains(string(b), "TableMessageAnnouncement = new Uint8Array(") {
			return string(b)
		}
	}
	t.Fatal("the generated unit carries no announcement surface")
	return ""
}

var announceArrayRe = regexp.MustCompile(`TableMessageAnnouncement = new Uint8Array\(\[([^\]]*)\]\)`)

func emittedAnnouncement(t *testing.T, src string) []byte {
	t.Helper()
	m := announceArrayRe.FindStringSubmatch(src)
	if m == nil {
		t.Fatal("the generated module has no TableMessageAnnouncement array")
	}
	var out []byte
	for f := range strings.SplitSeq(m[1], ",") {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		v, err := strconv.ParseUint(strings.TrimPrefix(f, "0x"), 16, 8)
		if err != nil {
			t.Fatalf("bad byte literal %q: %v", f, err)
		}
		out = append(out, byte(v))
	}
	return out
}

// ---- the framed announcements the strict-check clause drives, on the same
// framing the Go leg's refusals test uses (docs/SPEC-TABLES.md §3.3): form
// byte 1, the fields, the zero terminator, and the two reserved trailer ids.

func lebs(v uint64) []byte {
	var out []byte
	for {
		c := byte(v & 0x7f)
		v >>= 7
		if v != 0 {
			c |= 0x80
		}
		out = append(out, c)
		if v == 0 {
			return out
		}
	}
}

func versionField(version uint64) []byte {
	return binary.LittleEndian.AppendUint64([]byte{1, 9}, version)
}

func vocabField(elem uint8, vocab []byte) []byte {
	sub := append([]byte{elem}, lebs(uint64(len(vocab)))...)
	sub = append(sub, vocab...)
	field := []byte{2, 14}
	field = append(field, lebs(uint64(len(sub)))...)
	return append(field, sub...)
}

func vocabFieldBadKind(kind uint8) []byte {
	sub := append([]byte{6}, lebs(0)...)
	field := []byte{2, kind}
	field = append(field, lebs(uint64(len(sub)))...)
	return append(field, sub...)
}

func announcement(fields []byte) []byte {
	out := append([]byte{1}, fields...)
	out = append(out, 0x00)
	out = binary.LittleEndian.AppendUint64(out, 0xfffffffffffffffe)
	out = binary.LittleEndian.AppendUint64(out, 0xfffffffffffffffd)
	return binary.LittleEndian.AppendUint64(out, 2)
}
