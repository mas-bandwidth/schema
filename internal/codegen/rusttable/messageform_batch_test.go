package rusttable

// TestMessageFormBatchRust holds the MESSAGE FORM's BATCH (docs/SPEC-TABLES.md
// §3.3) to the Rust leg: "A BATCH OF BODIES IS THE UNIT THIS FORM READS AND
// WRITES, and a single message is the batch of one". SaveMessages takes an
// array of bodies of ONE root and writes ONE buffer, LoadMessages reads one
// buffer into the caller's storage against the endpoint's announced vocabulary,
// and MeasureMessages sizes the buffer. The announcement half landed with
// TestMessageFormAnnounceRust (messageform_announce_test.go); this test owes
// the batch itself.
//
// THE BYTES ARE NEVER THIS LEG'S OWN. Every expectation is one of two oracles:
//
//   - the manifest's `message` rows (testdata/conformance/tables/MANIFEST.txt),
//     the C++ reference's bytes, which the Go leg (gotable messagewrite.go and
//     messageread.go) reproduces; and
//   - the repository's own Go codec (internal/tablewire over
//     internal/tabletext), which writes the batch for a value read from text and
//     decodes a stranger's batch the way §3.3 says, for the constructs and the
//     versioning rules no manifest row reaches.
//
// THE USE CASE IS A CARD WRITTEN IN ONE LANGUAGE AND READ AND UPGRADED IN
// ANOTHER (Glenn, 2026-10-12), so the versioning clause is the one that
// matters most: an older writer's batch read under its own announcement by a
// newer reader, every field kept, the counters §4 names moving exactly as the
// oracle's, and the upgraded value written back byte for byte as the oracle
// writes it.
//
// A FUNCTIONAL TEST: it compiles and runs Rust with rustc. It skips by name
// when rustc is absent and SCHEMA_REQUIRE_CORPUS is unset, and fails by name
// when SCHEMA_REQUIRE_CORPUS=1, which is how `make tables-rust-message-batch`
// runs it, so that target never passes on a SKIP. The Go half reads only the
// generator's output and the oracles: no clock, no socket, no network.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestMessageFormBatchRust(t *testing.T) {
	t.Parallel()
	messageBatchRustc(t)
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}

	// 1. THE MANIFEST's `message` ROWS: AnnounceRead the connection's
	// announcement, LoadMessages the reference's batch against it,
	// MeasureMessages equals its length and SaveMessages reproduces it byte
	// for byte (§3.3; test/conformance/README.md's `message` surface).
	t.Run("manifest_rows", func(t *testing.T) {
		t.Parallel()
		rows := messageBatchManifest(t, root)
		// "`fixed table T` may ride here and a variable table may not" (§3.3):
		// a row whose root is a variable table (graph_tree's Scene) carries no
		// verbs, and its absence is the conformance driver's `.absent`, never a
		// pass here. Such rows are set aside before any subtest runs, so the
		// target never reports a SKIP.
		byUnit := map[string][]messageBatchRow{}
		units := map[string]*ir.Unit{}
		var names []string
		var plain []string
		for _, row := range rows {
			u, ok := units[row.unit]
			if !ok {
				u = generateUnit(t, root, row.paths)
				units[row.unit] = u
			}
			if st := messageBatchTable(u, row.root); st == nil || !st.FixedDeclared {
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
						t.Errorf("%s/%s: the Rust batch does not reproduce the reference bytes: %s", unit, row.name, line)
					}
				}
			})
		}
	})

	// 2. THE CONSTRUCTS NO MANIFEST ROW REACHES, against the Go codec: every
	// scalar kind and packing the fixed form carries, text, bytes and wide
	// text, fixed and counted arrays, a nested type, an enum array, and a
	// union with a table arm, a scalar arm, an enum arm and a payload-free arm.
	// The oracle writes the batch from text; the Rust leg loads it, measures
	// it, and writes it back byte for byte.
	t.Run("oracle_constructs", func(t *testing.T) {
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
		// AND THE WHOLE SET AS ONE BATCH: the bodies are one continuous bit
		// stream with no alignment between them (§3.3), which a batch of one
		// never exercises.
		wire, err := tablewire.EncodeMessages(m, insts)
		if err != nil {
			t.Fatalf("the oracle did not encode the batch: %v", err)
		}
		names = append(names, "probe_batch")
		body.WriteString(messageBatchRoundTrip("probe_batch", "Probe", announce, wire))
		out := runMessageBatchProbe(t, u, body.String())
		for _, name := range names {
			if line := messageBatchLine(out, name); line != "BATCH "+name+" ok" {
				t.Errorf("%s: the Rust batch does not reproduce the Go codec's bytes: %s", name, line)
			}
		}
	})

	// 3. VERSIONING, the use case: an OLDER writer's batch read under ITS
	// announcement by a NEWER reader (§3.3: "a reader decodes a body against
	// the table THAT endpoint announced"). A removed field is skipped and counted
	// unknown, a widened kind is decoded exactly and counted widened, a
	// shrunk capacity, count bound or range clamps and counts clamped, a field
	// the writer never had takes its declared default, and an unknown arm and
	// an unknown enum variant land as None and count unknown. The counters are
	// the oracle's, and the upgraded value saves back as the oracle saves it.
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
        const ANN: [u8; %d] = [%s];
        const OLD: [u8; %d] = [%s];
        const WANT: [u8; %d] = [%s];
        let mut storage = vec![TableMessageEntry::default(); 4096];
        let mut vocab = TableVocabulary::new(&mut storage);
        let mut arep = TableReport::default();
        let announced = announce_read(&mut vocab, &ANN, &mut arep);
        let mut values = vec![CardRow::default(); %d];
        let mut lrep = TableReport::default();
        let (count, ok) = card_load_messages(&mut values, &vocab, &OLD, &mut lrep);
        let counters = (lrep.unknown, lrep.kind_mismatch, lrep.widened, lrep.clamped);
        let want = (%d, %d, %d, %d);
        let mut srep = TableReport::default();
        let mut buf = vec![0xa5u8; WANT.len()];
        let wrote = card_save_messages(&values[..count], &mut buf, &mut srep);
        let mut mrep = TableReport::default();
        let measured = card_measure_messages(&values[..count], &mut mrep);
        say("upgrade", announced && ok && count == %d && counters == want && wrote == WANT.len() as i64 && measured == WANT.len() as i64 && buf == WANT,
            format!("announced={announced} ok={ok} count={count} counters={counters:?} want={want:?} wrote={wrote} measured={measured} same={} report={lrep:?}", buf == WANT));
    }
