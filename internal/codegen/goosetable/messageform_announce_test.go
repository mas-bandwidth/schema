package goosetable

// ANNOUNCEREAD ON THE GOOSE LEG (docs/SPEC-TABLES.md §3.3), held to the Go
// reference (tablewire.Vocabulary.AnnounceRead) verdict for verdict.
//
// The battery is every connection golden of the conformance tree, each
// whole, cut short at every length, and with every single byte inverted, plus
// the form bytes a file reader refuses: thousands of announcements, run in
// ONE Goose program (the runtime is the unit's own, so any unit carries it).
// For each, both legs answer whether a vocabulary was set, the refusal by
// name, the damage flag, the unknown fields of the announcement's own body,
// the slots set, the reference width and the build version kept. Then the
// connection's rules on the one value: a second announcement is refused
// `second_announcement` and changes nothing, as is any after a refused first,
// and an entry count past the receiver's declared bound is
// `vocabulary_too_large`.

import (
	"encoding/binary"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose/gooserun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// goAnnounceVerdict is the Go reference's answer, in the probe's line shape.
func goAnnounceVerdict(data []byte, maxEntries int) string {
	v := tablewire.Vocabulary{MaxEntries: maxEntries}
	var rep tabletext.Report
	err := v.AnnounceRead(data, &rep)
	reason := "ok"
	if err != nil {
		var refusal *tablewire.MessageRefusal
		if !errors.As(err, &refusal) {
			return "error " + err.Error()
		}
		reason = refusal.Reason.String()
	}
	ok, slots, bits := 0, 0, 1
	if v.Announced() {
		ok, slots, bits = 1, len(v.Entries()), v.RefBits()
	}
	mal := 0
	if rep.Malformed {
		mal = 1
	}
	return fmt.Sprintf("%d %s %d %d %d %d %016x", ok, reason, mal, rep.Unknown, slots, bits, v.BuildVersion())
}

// gooseAnnounceProbe runs announce_read over every case in one program.
func gooseAnnounceProbe(t *testing.T, u *ir.Unit, cases [][]byte, maxEntries int) []string {
	t.Helper()
	files, err := goose.Generate(u)
	if err != nil {
		t.Fatalf("goose type surface: %v", err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatalf("goose table surface: %v", err)
	}
	maps.Copy(files, tables)
	var mods []string
	for name := range files {
		if before, ok := strings.CutSuffix(name, ".goose"); ok {
			mods = append(mods, before)
		}
	}
	sort.Strings(mods)
	var b strings.Builder
	b.WriteString("import os;\n")
	for _, m := range mods {
		fmt.Fprintf(&b, "import .%s;\n", m)
	}
	b.WriteString(`
fn hex16(v: u64) -> u8[16] {
    let digits = "0123456789abcdef";
    var out: u8[16] = [0; 16];
    for i in 16 {
        out[i] = digits[((v >> ((60 - 4 * i) as! u64)) & 15) as! i64];
    }
    return out;
}

fn main() {
    var all: u8[>..] = [];
    if !read_file("cases.bin", all) { print("read-failed"); return; }
    var at: i64 = 0;
    while at + 4 <= all.len {
        let n = schema_rt::le_at(all, at, 4) as! i64;
        at += 4;
        let data = all[at..at + n];
        at += n;
        var voc = schema_rt::MsgVocabulary { .. };
`)
	fmt.Fprintf(&b, "        voc.max_entries = %d;\n", maxEntries)
	b.WriteString(`        var rep = schema_rt::MsgReport { .. };
        let ok = schema_rt::announce_read(voc, data, rep);
        let h = hex16(voc.build_version);
        print((if ok && voc.announced { 1 } else { 0 }), " ", schema_rt::msg_reason_name(rep.reason), " ", (if rep.malformed { 1 } else { 0 }), " ", rep.unknown, " ", voc.count, " ", voc.ref_bits, " ", h);
    }
}
`)
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "main.goose"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	var all []byte
	for _, c := range cases {
		all = binary.LittleEndian.AppendUint32(all, uint32(len(c)))
		all = append(all, c...)
	}
	out := strings.TrimRight(string(gooserun.Run(t, dir, "main.goose", map[string][]byte{"cases.bin": all})), "\n")
	return strings.Split(out, "\n")
}

func TestGooseMessageFormAnnounceRead(t *testing.T) {
	t.Parallel()
	conns, err := filepath.Glob(filepath.Join(fixedRoot, "testdata", "wire", "tables", "*_conn.bin"))
	if err != nil || len(conns) == 0 {
		t.Fatalf("no connection goldens: %v", err)
	}
	var cases [][]byte
	var names []string
	add := func(name string, data []byte) {
		cases = append(cases, append([]byte(nil), data...))
		names = append(names, name)
	}
	for _, path := range conns {
		golden, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		base := filepath.Base(path)
		add(base, golden)
		for n := range golden {
			add(fmt.Sprintf("%s cut %d", base, n), golden[:n])
		}
		for i := range golden {
			flip := append([]byte(nil), golden...)
			flip[i] ^= 0xFF
			add(fmt.Sprintf("%s flip %d", base, i), flip)
		}
		for _, form := range []byte{0, 2, 3, 0xFF} {
			re := append([]byte(nil), golden...)
			re[0] = form
			add(fmt.Sprintf("%s form %d", base, form), re)
		}
	}
	u := gooseMsgUnitDir(t, "tables/backend")
	for _, bound := range []int{4096, 3} {
		got := gooseAnnounceProbe(t, u, cases, bound)
		if len(got) != len(cases) {
			t.Fatalf("bound %d: the probe answered %d of %d announcements:\n%s", bound, len(got), len(cases), strings.Join(got, "\n"))
		}
		moved := map[string]bool{}
		for i, c := range cases {
			want := goAnnounceVerdict(c, bound)
			if got[i] != want {
				t.Fatalf("bound %d, %s: goose answered %q, the Go reference %q", bound, names[i], got[i], want)
			}
			moved[strings.Fields(want)[1]+"/"+strings.Fields(want)[2]] = true
		}
		// the battery reaches the verdicts it is about
		reached := []string{"ok/0", "ok/1", "message_form_as_file/0", "newer_form/0"}
		if bound == 3 {
			reached = []string{"vocabulary_too_large/0", "ok/1", "message_form_as_file/0", "newer_form/0"}
		}
		for _, verdict := range reached {
			if !moved[verdict] {
				t.Fatalf("bound %d: no announcement reached %s", bound, verdict)
			}
		}
	}
}

// TestGooseMessageFormAnnounceOnce holds the connection's rule: the first
// announcement is the only one that sets the vocabulary; a second is refused
// `second_announcement` and changes nothing, and so is any after a refused
// first (§3.3).
func TestGooseMessageFormAnnounceOnce(t *testing.T) {
	t.Parallel()
	u := gooseMsgUnitDir(t, "tables/backend")
	files, err := goose.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	var b strings.Builder
	b.WriteString("import os;\n")
	var mods []string
	for name := range files {
		if before, ok := strings.CutSuffix(name, ".goose"); ok {
			mods = append(mods, before)
		}
	}
	sort.Strings(mods)
	for _, m := range mods {
		fmt.Fprintf(&b, "import .%s;\n", m)
	}
	ns := goose.Namespace(u)
	fmt.Fprintf(&b, `
fn main() {
    let own = %s::LoginRequestAnnounce;
    var voc = schema_rt::MsgVocabulary { .. };
    var rep = schema_rt::MsgReport { .. };
    let first = schema_rt::announce_read(voc, own, rep);
    let slots = voc.count;
    let second = schema_rt::announce_read(voc, own, rep);
    print(if first { 1 } else { 0 }, " ", if second { 1 } else { 0 }, " ", schema_rt::msg_reason_name(rep.reason), " ", (if voc.announced { 1 } else { 0 }), " ", voc.count - slots);
    var bad = schema_rt::MsgVocabulary { .. };
    let refused = schema_rt::announce_read(bad, own[0..3], rep);
    let after = schema_rt::announce_read(bad, own, rep);
    print(if refused { 1 } else { 0 }, " ", if after { 1 } else { 0 }, " ", schema_rt::msg_reason_name(rep.reason), " ", (if bad.announced { 1 } else { 0 }), " ", bad.count);
}
`, ns)
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "main.goose"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	got := strings.TrimSpace(string(gooserun.Run(t, dir, "main.goose", nil)))
	want := "1 0 second_announcement 1 0\n0 0 second_announcement 0 0"
	if got != want {
		t.Fatalf("the connection's announcement rule:\n got  %s\n want %s", got, want)
	}
}

// TestGooseMessageFormHostileBatches crosses the peer read's DAMAGE and SKIP
// paths with the Go reference: every fixed-root `message` golden cut short at
// every length and with every single byte inverted, each read by both legs
// under the connection's announcement, which must agree on the bodies
// delivered, the refusal by name, the damage flag and §4's counters.
func TestGooseMessageFormHostileBatches(t *testing.T) {
	t.Parallel()
	for _, row := range gooseMsgRows(t) {
		u := gooseMsgUnitDir(t, row.unitDir)
		if !isFixedRoot(u, row.root) {
			continue
		}
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			announce := gooseMsgFile(t, row.conn)
			golden := gooseMsgFile(t, row.message)
			var cases [][]byte
			var names []string
			for n := range golden {
				cases = append(cases, append([]byte(nil), golden[:n]...))
				names = append(names, fmt.Sprintf("cut %d", n))
			}
			for i := range golden {
				flip := append([]byte(nil), golden...)
				flip[i] ^= 0xFF
				cases = append(cases, flip)
				names = append(names, fmt.Sprintf("flip %d", i))
			}
			got := gooseBatchProbe(t, u, row.root, announce, cases)
			if len(got) != len(cases) {
				t.Fatalf("the probe answered %d of %d batches:\n%s", len(got), len(cases), strings.Join(got, "\n"))
			}
			for i, c := range cases {
				if want := goBatchVerdict(t, u, row.root, announce, c); got[i] != want {
					t.Fatalf("%s: goose answered %q, the Go reference %q", names[i], got[i], want)
				}
			}
		})
	}
}

