package javatable

// messageform_batch — the Java leg's MESSAGE FORM batch surface
// (docs/SPEC-TABLES.md §3.3, form byte 2):
//
//   1. the manifest's `message` rows (backenddemo, vocabdemo, vocab9demo):
//      announceRead the connection's announcement, loadMessages the reference
//      batch, measureMessages equals its length, saveMessages reproduces it
//      byte for byte;
//   2. every fixed-form construct against the Go codec (internal/tablewire),
//      including `tables/messages` (`unit messagedemo`);
//   3. an older card read and upgraded by a newer reader, reporting the
//      oracle's exact unknown, kindMismatch, widened and clamped counts;
//   4. the batch's refusals (`no_vocabulary`, `batch_too_large` on measure,
//      save and read — closing `java/F9` — and `newer_form`) and its damage
//      rules (damage in body two delivers body one, bytes after pad, short
//      buffer, unknown field skipped, quantized index at/above count).

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	javacodegen "github.com/mas-bandwidth/schema/v2/internal/codegen/java"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestMessageFormBatchJava(t *testing.T) {
	t.Parallel()
	javaTools(t)
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}

	// 1. THE MANIFEST's `message` ROWS: announceRead the connection's
	// announcement, loadMessages the reference's batch against it,
	// measureMessages equals its length and saveMessages reproduces it byte
	// for byte (§3.3; test/conformance/README.md's `message` surface).
	t.Run("manifest_rows", func(t *testing.T) {
		t.Parallel()
		rows := messageBatchManifest(t, root)
		// "`fixed table T` may ride here, a plain `table T` may not" (§3.3):
		// a row whose root is a plain table (graph_tree's Scene) carries no
		// verbs, and its absence is the conformance driver's `.absent`, never a
		// pass here.
		byUnit := map[string][]messageBatchRow{}
		units := map[string]*ir.Unit{}
		var names []string
		var plain []string
		for _, row := range rows {
			u, ok := units[row.unit]
			if !ok {
				u = messageBatchLoadPaths(t, root, row.paths)
				units[row.unit] = u
			}
			if ir.VariableTables(u)[row.root] {
				plain = append(plain, row.name)
				continue
			}
			if _, ok := byUnit[row.unit]; !ok {
				names = append(names, row.unit)
			}
			byUnit[row.unit] = append(byUnit[row.unit], row)
		}
		sort.Strings(names)
		if len(names) == 0 {
			t.Fatal("no manifest message row has a fixed root")
		}
		t.Logf("plain-table message rows, absent by §3.3: %v", plain)
		for _, unit := range names {
			t.Run(unit, func(t *testing.T) {
				t.Parallel()
				carried := byUnit[unit]
				u := units[unit]
				var body strings.Builder
				for _, row := range carried {
					body.WriteString(messageBatchRoundTrip(row.name, row.root, row.announce, row.message))
				}
				out := runMessageBatchProbe(t, u, body.String())
				for _, row := range carried {
					if line := messageBatchLine(out, row.name); line != "BATCH "+row.name+" ok" {
						t.Errorf("%s/%s: the Java batch does not reproduce the reference bytes: %s", unit, row.name, line)
					}
				}
			})
		}
	})

	// 2. THE CONSTRUCTS NO MANIFEST ROW REACHES, against the Go codec:
	// `tables/messages` (`unit messagedemo`) and every scalar kind and packing
	// the fixed form carries, text, bytes, fixed and counted
	// arrays, a nested type, an enum array, and a union with a table arm, a
	// scalar arm, an enum arm and a payload-free arm.
	t.Run("oracle_constructs", func(t *testing.T) {
		t.Parallel()

		t.Run("messagedemo", func(t *testing.T) {
			t.Parallel()
			u := loadUnitDir(t, root, "tables/messages")
			m := tabletext.NewModel(u)
			instanceNames := []string{
				"message_open", "message_transaction", "message_ping",
				"message_save", "message_empty", "message_batch",
				"message_trace", "message_arms", "message_arm_edits",
				"message_ack",
			}
			var insts []*tabletext.Instance
			var body strings.Builder
			var names []string
			announce := ir.TableAnnouncement(u)
			for _, name := range instanceNames {
				raw, err := os.ReadFile(filepath.Join(root, "testdata", "conformance", "tables", "json", name+".json"))
				if err != nil {
					t.Fatal(err)
				}
				inst := m.New(m.Lookup("ToolMessage"))
				var rep tabletext.Report
				if !m.Read(inst, raw, &rep) || rep != (tabletext.Report{}) {
					t.Fatalf("%s: the oracle did not read the JSON cleanly: %+v", name, rep)
				}
				insts = append(insts, inst)
				wire, err := tablewire.EncodeMessages(m, []*tabletext.Instance{inst})
				if err != nil {
					t.Fatalf("%s: the oracle did not encode: %v", name, err)
				}
				names = append(names, name)
				body.WriteString(messageBatchRoundTrip(name, "ToolMessage", announce, wire))
			}
			wire, err := tablewire.EncodeMessages(m, insts)
			if err != nil {
				t.Fatalf("messagedemo batch: the oracle did not encode: %v", err)
			}
			names = append(names, "messagedemo_all")
			body.WriteString(messageBatchRoundTrip("messagedemo_all", "ToolMessage", announce, wire))
			out := runMessageBatchProbe(t, u, body.String())
			for _, name := range names {
				if line := messageBatchLine(out, name); line != "BATCH "+name+" ok" {
					t.Errorf("%s: the Java batch does not reproduce the Go codec's bytes: %s", name, line)
				}
			}
		})

		t.Run("probe", func(t *testing.T) {
			t.Parallel()
			u := unitFrom(t, messageBatchProbeSrc)
			m := tabletext.NewModel(u)
			var insts []*tabletext.Instance
			for i, text := range messageBatchProbeTexts {
				inst := m.New(m.Lookup("Probe"))
				var rep tabletext.Report
				if !m.Read(inst, []byte(text), &rep) || rep != (tabletext.Report{}) {
					t.Fatalf("text %d: the oracle did not read the probe text cleanly: %+v", i, rep)
				}
				insts = append(insts, inst)
			}
			announce := ir.TableAnnouncement(u)
			var body strings.Builder
			var names []string
			for i, inst := range insts {
				wire, err := tablewire.EncodeMessages(m, []*tabletext.Instance{inst})
				if err != nil {
					t.Fatalf("text %d: the oracle did not encode: %v", i, err)
				}
				name := fmt.Sprintf("probe_%d", i)
				names = append(names, name)
				body.WriteString(messageBatchRoundTrip(name, "Probe", announce, wire))
			}
			wire, err := tablewire.EncodeMessages(m, insts)
			if err != nil {
				t.Fatalf("the oracle did not encode the batch: %v", err)
			}
			names = append(names, "probe_batch")
			body.WriteString(messageBatchRoundTrip("probe_batch", "Probe", announce, wire))
			out := runMessageBatchProbe(t, u, body.String())
			for _, name := range names {
				if line := messageBatchLine(out, name); line != "BATCH "+name+" ok" {
					t.Errorf("%s: the Java batch does not reproduce the Go codec's bytes: %s", name, line)
				}
			}
		})
	})

	// 3. VERSIONING, the use case: an OLDER writer's batch read under ITS
	// announcement by a NEWER reader (§3.3: "a reader decodes a body against
	// the table THAT endpoint announced").
	t.Run("versioning_upgrade", func(t *testing.T) {
		t.Parallel()
		older := unitFrom(t, messageBatchOlderSrc)
		newer := unitFrom(t, messageBatchNewerSrc)
		mo := tabletext.NewModel(older)
		mn := tabletext.NewModel(newer)
		var olds []*tabletext.Instance
		for i, text := range messageBatchOlderTexts {
			inst := mo.New(mo.Lookup("Card"))
			var rep tabletext.Report
			if !mo.Read(inst, []byte(text), &rep) || rep != (tabletext.Report{}) {
				t.Fatalf("older text %d: the oracle did not read it cleanly: %+v", i, rep)
			}
			olds = append(olds, inst)
		}
		wire, err := tablewire.EncodeMessages(mo, olds)
		if err != nil {
			t.Fatalf("the oracle did not encode the older batch: %v", err)
		}
		announce := ir.TableAnnouncement(older)
		v := &tablewire.Vocabulary{}
		var arep tabletext.Report
		if err := v.AnnounceRead(announce, &arep); err != nil {
			t.Fatalf("the oracle refused the older announcement: %v", err)
		}
		news := make([]*tabletext.Instance, len(olds))
		for i := range news {
			news[i] = mn.New(mn.Lookup("Card"))
		}
		var rep tabletext.Report
		count, ok, err := tablewire.DecodeMessages(mn, news, wire, v, &rep)
		if err != nil || !ok || count != len(olds) {
			t.Fatalf("the oracle did not decode the older batch: count=%d ok=%v err=%v", count, ok, err)
		}
		if rep.Unknown == 0 || rep.Widened == 0 || rep.Clamped == 0 || rep.KindMismatch == 0 {
			t.Fatalf("the versioning case no longer exercises every counter: %+v", rep)
		}
		upgraded, err := tablewire.EncodeMessages(mn, news)
		if err != nil {
			t.Fatalf("the oracle did not encode the upgraded batch: %v", err)
		}
		main := fmt.Sprintf(`
        {
            final byte[] ann = fromHex(%q);
            final byte[] old = fromHex(%q);
            final byte[] want = fromHex(%q);
            final TableVocabulary vocab = new TableVocabulary();
            vocab.reset(4096);
            final TableVocabulary.Report arep = new TableVocabulary.Report();
            final boolean announced = TableVocabulary.announceRead(vocab, ann, arep);
            final CardFixed.Value[] values = new CardFixed.Value[%d];
            for (int i = 0; i < values.length; i++) { values[i] = new CardFixed.Value(); }
            final TableVocabulary.Report lrep = new TableVocabulary.Report();
            final int count = CardMessage.loadMessages(values, values.length, vocab, old, lrep);
            final boolean ok = !lrep.malformed && !lrep.refused;
            final boolean countersOk = lrep.unknown == %d && lrep.kindMismatch == %d && lrep.widened == %d && lrep.clamped == %d;
            final TableVocabulary.Report srep = new TableVocabulary.Report();
            final byte[] buf = new byte[want.length];
            java.util.Arrays.fill(buf, (byte) 0xa5);
            final int wrote = CardMessage.saveMessages(values, count, buf, srep);
            final TableVocabulary.Report mrep = new TableVocabulary.Report();
            final int measured = CardMessage.measureMessages(values, count, mrep);
            final boolean same = java.util.Arrays.equals(buf, want);
            say("upgrade", announced && ok && count == %d && countersOk && wrote == want.length && measured == want.length && same,
                "announced=" + announced + " ok=" + ok + " count=" + count
                + " counters=" + lrep.unknown + "," + lrep.kindMismatch + "," + lrep.widened + "," + lrep.clamped
                + " wrote=" + wrote + " measured=" + measured + " same=" + same);
        }
`, hexOf(announce), hexOf(wire), hexOf(upgraded),
			len(olds), rep.Unknown, rep.KindMismatch, rep.Widened, rep.Clamped, len(olds))
		out := runMessageBatchProbe(t, newer, main)
		if line := messageBatchLine(out, "upgrade"); line != "BATCH upgrade ok" {
			t.Fatalf("the newer Java reader does not upgrade the older batch as the oracle does: %s", line)
		}
	})

	// 4. THE BATCH's REFUSALS AND ITS DAMAGE RULES (§3.3), each by name.
	t.Run("refusals_and_damage", func(t *testing.T) {
		t.Parallel()
		u := unitFrom(t, messageBatchSmallSrc)
		out := runMessageBatchProbe(t, u, messageBatchRefusalsMain)
		for _, id := range []string{
			"no_vocabulary", "batch_too_large_measure", "batch_too_large_save",
			"batch_too_large_read", "newer_form", "damage_in_body_two",
			"bytes_after_pad", "short_buffer", "unknown_field_skipped",
			"quantized_index_at_count", "quantized_index_above_count",
		} {
			if line := messageBatchLine(out, id); line != "BATCH "+id+" ok" {
				t.Errorf("%s: %s", id, line)
			}
		}
	})
}

