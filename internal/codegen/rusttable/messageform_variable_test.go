package rusttable

// TestMessageFormVariableRust holds the MESSAGE FORM over a POINTERED root
// (docs/SPEC-TABLES.md §3.1, §3.3) to the Rust leg: a variable root's batch,
// each body's NODE TABLE its first field, the index width the node count
// settles, a record's body ending at its own zero reference and a blob's
// thirty-two bit length and align, and no node table at all for a root that
// reaches no node.
//
// THE BYTES ARE NEVER THIS LEG'S OWN. The expectations are the manifest's
// `graph_tree` message row (the C++ reference's bytes) and the repository's own
// codec (internal/tablewire), which writes the batch for a value read from
// text, decodes a batch with the counters §4 names, and is the engine the
// harness's wire fuzzer compares against.
//
// A FUNCTIONAL TEST: it compiles and runs Rust with rustc, skipping by name
// when rustc is absent and SCHEMA_REQUIRE_CORPUS is unset and failing by name
// when it is set (`make tables-rust-message-variable`). The Go half reads only
// the generator's output and the oracles: no clock, no socket, no network.

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

func TestMessageFormVariableRust(t *testing.T) {
	t.Parallel()
	messageBatchRustc(t)
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}

	// 1. THE MANIFEST's POINTERED ROW: graph_tree's Scene, the reference's
	// bytes, loaded, measured and saved back byte for byte.
	t.Run("manifest_graph_tree", func(t *testing.T) {
		t.Parallel()
		var row *messageBatchRow
		for _, r := range messageBatchManifest(t, root) {
			if r.name == "graph_tree" {
				row = &r
			}
		}
		if row == nil {
			t.Fatal("the manifest carries no graph_tree message row")
		}
		u := generateUnit(t, root, row.paths)
		out := runVariableMessageProbe(t, u, variableMessageRoundTrip(row.name, row.root, row.announce, row.message, nil))
		if line := messageBatchLine(out, row.name); line != "BATCH "+row.name+" ok" {
			t.Fatalf("the Rust batch does not reproduce the reference's graph_tree bytes: %s", line)
		}
	})

	// 2. THE CONSTRUCTS AND THE NUMBERING against the Go codec: a tree, a node
	// two slots name, a chain, both blob kinds, pointer arrays with null
	// slots, fixed and counted arrays of tables, enums and scalars, optionals,
	// a fixed table by value and by pointer, and a root that reaches no node.
	// Each body alone and all of them as one batch, the Rust load's report
	// equal to the codec's decode report.
	t.Run("oracle_constructs", func(t *testing.T) {
		t.Parallel()
		u := unitFrom(t, variableMessageProbeSrc)
		m := tabletext.NewModel(u)
		var insts []*tabletext.Instance
		for i, text := range variableMessageProbeTexts {
			inst := m.New(m.Lookup("Root"))
			var rep tabletext.Report
			if !m.Read(inst, []byte(text), &rep) || rep != (tabletext.Report{}) {
				t.Fatalf("text %d: the oracle did not read the probe text cleanly: %+v", i, rep)
			}
			insts = append(insts, inst)
		}
		announce := ir.TableAnnouncement(u)
		var body strings.Builder
		var names []string
		add := func(name string, batch []*tabletext.Instance) {
			wire, err := tablewire.EncodeMessages(m, batch)
			if err != nil {
				t.Fatalf("%s: the oracle did not encode: %v", name, err)
			}
			names = append(names, name)
			body.WriteString(variableMessageRoundTrip(name, "Root", announce, wire, oracleCounters(t, m, announce, wire, len(batch))))
		}
		for i, inst := range insts {
			add(fmt.Sprintf("probe_%d", i), []*tabletext.Instance{inst})
		}
		add("probe_batch", insts)
		out := runVariableMessageProbe(t, u, body.String())
		for _, name := range names {
			if line := messageBatchLine(out, name); line != "BATCH "+name+" ok" {
				t.Errorf("%s: the Rust batch does not agree with the Go codec: %s", name, line)
			}
		}
	})

	// 3. VERSIONING: an OLDER writer's pointered batch read under its own
	// announcement by a NEWER reader. A removed pointer field is stepped over
	// at the body's index width and counted unknown, a node record of a type
	// the newer root cannot place keeps its index and counts unknown once, a
	// widened kind decodes exactly, and the upgraded value saves back as the
	// codec saves it.
	t.Run("versioning_upgrade", func(t *testing.T) {
		t.Parallel()
		older := unitFrom(t, variableMessageOlderSrc)
		newer := unitFrom(t, variableMessageNewerSrc)
		mo := tabletext.NewModel(older)
		mn := tabletext.NewModel(newer)
		var olds []*tabletext.Instance
		for i, text := range variableMessageOlderTexts {
			inst := mo.New(mo.Lookup("Deck"))
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
			news[i] = mn.New(mn.Lookup("Deck"))
		}
		var rep tabletext.Report
		count, ok, err := tablewire.DecodeMessages(mn, news, wire, v, &rep)
		if err != nil || !ok || count != len(olds) {
			t.Fatalf("the oracle did not decode the older batch: count=%d ok=%v err=%v", count, ok, err)
		}
		if rep.Unknown < 2 || rep.Widened == 0 {
			t.Fatalf("the versioning case no longer exercises the unknown pointer, the unknown record and the widening: %+v", rep)
		}
		upgraded, err := tablewire.EncodeMessages(mn, news)
		if err != nil {
			t.Fatalf("the oracle did not encode the upgraded batch: %v", err)
		}
		want := fmt.Sprintf("(%d, %d, %d, %d)", rep.Unknown, rep.KindMismatch, rep.Widened, rep.Clamped)
		main := fmt.Sprintf(`
    {
        const ANN: [u8; %d] = [%s];
        const OLD: [u8; %d] = [%s];
        const WANT: [u8; %d] = [%s];
        let mut storage = vec![TableMessageEntry::default(); 4096];
        let mut vocab = TableVocabulary::new(&mut storage);
        let mut arep = TableReport::default();
        let announced = announce_read(&mut vocab, &ANN, &mut arep);
        let mut values = vec![Deck::default(); %d];
        let mut lrep = TableReport::default();
        let (count, ok) = deck_load_messages(&mut values, &vocab, &OLD, &mut lrep);
        let counters = (lrep.unknown, lrep.kind_mismatch, lrep.widened, lrep.clamped);
        let want = %s;
        let mut srep = TableReport::default();
        let mut buf = vec![0xa5u8; WANT.len()];
        let wrote = deck_save_messages(&values[..count], &mut buf, &mut srep);
        let mut mrep = TableReport::default();
        let measured = deck_measure_messages(&values[..count], &mut mrep);
        say("upgrade", announced && ok && count == %d && counters == want && wrote == WANT.len() as i64 && measured == WANT.len() as i64 && buf == WANT,
            format!("announced={announced} ok={ok} count={count} counters={counters:?} want={want:?} wrote={wrote} measured={measured} same={} report={lrep:?}", buf == WANT));
    }
`, len(announce), rustBytes(announce), len(wire), rustBytes(wire), len(upgraded), rustBytes(upgraded),
			len(olds), want, len(olds))
		out := runVariableMessageProbe(t, newer, main)
		if line := messageBatchLine(out, "upgrade"); line != "BATCH upgrade ok" {
			t.Fatalf("the newer Rust reader does not upgrade the older pointered batch as the codec does: %s", line)
		}
	})

	// 4. THE WRITER's REFUSAL BY NAME: a blob two slots name is blob_shared
	// (§2.5, §16.7), from MeasureMessages and SaveMessages alike, with nothing
	// written.
	t.Run("blob_shared", func(t *testing.T) {
		t.Parallel()
		u := unitFrom(t, variableMessageProbeSrc)
		out := runVariableMessageProbe(t, u, `
    {
        let blob: std::rc::Rc<[u8]> = std::rc::Rc::from(&[1u8, 2, 3][..]);
        let mut value = Root::default();
        value.blob = Some(blob.clone());
        let mut leaf = Leaf::default();
        leaf.data = Some(blob);
        value.head = Some(std::rc::Rc::new(leaf));
        let values = vec![value];
        let mut mrep = TableReport::default();
        let measured = root_measure_messages(&values, &mut mrep);
        let mut buf = vec![0xa5u8; 64];
        let mut srep = TableReport::default();
        let wrote = root_save_messages(&values, &mut buf, &mut srep);
        say("blob_shared", measured == -1 && wrote == -1 && mrep.reason == TableReason::BlobShared && srep.reason == TableReason::BlobShared,
            format!("measured={measured} wrote={wrote} m={mrep:?} s={srep:?}"));
    }
`)
		if line := messageBatchLine(out, "blob_shared"); line != "BATCH blob_shared ok" {
			t.Fatal(line)
		}
	})
}