// goBatchVerdict is the Go reference's read of one batch, in the probe's
// shape: `<bodies> <reason> <malformed>` and, on a read that refused nothing,
// `<unknown> <kind_mismatch> <widened> <clamped>`.
func goBatchVerdict(t *testing.T, u *ir.Unit, root string, announce, batch []byte) string {
	t.Helper()
	var vocab tablewire.Vocabulary
	var arep tabletext.Report
	if err := vocab.AnnounceRead(announce, &arep); err != nil {
		t.Fatal(err)
	}
	m := tabletext.NewModel(u)
	st := fixedRootStruct(t, u, root)
	insts := make([]*tabletext.Instance, 256)
	for i := range insts {
		insts[i] = m.New(st)
	}
	var rep tabletext.Report
	n, _, err := tablewire.DecodeMessages(m, insts, batch, &vocab, &rep)
	if err != nil {
		if refusal, ok := errors.AsType[*tablewire.MessageRefusal](err); ok {
			return "0 " + refusal.Reason.String() + " 0"
		}
		if _, ok := errors.AsType[*tablewire.NonFiniteRefusal](err); ok {
			return "0 non_finite_float 0"
		}
		return "error " + err.Error()
	}
	mal := 0
	if rep.Malformed {
		mal = 1
	}
	return fmt.Sprintf("%d ok %d %d %d %d %d", n, mal, rep.Unknown, rep.KindMismatch, rep.Widened, rep.Clamped)
}