type messageBatchRow struct {
	name, root, unit  string
	paths             []string
	announce, message []byte
}

func messageBatchManifest(t *testing.T, root string) []messageBatchRow {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for line := range strings.SplitSeq(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			lines = append(lines, line)
		}
	}
	units := map[string][]string{}
	connUnit := map[string]string{}
	connWire := map[string]string{}
	for _, line := range lines {
		f := strings.Fields(line)
		switch {
		case len(f) >= 3 && f[0] == "unit":
			units[f[1]] = f[2:]
		case len(f) >= 5 && f[0] == "connection":
			connUnit[f[1]] = f[2]
			connWire[f[1]] = f[4]
		}
	}
	var out []messageBatchRow
	for _, line := range lines {
		f := strings.Fields(line)
		if len(f) != 6 || f[0] != "message" {
			continue
		}
		unit, ok := connUnit[f[2]]
		if !ok {
			t.Fatalf("message %s names no connection %s", f[1], f[2])
		}
		paths, ok := units[unit]
		if !ok {
			t.Fatalf("connection %s names no unit %s", f[2], unit)
		}
		ann, err := os.ReadFile(filepath.Join(root, connWire[f[2]]))
		if err != nil {
			t.Fatal(err)
		}
		msg, err := os.ReadFile(filepath.Join(root, f[5]))
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, messageBatchRow{name: f[1], root: f[3], unit: unit, paths: paths, announce: ann, message: msg})
	}
	if len(out) == 0 {
		t.Fatal("the manifest carries no message row")
	}
	return out
}