// oracleCounters is the codec's decode report over one batch, as the Rust
// tuple (unknown, kind_mismatch, widened, clamped, malformed).
func oracleCounters(t *testing.T, m *tabletext.Model, announce, wire []byte, n int) []string {
	t.Helper()
	v := &tablewire.Vocabulary{}
	var arep tabletext.Report
	if err := v.AnnounceRead(announce, &arep); err != nil {
		t.Fatalf("the oracle refused its own announcement: %v", err)
	}
	insts := make([]*tabletext.Instance, n)
	for i := range insts {
		insts[i] = m.New(m.Lookup("Root"))
	}
	var rep tabletext.Report
	count, ok, err := tablewire.DecodeMessages(m, insts, wire, v, &rep)
	if err != nil || !ok || count != n {
		t.Fatalf("the oracle did not decode its own batch: count=%d ok=%v err=%v", count, ok, err)
	}
	return []string{fmt.Sprint(rep.Unknown), fmt.Sprint(rep.KindMismatch), fmt.Sprint(rep.Widened), fmt.Sprint(rep.Clamped), fmt.Sprint(rep.Malformed)}
}

// variableMessageRoundTrip is one batch's three verbs against pinned bytes:
// LoadMessages, MeasureMessages equal to its length, SaveMessages into a
// buffer filled with garbage first. With counters it also requires the load's
// report to carry exactly the codec's; without, a clean report.
func variableMessageRoundTrip(name, root string, announce, message []byte, counters []string) string {
	snake := ir.RustSnake(root)
	bodies := int(message[1]) + 1
	check := "lrep == TableReport::default()"
	if counters != nil {
		check = fmt.Sprintf("(lrep.unknown, lrep.kind_mismatch, lrep.widened, lrep.clamped, lrep.malformed) == (%s, %s, %s, %s, %s)",
			counters[0], counters[1], counters[2], counters[3], counters[4])
	}
	return fmt.Sprintf(`
    {
        const ANN: [u8; %d] = [%s];
        const MSG: [u8; %d] = [%s];
        let mut storage = vec![TableMessageEntry::default(); 4096];
        let mut vocab = TableVocabulary::new(&mut storage);
        let mut arep = TableReport::default();
        let announced = announce_read(&mut vocab, &ANN, &mut arep);
        let mut values = vec![%s::default(); TABLE_MESSAGE_BATCH_MAX];
        let mut lrep = TableReport::default();
        let (count, ok) = %s_load_messages(&mut values, &vocab, &MSG, &mut lrep);
        let mut mrep = TableReport::default();
        let measured = %s_measure_messages(&values[..count], &mut mrep);
        let mut buf = vec![0xa5u8; MSG.len()];
        let mut srep = TableReport::default();
        let wrote = %s_save_messages(&values[..count], &mut buf, &mut srep);
        let clean = %s;
        say(%q, announced && ok && clean && count == %d && measured == MSG.len() as i64 && wrote == MSG.len() as i64 && buf == MSG,
            format!("announced={announced} ok={ok} count={count} report={lrep:?} measured={measured} wrote={wrote} want={} got={:02x?} want={:02x?}", MSG.len(), buf, MSG));
    }
`, len(announce), rustBytes(announce), len(message), rustBytes(message), root, snake, snake, snake, check, name, bodies)
}