`, len(announce), rustBytes(announce), len(wire), rustBytes(wire), len(upgraded), rustBytes(upgraded),
			len(olds), rep.Unknown, rep.KindMismatch, rep.Widened, rep.Clamped, len(olds))
		out := runMessageBatchProbe(t, newer, main)
		if line := messageBatchLine(out, "upgrade"); line != "BATCH upgrade ok" {
			t.Fatalf("the newer Rust reader does not upgrade the older batch as the oracle does: %s", line)
		}
	})

	// 4. THE BATCH's REFUSALS AND ITS DAMAGE RULES (§3.3), each by name:
	// "`M` ABOVE 256 ON THE WRITE SIDE IS A REFUSAL BY NAME" (MeasureMessages
	// and SaveMessages both, -1), "`M` ABOVE THE CALLER'S CAPACITY ON THE READ
	// SIDE IS A REFUSAL BY NAME ... The reader sets the returned count to the
	// wire's `M`", "A BODY FROM AN ENDPOINT THAT NEVER ANNOUNCED IS REFUSED BY
	// NAME", the form byte read FIRST and refused as `newer_form`, "DAMAGE
	// INSIDE BODY `k` DELIVERS BODIES `1` TO `k - 1`", and "BYTES AFTER THE PAD
	// ARE MALFORMED".
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

// ---- the manifest ----------------------------------------------------------

type messageBatchRow struct {
	name, root, unit  string
	paths             []string
	announce, message []byte
}

// messageBatchManifest joins the manifest's `message` rows to their
// `connection` (the announcement) and `unit` (the schema paths) rows. A row is
// `message <name> <connection> <root> <file-form wire> <message-form wire>`.
func messageBatchManifest(t *testing.T, root string) []messageBatchRow {
	t.Helper()
	lines := readManifest(t, filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt"))
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
		if len(f) != 6 || f[0] != "message" || f[5] == "-" { // a named refusal row (a variable table, §3.3) carries no message bytes
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

func messageBatchTable(u *ir.Unit, name string) *ir.Struct {
	for _, st := range u.Tables {
		if st.Name == name {
			return st
		}
	}
	return nil
}

// ---- the probe -------------------------------------------------------------

// messageBatchRustc is the functional gate: rustc on PATH, or a skip by name,
// or — under SCHEMA_REQUIRE_CORPUS — a failure by name.
func messageBatchRustc(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("rustc"); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and rustc is not on PATH, so the emitted message batch cannot be built or run: %v", err)
		}
		t.Skipf("rustc is not on PATH and SCHEMA_REQUIRE_CORPUS is unset: this is a functional test that compiles and runs Rust: %v", err)
	}
}

// runMessageBatchProbe writes the unit's fixed-form and message-form modules
// beside a crate root and builds them with rustc alone: a unit's table
// runtimes are self-contained, so no packet module and no cargo workspace is
// needed. The probe prints one `BATCH <id> ok` or `BATCH <id> BAD <why>` line
// per clause.
func runMessageBatchProbe(t *testing.T, u *ir.Unit, body string) string {
	t.Helper()
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	dir := t.TempDir()
	var mods []string
	for name, data := range files {
		keep := name == BuildVersionModule+".rs" || name == FixedRuntimeModule+".rs" || name == MessageRuntimeModule+".rs" ||
			name == VariableRuntimeModule+".rs" ||
			strings.HasSuffix(name, "_records.rs") || strings.HasSuffix(name, "_fixed.rs") || strings.HasSuffix(name, "_message.rs")
		if !keep {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
		mods = append(mods, strings.TrimSuffix(name, ".rs"))
	}
	sort.Strings(mods)
	var main strings.Builder
	main.WriteString("#![allow(dead_code, unused_imports, unused_variables)]\n")
	for _, m := range mods {
		fmt.Fprintf(&main, "mod %s;\npub use %s::*;\n", m, m)
	}
	main.WriteString(`