func messageBatchLoadPaths(t *testing.T, root string, paths []string) *ir.Unit {
	t.Helper()
	var files []check.SourceFile
	for _, rel := range paths {
		abs := filepath.Join(root, rel)
		info, err := os.Stat(abs)
		if err != nil {
			t.Fatal(err)
		}
		if info.IsDir() {
			entries, err := os.ReadDir(abs)
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range entries {
				if e.IsDir() || !strings.HasSuffix(e.Name(), ".schema") || len(e.Name()) <= len(".schema") {
					continue
				}
				p := filepath.Join(abs, e.Name())
				data, err := os.ReadFile(p)
				if err != nil {
					t.Fatal(err)
				}
				f, perrs := parser.Parse(p, data)
				if len(perrs) > 0 {
					t.Fatalf("parse %s: %v", p, perrs[0])
				}
				files = append(files, check.SourceFile{Path: p, Name: e.Name(), Base: strings.TrimSuffix(e.Name(), ".schema"), Bytes: data, AST: f})
			}
		} else {
			data, err := os.ReadFile(abs)
			if err != nil {
				t.Fatal(err)
			}
			base := filepath.Base(abs)
			f, perrs := parser.Parse(abs, data)
			if len(perrs) > 0 {
				t.Fatalf("parse %s: %v", abs, perrs[0])
			}
			files = append(files, check.SourceFile{Path: abs, Name: base, Base: strings.TrimSuffix(base, ".schema"), Bytes: data, AST: f})
		}
	}
	u, cerrs := check.Unit(files)
	if len(cerrs) > 0 {
		t.Fatalf("check %v: %v", paths, cerrs[0])
	}
	return u
}