// runVariableMessageProbe builds every table module of the unit but the two
// accelerators beside a crate root with rustc alone and runs it.
func runVariableMessageProbe(t *testing.T, u *ir.Unit, body string) string {
	t.Helper()
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if _, ok := files[VariableMessageRuntimeModule+".rs"]; !ok {
		t.Fatal("the unit carries no pointered message batch: vmessage_runtime.rs was not emitted")
	}
	dir := t.TempDir()
	var mods []string
	for name, data := range files {
		mod := strings.TrimSuffix(name, ".rs")
		if strings.HasSuffix(mod, "_block") || mod == BlockRuntimeModule || strings.HasSuffix(mod, "_cook") || mod == CookRuntimeModule || strings.HasSuffix(mod, "_variable") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
		mods = append(mods, mod)
	}
	sort.Strings(mods)
	var main strings.Builder
	main.WriteString("#![allow(dead_code, unused_imports, unused_variables)]\n")
	for _, m := range mods {
		fmt.Fprintf(&main, "mod %s;\npub use %s::*;\n", m, m)
	}
	main.WriteString("pub mod tables {\n")
	for _, m := range mods {
		fmt.Fprintf(&main, "    pub use super::%s::*;\n", m)
	}
	main.WriteString(`}

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
	bin := filepath.Join(dir, "vbatchprobe")
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

// variableMessageProbeSrc is every construct the pointered batch carries.
const variableMessageProbeSrc = `package vmsgprobe

enum Tone { Warm, Cool, Neutral }

fixed table Spec
{
    level int32 = 3 | min = 0, max = 9
    tag   string(6)
}

table Leaf
{
    label  string(10)
    weight float32
    tone   Tone
    next   *Leaf
    data   *bytes
    note   *string
}

table Root
{
    name    string(16)
    count   int64
    small   uint8 = 7
    ratio   float64
    flag    bool
    tone    Tone = Cool
    raw     bytes(8)
    head    *Leaf
    spec    *Spec
    inner   Spec
    opt     ?Spec
    mood    ?Tone
    qty     ?int32
    leaves  [2]Leaf
    some    [..3]Leaf
    ptrs    [2]*Leaf
    plist   [..3]*Leaf
    nums    [3]int16
    cnt     [..4]uint32
    tones   [..3]Tone
    blob    *bytes
    text    *string
}
`

var variableMessageProbeTexts = []string{
	`{}`,
	`{"name": "empty", "small": 9, "tone": "Warm", "inner": {"level": 5, "tag": "in"}, "nums": [1, -2, 3]}`,
	`{
  "name": "tree", "count": -77, "ratio": 2.5, "flag": true, "raw": "AQID",
  "head": {"&node": 1, "label": "a", "weight": 1.5, "tone": "Neutral", "data": "BAUG", "note": "hé",
    "next": {"label": "b", "next": {"label": "c"}}},
  "spec": {"level": 9, "tag": "spec"},
  "opt": {"level": 0, "tag": ""}, "mood": "Warm", "qty": -5,
  "leaves": [{"label": "l0"}, {"label": "l1", "next": {"&node": 1}}],
  "some": [{"label": "s0", "tone": "Cool"}],
  "ptrs": [null, {"&node": 1}],
  "plist": [{"label": "p0"}, null, {"&node": 1}],
  "cnt": [4, 5], "tones": ["Warm", "None", "Neutral"],
  "blob": "3q2+7w==", "text": "root text"
}`,
	`{"plist": [null], "ptrs": [null, null], "cnt": [0], "mood": "None", "qty": 0, "opt": {"level": 3, "tag": ""}}`,
}

// THE VERSIONING PAIR: the older Deck carries a pointer the newer one
// removed (a kind-17 field the reader steps over at the body's index width)
// whose target the newer root no longer reaches (a node record it cannot
// place), and a field the newer declaration widens.
const variableMessageOlderSrc = `package vmsgdeck

// a fixed table, so the unit carries the message form's announcement
fixed table Tag
{
    v int8
}

table Card
{
    title string(12)
    next  *Card
}

table Old
{
    note string(8)
}

table Deck
{
    name  string(12)
    level int16
    top   *Card
    gone  *Old
}
`

const variableMessageNewerSrc = `package vmsgdeck

// a fixed table, so the unit carries the message form's announcement
fixed table Tag
{
    v int8
}

table Card
{
    title string(12)
    next  *Card
}

table Deck
{
    name  string(12)
    level int64
    top   *Card
    fresh int32 = 4
}
`

var variableMessageOlderTexts = []string{
	`{"name": "deck", "level": -300, "top": {"title": "one", "next": {"title": "two"}}, "gone": {"note": "bye"}}`,
	`{"name": "plain", "gone": {"note": "x"}}`,
}