// gooseBatchProbe reads every batch with <root>LoadMessages under the
// announcement, in one program.
func gooseBatchProbe(t *testing.T, u *ir.Unit, root string, announce []byte, cases [][]byte) []string {
	t.Helper()
	files, err := goose.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	var mods []string
	for name := range files {
		if before, ok := strings.CutSuffix(name, ".goose"); ok {
			mods = append(mods, before)
		}
	}
	sort.Strings(mods)
	ns := goose.Namespace(u)
	var b strings.Builder
	b.WriteString("import os;\n")
	for _, m := range mods {
		fmt.Fprintf(&b, "import .%s;\n", m)
	}
	fmt.Fprintf(&b, `
fn main() {
    var peer: u8[>..] = [];
    if !read_file("announce.bin", peer) { print("read-failed"); return; }
    var voc = schema_rt::MsgVocabulary { .. };
    var vrep = schema_rt::MsgReport { .. };
    if !schema_rt::announce_read(voc, peer, vrep) { print("announce-refused"); return; }
    var all: u8[>..] = [];
    if !read_file("cases.bin", all) { print("read-failed"); return; }
    var values: %[1]s::%[2]s[256] = [%[1]s::%[2]s { .. }; 256];
    var at: i64 = 0;
    while at + 4 <= all.len {
        let n = schema_rt::le_at(all, at, 4) as! i64;
        at += 4;
        let data = all[at..at + n];
        at += n;
        var rep = schema_rt::MsgReport { .. };
        var got = %[1]s::%[2]sLoadMessages(values[0..256], voc, data, rep);
        if got < 0 { got = 0; }
        if rep.reason != 0 {
            print(got, " ", schema_rt::msg_reason_name(rep.reason), " 0");
        } else {
            print(got, " ok ", (if rep.malformed { 1 } else { 0 }), " ", rep.unknown, " ", rep.kind_mismatch, " ", rep.widened, " ", rep.clamped);
        }
    }
}
`, ns, root)
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "main.goose"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	var all []byte
	for _, c := range cases {
		all = binary.LittleEndian.AppendUint32(all, uint32(len(c)))
		all = append(all, c...)
	}
	out := strings.TrimRight(string(gooserun.Run(t, dir, "main.goose", map[string][]byte{"cases.bin": all, "announce.bin": announce})), "\n")
	return strings.Split(out, "\n")
}