func runMessageBatchProbe(t *testing.T, u *ir.Unit, body string) string {
	t.Helper()
	javac, javaBin := javaTools(t)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	pkt, err := javacodegen.Generate(u)
	if err != nil {
		t.Fatalf("packet generate: %v", err)
	}
	dir := t.TempDir()
	srcDir := filepath.Join(dir, "src", u.Package)
	classes := filepath.Join(dir, "classes")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	var args []string
	args = append(args, "--release", "17", "-nowarn", "-d", classes)
	var names []string
	for name := range files {
		keep := name == "TableFixed.java" ||
			name == "TableVocabulary.java" ||
			name == "TableMessage.java" ||
			strings.HasSuffix(name, "Fixed.java") ||
			strings.HasSuffix(name, "Message.java")
		if keep {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		p := filepath.Join(srcDir, name)
		if err := os.WriteFile(p, files[name], 0o644); err != nil {
			t.Fatal(err)
		}
		args = append(args, p)
	}
	for _, name := range []string{"Int128.java", "UInt128.java"} {
		if data, ok := pkt[name]; ok {
			p := filepath.Join(srcDir, name)
			if err := os.WriteFile(p, data, 0o644); err != nil {
				t.Fatal(err)
			}
			args = append(args, p)
		}
	}
	driver := "package " + u.Package + ";\n\n" + driverPrologue + `
    static void say(String id, boolean ok, String why) {
        if (ok) {
            System.out.println("BATCH " + id + " ok");
        } else {
            System.out.println("BATCH " + id + " BAD " + why);
        }
    }

    public static void main(String[] args) {
` + body + "    }\n}\n"
	driverPath := filepath.Join(srcDir, "Driver.java")
	if err := os.WriteFile(driverPath, []byte(driver), 0o644); err != nil {
		t.Fatal(err)
	}
	args = append(args, driverPath)
	cmd := exec.Command(javac, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
	run := exec.Command(javaBin, "-ea", "-cp", classes, u.Package+".Driver")
	out, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("java: %v\n%s", err, out)
	}
	return string(out)
}

func messageBatchLine(out, id string) string {
	prefix := "BATCH " + id + " "
	for l := range strings.SplitSeq(out, "\n") {
		if strings.HasPrefix(l, prefix) {
			return l
		}
	}
	return fmt.Sprintf("[no BATCH line for %s in the probe's output: %s]", id, strings.TrimSpace(out))
}

func messageBatchRoundTrip(name, root string, announce, message []byte) string {
	cls := ir.GoExportName(root)
	bodies := int(message[1]) + 1
	return fmt.Sprintf(`
        {
            final byte[] ann = fromHex(%q);
            final byte[] msg = fromHex(%q);
            final TableVocabulary vocab = new TableVocabulary();
            vocab.reset(4096);
            final TableVocabulary.Report arep = new TableVocabulary.Report();
            final boolean announced = TableVocabulary.announceRead(vocab, ann, arep);
            final %sFixed.Value[] values = new %sFixed.Value[TableMessage.batchMax];
            for (int i = 0; i < values.length; i++) { values[i] = new %sFixed.Value(); }
            final TableVocabulary.Report lrep = new TableVocabulary.Report();
            final int count = %sMessage.loadMessages(values, values.length, vocab, msg, lrep);
            final boolean ok = !lrep.malformed && !lrep.refused;
            final boolean clean = !lrep.malformed && !lrep.refused
                && lrep.unknown == 0 && lrep.kindMismatch == 0 && lrep.widened == 0 && lrep.clamped == 0;
            final TableVocabulary.Report mrep = new TableVocabulary.Report();
            final int measured = %sMessage.measureMessages(values, count, mrep);
            final byte[] buf = new byte[msg.length];
            java.util.Arrays.fill(buf, (byte) 0xa5);
            final TableVocabulary.Report srep = new TableVocabulary.Report();
            final int wrote = %sMessage.saveMessages(values, count, buf, srep);
            final boolean same = java.util.Arrays.equals(buf, msg);
            say(%q, announced && ok && clean && count == %d && measured == msg.length && wrote == msg.length && same,
                "announced=" + announced + " ok=" + ok + " count=" + count + " measured=" + measured + " wrote=" + wrote + " same=" + same
                + " got=" + hex(buf) + " want=" + hex(msg));
        }
`, hexOf(announce), hexOf(message), cls, cls, cls, cls, cls, cls, name, bodies)
}

const messageBatchProbeSrc = `package batchprobe

enum Mood { Calm, Angry, Sleepy }

flags Caps { Jump, Crouch, Fly }

type Spot
{
    x int16
    y uint8 = 3
}

fixed table Leaf
{
    id   uint32
    name string(8)
}

union Pick
{
    leaf  Leaf
    qty   uint16 | min = 0, max = 500
    mood  Mood
    ping
}

fixed table Probe
{
    i8v   int8
    i16v  int16 = -3
    i32v  int32
    i64v  int64
    u8v   uint8
    u16v  uint16
    u32v  uint32 = 7
    u64v  uint64
    r32   int32 | min = -1000, max = 1000
    ru64  uint64 = 5 | min = 5, max = 900000
    i128v int128 | min = -5000000000, max = 5000000000
    u128v uint128
    b3    bits(3)
    caps  Caps
    flag  bool
    f32v  float32
    cf    float32 | min = -1, max = 1, resolution = 0.01
    f64v  float64
    mood  Mood = Angry
    moods [3]Mood
    label string(12)
    blob  bytes(6)
    small [4]int16
    many  [..5]uint32
    spot  Spot
    spots [..3]Spot
    pair  [2]Spot
    pick  Pick
    picks [2]Pick
}
`

var messageBatchProbeTexts = []string{
	`{}`,
	`{
  "i8v": -128, "i16v": 32767, "i32v": -2147483648, "i64v": 9223372036854775807,
  "u8v": 255, "u16v": 65535, "u32v": 4294967295, "u64v": 18446744073709551615,
  "r32": -1000, "ru64": 900000, "i128v": -5000000000, "u128v": 340282366920938463463374607431768211455,
  "b3": 7, "caps": ["Jump", "Fly"], "flag": true, "f32v": 3.25, "cf": 0.37, "f64v": -1e-300,
  "mood": "Sleepy", "moods": ["Calm", "None", "Angry"], "label": "héllo wörl",
  "blob": "AQIDBAUG", "small": [-1, 0, 127, -32768],
  "many": [1, 2, 3, 4, 5], "spot": {"x": -4, "y": 9},
  "spots": [{"x": 1, "y": 2}, {"x": 0, "y": 3}], "pair": [{"x": 5, "y": 3}, {"x": -5, "y": 0}],
  "pick": {"leaf": {"id": 77, "name": "leafy"}},
  "picks": [{"qty": 499}, {"mood": "Calm"}]
}`,
	`{"i16v": -3, "u32v": 7, "mood": "Angry", "label": "hi", "pick": {"ping": null}, "picks": [{"qty": 10}, {}], "spot": {"x": 0, "y": 3}}`,
	`{"mood": "None", "cf": -1, "many": [], "spots": [{"x": 0, "y": 0}], "pick": {"mood": "Sleepy"}, "small": [0, 0, 0, 1]}`,
}

const messageBatchOlderSrc = `package cardver

enum Mood { Calm, Angry, Bored }

fixed table Note
{
    text string(16)
}

union Body
{
    note   Note
    legacy uint32
    mood   Mood
}

fixed table Card
{
    id    uint32
    title string(16)
    score int16
    gone  uint8
    level uint8 | min = 0, max = 10
    rank  uint16
    mood  Mood
    tags  [..4]uint16
    body  Body
}
`

const messageBatchNewerSrc = `package cardver

enum Mood { Calm, Angry, Sleepy }

fixed table Note
{
    text string(16)
}

union Body
{
    note Note
    mood Mood
}

fixed table Card
{
    id    uint32
    title string(8)
    score int32
    level uint8 | min = 0, max = 5
    rank  string(4)
    mood  Mood
    tags  [..2]uint16
    body  Body
    added uint32 = 7
}
`

var messageBatchOlderTexts = []string{
	`{"id": 1, "title": "a card title", "score": -300, "gone": 9, "level": 9, "rank": 4, "mood": "Bored", "tags": [1, 2, 3], "body": {"legacy": 42}}`,
	`{"id": 2, "title": "short", "score": 12, "level": 3, "mood": "Angry", "tags": [7], "body": {"note": {"text": "kept"}}}`,
	`{"id": 3, "body": {"mood": "Bored"}}`,
}

const messageBatchSmallSrc = `package small

fixed table Root
{
    a int32
    b bool
    c uint8
    d float32 | min = -1, max = 1, resolution = 0.01
}
`

const messageBatchRefusalsMain = `
        final byte[] ann = new byte[TableVocabulary.announceMeasure()];
        if (TableVocabulary.announce(ann) != ann.length) { fail("announce"); }
        final TableVocabulary vocab = new TableVocabulary();
        vocab.reset(4096);
        if (!TableVocabulary.announceRead(vocab, ann, new TableVocabulary.Report())) { fail("announceRead"); }

        final RootFixed.Value one = new RootFixed.Value();
        one.a = 42;
        one.b = true;
        one.c = 7;
        final RootFixed.Value[] oneArr = new RootFixed.Value[] { one };
        final int m = RootMessage.measureMessages(oneArr, 1, new TableVocabulary.Report());
        if (m <= 0) { fail("measureMessages: " + m); }
        final byte[] wire = new byte[m];
        if (RootMessage.saveMessages(oneArr, 1, wire, new TableVocabulary.Report()) != m) { fail("saveMessages"); }

        {
            // "A BODY FROM AN ENDPOINT THAT NEVER ANNOUNCED IS REFUSED BY NAME ... no_vocabulary"
            final TableVocabulary fresh = new TableVocabulary();
            fresh.reset(4096);
            final TableVocabulary.Report rep = new TableVocabulary.Report();
            final RootFixed.Value[] values = new RootFixed.Value[] { new RootFixed.Value() };
            final int count = RootMessage.loadMessages(values, values.length, fresh, wire, rep);
            say("no_vocabulary", count == 0 && rep.refused && "no_vocabulary".equals(rep.reason) && !rep.malformed && rep.unknown == 0 && values[0].a == 0,
                "count=" + count + " refused=" + rep.refused + " reason=" + rep.reason);
        }
        {
            // "M ABOVE 256 ON THE WRITE SIDE IS A REFUSAL BY NAME": both verbs refuse and return -1.
            final RootFixed.Value[] huge = new RootFixed.Value[TableMessage.batchMax + 1];
            for (int i = 0; i < huge.length; i++) { huge[i] = new RootFixed.Value(); }
            final TableVocabulary.Report repM = new TableVocabulary.Report();
            final int gotM = RootMessage.measureMessages(huge, huge.length, repM);
            final TableVocabulary.Report repZero = new TableVocabulary.Report();
            final int gotZero = RootMessage.measureMessages(huge, 0, repZero);
            say("batch_too_large_measure",
                gotM == -1 && repM.refused && "batch_too_large".equals(repM.reason) && !repM.malformed
                && gotZero == -1 && repZero.refused && "batch_too_large".equals(repZero.reason),
                "gotM=" + gotM + " reason=" + repM.reason + " gotZero=" + gotZero);
            final TableVocabulary.Report repS = new TableVocabulary.Report();
            final byte[] buf = new byte[4096];
            final int gotS = RootMessage.saveMessages(huge, huge.length, buf, repS);
            say("batch_too_large_save",
                gotS == -1 && repS.refused && "batch_too_large".equals(repS.reason) && !repS.malformed && buf[0] == 0,
                "gotS=" + gotS + " reason=" + repS.reason);
        }
        {
            // "M ABOVE THE CALLER'S CAPACITY ON THE READ SIDE IS A REFUSAL BY NAME
            // ... The reader sets the returned count to the wire's M." (closes java/F9)
            final RootFixed.Value[] three = new RootFixed.Value[] { one, one, one };
            final int m3 = RootMessage.measureMessages(three, 3, new TableVocabulary.Report());
            final byte[] buf3 = new byte[m3];
            if (RootMessage.saveMessages(three, 3, buf3, new TableVocabulary.Report()) != m3) { fail("save 3"); }
            final RootFixed.Value[] values = new RootFixed.Value[] { new RootFixed.Value(), new RootFixed.Value() };
            final TableVocabulary.Report rep = new TableVocabulary.Report();
            final int count = RootMessage.loadMessages(values, values.length, vocab, buf3, rep);
            say("batch_too_large_read",
                count == 3 && rep.refused && "batch_too_large".equals(rep.reason) && !rep.malformed
                && rep.unknown == 0 && rep.kindMismatch == 0 && rep.widened == 0 && rep.clamped == 0
                && values[0].a == 0 && values[1].a == 0,
                "count=" + count + " refused=" + rep.refused + " reason=" + rep.reason);
        }
        {
            // "The FORM BYTE is a whole byte and is read FIRST ... a form a reader
            // does not know is a REFUSAL by name and never damage."
            boolean all = true;
            for (byte form : new byte[] { 1, 3, 6 }) {
                final RootFixed.Value[] values = new RootFixed.Value[] { new RootFixed.Value() };
                final TableVocabulary.Report rep = new TableVocabulary.Report();
                final int count = RootMessage.loadMessages(values, values.length, vocab, new byte[] { form }, rep);
                all = all && count == 0 && rep.refused && "newer_form".equals(rep.reason) && !rep.malformed;
            }
            say("newer_form", all, "a foreign form byte was not refused as newer_form");
        }
        {
            // "DAMAGE INSIDE BODY k DELIVERS BODIES 1 TO k - 1."
            final int bits = vocab.refBits;
            final byte[] forged = new byte[] { 2, 2, 0, 0, 0, 0 };
            final long ones = (1L << bits) - 1L;
            final long v = ones << bits;
            for (int i = 0; i < 4; i++) {
                forged[2 + i] = (byte) (v >>> (8 * i));
            }
            final RootFixed.Value[] values = new RootFixed.Value[] { new RootFixed.Value(), new RootFixed.Value(), new RootFixed.Value() };
            final TableVocabulary.Report rep = new TableVocabulary.Report();
            final int count = RootMessage.loadMessages(values, values.length, vocab, forged, rep);
            say("damage_in_body_two", count == 1 && rep.malformed && !rep.refused,
                "count=" + count + " malformed=" + rep.malformed + " refused=" + rep.refused);
        }
        {
            // "BYTES AFTER THE PAD ARE MALFORMED"
            final byte[] padded = java.util.Arrays.copyOf(wire, wire.length + 1);
            final RootFixed.Value[] values = new RootFixed.Value[] { new RootFixed.Value() };
            final TableVocabulary.Report rep = new TableVocabulary.Report();
            final int count = RootMessage.loadMessages(values, values.length, vocab, padded, rep);
            say("bytes_after_pad", count == 1 && rep.malformed && !rep.refused,
                "count=" + count + " malformed=" + rep.malformed + " refused=" + rep.refused);
        }
        {
            // A buffer too short for the batch is a -1 and never a partial write.
            final byte[] shortBuf = new byte[m - 1];
            final int got = RootMessage.saveMessages(oneArr, 1, shortBuf, new TableVocabulary.Report());
            say("short_buffer", got == -1, "got=" + got);
        }
        {
            // An unknown field under a slot the endpoint announced is skipped by its announced shape and counted unknown.
            final RootFixed.Value[] values = new RootFixed.Value[] { new RootFixed.Value() };
            final TableVocabulary.Report rep = new TableVocabulary.Report();
            final TableVocabulary v2 = new TableVocabulary();
            v2.reset(4096);
            boolean okAll = TableVocabulary.announceRead(v2, ann, new TableVocabulary.Report());
            final TableVocabulary.Entry unk = new TableVocabulary.Entry();
            unk.id = 0x0123456789abcdefL;
            unk.kind = 8;
            unk.shape = new TableVocabulary.Shape();
            unk.shape.kind = 8;
            unk.shape.packing = 0;
            unk.shape.bits = 32;
            v2.entries[0] = unk;
            final byte[] wBuf = new byte[16];
            final TableMessage.Writer w = new TableMessage.Writer(wBuf, 2, false);
            w.put(1L, v2.refBits);
            w.put(0xdeadbeefL, 32);
            w.put(0L, v2.refBits);
            w.align();
            final int used = (int) (w.bits / 8);
            wBuf[0] = 2;
            wBuf[1] = 0;
            final byte[] slice = java.util.Arrays.copyOf(wBuf, 2 + used);
            final int count = RootMessage.loadMessages(values, values.length, v2, slice, rep);
            okAll = okAll && !rep.malformed && !rep.refused && count == 1 && rep.unknown == 1 && values[0].a == 0;
            say("unknown_field_skipped", okAll, "count=" + count + " unknown=" + rep.unknown + " malformed=" + rep.malformed);
        }
        {
            // "AN INDEX ABOVE count IS BATCH DAMAGE" (§3.3): d's 0.01 steps over
            // [-1, 1] announce count = 200 at eight bits, which spell up to 255.
            long slot = 0;
            int qbits = 0;
            long qcount = 0;
            for (int i = 0; i < vocab.count; i++) {
                final TableVocabulary.Entry e = vocab.entries[i];
                if (e.kind == 10 && e.shape.packing == 2) {
                    slot = i + 1;
                    qbits = (int) e.shape.bits;
                    qcount = e.shape.steps;
                }
            }
            final boolean shaped = slot != 0 && qcount == 200 && qbits == 8;
            final byte[] atCountBuf = new byte[16];
            final TableMessage.Writer w1 = new TableMessage.Writer(atCountBuf, 2, false);
            w1.put(slot, vocab.refBits);
            w1.put(qcount, qbits);
            w1.put(0L, vocab.refBits);
            w1.align();
            atCountBuf[0] = 2;
            atCountBuf[1] = 0;
            final byte[] wireAtCount = java.util.Arrays.copyOf(atCountBuf, 2 + (int) (w1.bits / 8));
            final RootFixed.Value[] vAt = new RootFixed.Value[] { new RootFixed.Value() };
            final TableVocabulary.Report repAt = new TableVocabulary.Report();
            final int countAt = RootMessage.loadMessages(vAt, 1, vocab, wireAtCount, repAt);
            say("quantized_index_at_count",
                shaped && !repAt.malformed && !repAt.refused && countAt == 1 && vAt[0].d == 1.0f,
                "countAt=" + countAt + " d=" + vAt[0].d + " malformed=" + repAt.malformed);

            final byte[] aboveBuf = new byte[16];
            final TableMessage.Writer w2 = new TableMessage.Writer(aboveBuf, 2, false);
            w2.put(slot, vocab.refBits);
            w2.put(255L, qbits);
            w2.put(0L, vocab.refBits);
            w2.align();
            aboveBuf[0] = 2;
            aboveBuf[1] = 0;
            final byte[] wireAbove = java.util.Arrays.copyOf(aboveBuf, 2 + (int) (w2.bits / 8));
            final RootFixed.Value[] vAbove = new RootFixed.Value[] { new RootFixed.Value() };
            final TableVocabulary.Report repAbove = new TableVocabulary.Report();
            final int countAbove = RootMessage.loadMessages(vAbove, 1, vocab, wireAbove, repAbove);
            say("quantized_index_above_count",
                shaped && repAbove.malformed && !repAbove.refused && countAbove == 0 && vAbove[0].d == 0.0f,
                "countAbove=" + countAbove + " d=" + vAbove[0].d + " malformed=" + repAbove.malformed);
        }
`