fn say(id: &str, ok: bool, why: String) {
    if ok {
        println!("BATCH {id} ok");
    } else {
        println!("BATCH {id} BAD {why}");
    }
}

fn main() {
`)
	main.WriteString(body)
	main.WriteString("}\n")
	if err := os.WriteFile(filepath.Join(dir, "main.rs"), []byte(main.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "batchprobe")
	cmd := exec.Command("rustc", "--edition", "2024", "-o", bin, "main.rs")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the probe did not build: %v\n%s", err, out)
	}
	run := exec.Command(bin)
	run.Dir = dir
	out, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("the probe did not run: %v\n%s", err, out)
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

// messageBatchRoundTrip is one row's three verbs against pinned bytes: the
// announcement read, LoadMessages of the batch with storage for every body the
// form can spell, MeasureMessages equal to the batch's length, and
// SaveMessages into a buffer FILLED WITH GARBAGE first, so a writer that ORs
// into the caller's bytes instead of writing them goes red.
func messageBatchRoundTrip(name, root string, announce, message []byte) string {
	snake := ir.RustSnake(root)
	bodies := int(message[1]) + 1
	return fmt.Sprintf(`
    {
        const ANN: [u8; %d] = [%s];
        const MSG: [u8; %d] = [%s];
        let mut storage = vec![TableMessageEntry::default(); 4096];
        let mut vocab = TableVocabulary::new(&mut storage);
        let mut arep = TableReport::default();
        let announced = announce_read(&mut vocab, &ANN, &mut arep);
        let mut values = vec![%sRow::default(); TABLE_MESSAGE_BATCH_MAX];
        let mut lrep = TableReport::default();
        let (count, ok) = %s_load_messages(&mut values, &vocab, &MSG, &mut lrep);
        let mut mrep = TableReport::default();
        let measured = %s_measure_messages(&values[..count], &mut mrep);
        let mut buf = vec![0xa5u8; MSG.len()];
        let mut srep = TableReport::default();
        let wrote = %s_save_messages(&values[..count], &mut buf, &mut srep);
        let clean = lrep == TableReport::default();
        say(%q, announced && ok && clean && count == %d && measured == MSG.len() as i64 && wrote == MSG.len() as i64 && buf == MSG,
            format!("announced={announced} ok={ok} count={count} report={lrep:?} measured={measured} wrote={wrote} want={} got={:02x?} want={:02x?}", MSG.len(), buf, MSG));
    }
`, len(announce), rustBytes(announce), len(message), rustBytes(message), root, snake, snake, snake, name, bodies)
}

// ---- the oracle units ------------------------------------------------------

// messageBatchProbeSrc is every construct the Rust fixed form carries that the
// message form's batch owes (§3.3's shape table): each integer width raw and
// ranged, 128-bit raw and ranged, bits(N), flags, bool, float32 raw and
// compressed, float64, an enum and an enum array, string with a declared
// default, bytes, wide text, fixed and counted arrays of scalars and of a
// nested type, and a union whose arms are a table, a scalar, an enum and
// nothing at all.
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
    label string(12) = "hi"
    blob  bytes(6)
    wide  wstring(5)
    small [4]int16
    many  [..5]uint32
    spot  Spot
    spots [..3]Spot
    pair  [2]Spot
    pick  Pick
    picks [2]Pick
}
`

// Every text is a FIXED POINT of load-then-save. A text field rides when it is
// non-empty (the C++ reference's rule, and tablewire's), so `label` set to ""
// under its declared default "hi" is absent on the wire and loads back as "hi":
// that value is not a round trip on this form, and no text here spells it.
var messageBatchProbeTexts = []string{
	`{}`,
	`{
  "i8v": -128, "i16v": 32767, "i32v": -2147483648, "i64v": 9223372036854775807,
  "u8v": 255, "u16v": 65535, "u32v": 4294967295, "u64v": 18446744073709551615,
  "r32": -1000, "ru64": 900000, "i128v": -5000000000, "u128v": 340282366920938463463374607431768211455,
  "b3": 7, "caps": ["Jump", "Fly"], "flag": true, "f32v": 3.25, "cf": 0.37, "f64v": -1e-300,
  "mood": "Sleepy", "moods": ["Calm", "None", "Angry"], "label": "héllo wörl",
  "blob": "AQIDBAUG", "wide": "wïde", "small": [-1, 0, 127, -32768],
  "many": [1, 2, 3, 4, 5], "spot": {"x": -4, "y": 9},
  "spots": [{"x": 1, "y": 2}, {"x": 0, "y": 3}], "pair": [{"x": 5, "y": 3}, {"x": -5, "y": 0}],
  "pick": {"leaf": {"id": 77, "name": "leafy"}},
  "picks": [{"qty": 499}, {"mood": "Calm"}]
}`,
	`{"i16v": -3, "u32v": 7, "mood": "Angry", "label": "hi", "pick": {"ping": null}, "picks": [{"qty": 10}, {}], "spot": {"x": 0, "y": 3}}`,
	`{"mood": "None", "cf": -1, "many": [], "spots": [{"x": 0, "y": 0}], "pick": {"mood": "Sleepy"}, "small": [0, 0, 0, 1]}`,
}

// messageBatchOlderSrc and messageBatchNewerSrc are ONE CARD at two versions:
// the newer reader widened `score`, shrank `title`, `tags` and `level`'s
// range, retyped `rank` (a kind mismatch), dropped `gone` and `legacy`'s arm,
// gained `added` and an enum variant.
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

// messageBatchSmallSrc is the refusal probe's one fixed root: its vocabulary
// is small, so the reference width is a handful of bits and a forged body is a
// byte or two.
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
    let mut storage = vec![TableMessageEntry::default(); 4096];
    let mut vocab = TableVocabulary::new(&mut storage);
    let mut arep = TableReport::default();
    let announced = announce_read(&mut vocab, &TABLE_ANNOUNCEMENT, &mut arep);
    assert!(announced, "the unit's own announcement reads");
    let mut one = RootRow::default();
    one.a = -5;
    one.b = true;
    one.c = 200;
    let m = root_measure_messages(&[one], &mut TableReport::default());
    let mut wire = vec![0u8; m as usize];
    assert!(root_save_messages(&[one], &mut wire, &mut TableReport::default()) == m);
    {
        // "A BODY FROM AN ENDPOINT THAT NEVER ANNOUNCED IS REFUSED BY NAME. Nothing
        // is decoded, no counter moves and malformed does not fire."
        let mut fresh_storage = vec![TableMessageEntry::default(); 4096];
        let fresh = TableVocabulary::new(&mut fresh_storage);
        let mut rep = TableReport::default();
        let mut values = vec![RootRow::default(); 4];
        let (count, ok) = root_load_messages(&mut values, &fresh, &wire, &mut rep);
        say("no_vocabulary", !ok && count == 0 && rep.refused && rep.reason == TableReason::NoVocabulary && !rep.malformed && rep.unknown == 0 && values[0].a == 0,
            format!("count={count} report={rep:?}"));
    }
    {
        // "M ABOVE 256 ON THE WRITE SIDE IS A REFUSAL BY NAME": both verbs
        // refuse and return -1.
        let huge = vec![RootRow::default(); TABLE_MESSAGE_BATCH_MAX + 1];
        let mut rep = TableReport::default();
        let got = root_measure_messages(&huge, &mut rep);
        say("batch_too_large_measure", got == -1 && rep.refused && rep.reason == TableReason::BatchTooLarge, format!("got={got} report={rep:?}"));
        let mut rep = TableReport::default();
        let mut buf = vec![0u8; 4096];
        let got = root_save_messages(&huge, &mut buf, &mut rep);
        say("batch_too_large_save", got == -1 && rep.refused && rep.reason == TableReason::BatchTooLarge, format!("got={got} report={rep:?}"));
    }
    {
        // "M ABOVE THE CALLER'S CAPACITY ON THE READ SIDE IS A REFUSAL BY NAME
        // ... The reader sets the returned count to the wire's M."
        let three = [one, one, one];
        let m = root_measure_messages(&three, &mut TableReport::default());
        let mut buf = vec![0u8; m as usize];
        assert!(root_save_messages(&three, &mut buf, &mut TableReport::default()) == m);
        let mut values = vec![RootRow::default(); 2];
        let mut rep = TableReport::default();
        let (count, ok) = root_load_messages(&mut values, &vocab, &buf, &mut rep);
        say("batch_too_large_read", !ok && count == 3 && rep.refused && rep.reason == TableReason::BatchTooLarge && !rep.malformed,
            format!("count={count} report={rep:?}"));
    }
    {
        // "The FORM BYTE is a whole byte and is read FIRST ... a form a reader
        // does not know is a REFUSAL by name and never damage."
        let mut all = true;
        for form in [1u8, 3, 6] {
            let mut values = vec![RootRow::default(); 1];
            let mut rep = TableReport::default();
            let (count, ok) = root_load_messages(&mut values, &vocab, &[form], &mut rep);
            all = all && !ok && count == 0 && rep.refused && rep.reason == TableReason::NewerForm && !rep.malformed;
        }
        say("newer_form", all, "a foreign form byte was not refused as newer_form".to_string());
    }
    {
        // "DAMAGE INSIDE BODY k DELIVERS BODIES 1 TO k - 1. The returned count
        // states k - 1, ONE malformed counts, and nothing at or after body k is
        // read." Body 1 is the empty body; body 2 opens with the all-ones
        // reference, which names no slot of this vocabulary.
        let bits = vocab.ref_bits;
        let mut forged = vec![2u8, 2u8, 0, 0, 0, 0];
        let ones = (1u64 << bits) - 1;
        let v = ones << bits;
        for i in 0..4 {
            forged[2 + i] = (v >> (8 * i)) as u8;
        }
        let mut values = vec![RootRow::default(); 3];
        let mut rep = TableReport::default();
        let (count, ok) = root_load_messages(&mut values, &vocab, &forged, &mut rep);
        say("damage_in_body_two", !ok && count == 1 && rep.malformed && !rep.refused, format!("count={count} report={rep:?}"));
    }
    {
        // "BYTES AFTER THE PAD ARE MALFORMED ... a buffer with bytes left over
        // describes no batch this reader can name."
        let mut padded = wire.clone();
        padded.push(0);
        let mut values = vec![RootRow::default(); 1];
        let mut rep = TableReport::default();
        let (count, ok) = root_load_messages(&mut values, &vocab, &padded, &mut rep);
        say("bytes_after_pad", !ok && count == 1 && rep.malformed && !rep.refused, format!("count={count} report={rep:?}"));
    }
    {
        // A buffer too short for the batch is a -1 and never a partial write.
        let mut buf = vec![0u8; (m - 1) as usize];
        let got = root_save_messages(&[one], &mut buf, &mut TableReport::default());
        say("short_buffer", got == -1, format!("got={got}"));
    }
    {
        // A field this reader cannot name, under a slot the ENDPOINT announced, is
        // skipped by its announced shape and counted unknown (§3.3). The endpoint
        // here is this build with one more entry: the reader's vocabulary is
        // forged by announcing a stranger's table whose slot 1 is an id this
        // build does not declare.
        let mut values = vec![RootRow::default(); 1];
        let mut rep = TableReport::default();
        let mut v2_storage = vec![TableMessageEntry::default(); 4096];
        let mut v2 = TableVocabulary::new(&mut v2_storage);
        let mut ok_all = announce_read(&mut v2, &TABLE_ANNOUNCEMENT, &mut TableReport::default());
        // slot 1 is now an unknown u32 field
        v2.entries[0] = TableMessageEntry { id: 0x0123_4567_89ab_cdef, kind: 8, ..Default::default() };
        let mut w_buf = vec![0u8; 16];
        let mut w = TableBitWriter::new(&mut w_buf[2..]);
        w.put(1, v2.ref_bits);
        w.put(0xdead_beef, 32);
        w.put(0, v2.ref_bits);
        w.align();
        let used = (w.bits / 8) as usize;
        w_buf[0] = 2;
        w_buf[1] = 0;
        let (count, ok) = root_load_messages(&mut values, &v2, &w_buf[..2 + used], &mut rep);
        ok_all = ok_all && ok && count == 1 && rep.unknown == 1 && !rep.malformed && values[0].a == 0;
        say("unknown_field_skipped", ok_all, format!("count={count} ok={ok} report={rep:?}"));
    }
    {
        // "AN INDEX ABOVE count IS BATCH DAMAGE" (§3.3): d's 0.01 steps over
        // [-1, 1] announce count = 200 at eight bits, which spell up to 255.
        // An index of exactly count is the range's top and reads; one above it
        // stops the batch at this body (no body delivered, ONE malformed) and
        // is never reconstructed and clamped onto max.
        let mut slot = 0u64;
        let mut qbits = 0u32;
        let mut qcount = 0u32;
        for (i, e) in vocab.entries[..vocab.count].iter().enumerate() {
            if e.kind == 10 && e.shape.packing == 2 {
                slot = (i + 1) as u64;
                qbits = e.shape.bits as u32;
                qcount = e.shape.qcount;
            }
        }
        let forge = |index: u64| -> Vec<u8> {
            let mut buf = vec![0u8; 16];
            let used = {
                let mut w = TableBitWriter::new(&mut buf[2..]);
                w.put(slot, vocab.ref_bits);
                w.put(index, qbits);
                w.put(0, vocab.ref_bits);
                w.align();
                (w.bits / 8) as usize
            };
            buf[0] = 2;
            buf[1] = 0;
            buf.truncate(2 + used);
            buf
        };
        let shaped = slot != 0 && qcount == 200 && qbits == 8;
        let mut values = vec![RootRow::default(); 1];
        let mut rep = TableReport::default();
        let (count, ok) = root_load_messages(&mut values, &vocab, &forge(qcount as u64), &mut rep);
        say("quantized_index_at_count", shaped && ok && count == 1 && !rep.malformed && values[0].d == 1.0,
            format!("slot={slot} bits={qbits} count={qcount} delivered={count} ok={ok} d={} report={rep:?}", values[0].d));
        let mut values = vec![RootRow::default(); 1];
        let mut rep = TableReport::default();
        let (count, ok) = root_load_messages(&mut values, &vocab, &forge(255), &mut rep);
        say("quantized_index_above_count", shaped && !ok && count == 0 && rep.malformed && !rep.refused && values[0].d == 0.0,
            format!("slot={slot} bits={qbits} count={qcount} delivered={count} ok={ok} d={} report={rep:?}", values[0].d));
    }
`
