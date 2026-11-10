package tablewire_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// The announcement carries the message closure alone (docs/SPEC-TABLES.md
// §3.3): the fixed tables and what they reach by value, and nothing a
// variable table alone can name.
const messageAnnounceUnit = `package announceprobe

enum Shade { Red, Green }

enum Tint { Pale, Deep }

type Inner
{
    a     int32
    shade Shade
}

union Payload
{
    inner Inner
    tally int32
}

union VarPayload
{
    vx int32
}

fixed table Root
{
    inner   Inner
    payload Payload
    shade   Shade
    n       int32
}

table VarRoot
{
    vtint  Tint
    vp     VarPayload
    vlabel string(8)
    vcount uint32
    vroot  *Root
    redHit int32 | was = "Red"
}
`

func TestMessageAnnounceCarriesMessageClosureAlone(t *testing.T) {
	t.Parallel()
	m := listModel(t, messageAnnounceUnit)
	u := m.Unit

	var vocab tablewire.Vocabulary
	var report tabletext.Report
	if err := vocab.AnnounceRead(tablewire.Announce(u), &report); err != nil {
		t.Fatalf("the unit refused its own announcement: %v %+v", err, report)
	}
	entries := vocab.Entries()
	want := tablewire.MessageVocabulary(u)
	if len(entries) != len(want) {
		t.Fatalf("announced %d entries, the message vocabulary holds %d", len(entries), len(want))
	}
	for i := range want {
		if entries[i].Key() != want[i].Key() {
			t.Fatalf("slot %d: announced %s, want %s", i+1, entries[i].Key(), want[i].Key())
		}
	}
	if full := ir.TableVocabulary(u); len(entries) >= len(full) {
		t.Fatalf("the trimmed announcement holds %d entries, the whole closure holds %d: nothing was trimmed", len(entries), len(full))
	}

	ids := map[uint64]bool{}
	keys := map[string]bool{}
	for _, e := range entries {
		ids[e.Id] = true
		keys[e.Key()] = true
	}
	// The tail takes no slot: not the node-table id, not a blob id, and not
	// any table's name id, fixed or variable.
	for _, id := range []uint64{ir.TableNodeWireId, ir.BytesWireTypeId, ir.StringWireTypeId} {
		if ids[id] {
			t.Errorf("the announcement carries the tail id %016x", id)
		}
	}
	for name, st := range u.Tables {
		if ids[ir.TableWireId(st.WireName())] {
			t.Errorf("the announcement carries table %s's name id", name)
		}
	}
	// Nothing only the variable table reaches is announced: not its fields,
	// not its enum's variants, not its union's arms.
	varRoot := u.Tables["VarRoot"]
	for _, f := range varRoot.Fields {
		if keys[ir.TableFieldEntry(f).Key()] {
			t.Errorf("the announcement carries variable-only field %s", f.Name)
		}
	}
	tint := u.Enums["Tint"]
	for i := range tint.Variants {
		if ids[ir.TableWireId(tint.VariantWireName(i))] {
			t.Errorf("the announcement carries variable-only variant %s", tint.Variants[i])
		}
	}
	// Everything the fixed root reaches by value is announced.
	root := u.Tables["Root"]
	for _, f := range root.Fields {
		if !keys[ir.TableFieldEntry(f).Key()] {
			t.Errorf("the announcement misses fixed field %s", f.Name)
		}
	}
	shade := u.Enums["Shade"]
	for i := range shade.Variants {
		if !ids[ir.TableWireId(shade.VariantWireName(i))] {
			t.Errorf("the announcement misses variant %s", shade.Variants[i])
		}
	}
	// redHit is a variable-table field whose wire name is the reached
	// variant Shade.Red. The kind-0 name stays; the field entry does not.
	redWire := shade.VariantWireNameOf("Red")
	var redHit *ir.Field
	for _, f := range varRoot.Fields {
		if f.Name == "redHit" {
			redHit = f
			break
		}
	}
	if redHit == nil {
		t.Fatal("redHit was not declared")
	}
	if got := ir.TableFieldWireName(redHit); got != redWire {
		t.Fatalf("redHit wire name %q, want Shade variant %q", got, redWire)
	}
	redID := ir.TableWireId(redWire)
	nameEntry := false
	for _, e := range entries {
		if e.Id != redID {
			continue
		}
		if e.Kind == 0 {
			nameEntry = true
			continue
		}
		t.Errorf("announced kind %d at Shade.%s wire id; a field or arm is not the variant name", e.Kind, redWire)
	}
	if !nameEntry {
		t.Errorf("Shade.%s kind-0 name entry is missing", redWire)
	}
	if keys[ir.TableFieldEntry(redHit).Key()] {
		t.Errorf("variable field redHit announced at Shade.%s wire id", redWire)
	}

	// A body written against the trimmed vocabulary reads back under the
	// announced one.
	inst := place(t, m, "Root", `{"inner":{"a":3,"shade":"Green"},"payload":{"tally":7},"shade":"Red","n":9}`)
	wire, err := tablewire.EncodeMessage(m, inst)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	dst := place(t, m, "Root", `{}`)
	var read tabletext.Report
	ok, err := tablewire.DecodeMessage(m, dst, wire, &vocab, &read)
	if err != nil || !ok {
		t.Fatalf("decode under the trimmed announcement: ok=%t err=%v %+v", ok, err, read)
	}
	if got := dst.Fields[3].Cell.I; got != 9 {
		t.Errorf("read back n=%d, want 9", got)
	}
}

// A unit with no fixed table announces an empty vocabulary, and that
// announcement still reads.
func TestMessageAnnounceEmptyWithoutFixedTable(t *testing.T) {
	t.Parallel()
	m := listModel(t, "package announceprobe\n\ntable VarRoot\n{\n    vcount uint32\n}\n")
	if got := tablewire.MessageVocabulary(m.Unit); len(got) != 0 {
		t.Fatalf("a variable-only unit's message vocabulary holds %d entries, want 0", len(got))
	}
	var vocab tablewire.Vocabulary
	var report tabletext.Report
	if err := vocab.AnnounceRead(tablewire.Announce(m.Unit), &report); err != nil {
		t.Fatalf("the empty announcement was refused: %v %+v", err, report)
	}
	if len(vocab.Entries()) != 0 {
		t.Fatalf("the empty announcement resolved %d entries", len(vocab.Entries()))
	}
}

// TestMessageAnnounceIdMatchNegativeControl plants the fault in the real
// predicate: the id match accepts a field (names[e.Id] with no kind check).
// The closure test must go red on redHit. Reverting the filter removes the
// kind check, so the planted text is no longer there and this control goes
// red instead of patching nothing.
func TestMessageAnnounceIdMatchNegativeControl(t *testing.T) {
	const (
		from = "keep[e.Key()] || (names[e.Id] && e.Kind == 0)"
		to   = "keep[e.Key()] || names[e.Id]"
		run  = "^TestMessageAnnounceCarriesMessageClosureAlone$"
	)
	out, failed := sabotageMessagePredicate(t, from, to, run)
	if !failed || !strings.Contains(out, "variable field redHit") {
		t.Fatalf("id match accepting a field did not turn the closure test red\n%s", out)
	}
}

// sabotageMessagePredicate rebuilds tablewire with one predicate edit and
// runs run. From must occur once, so a control cannot pass by patching nothing.
func sabotageMessagePredicate(t *testing.T, from, to, run string) (string, bool) {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	srcPath := filepath.Join(root, "internal/tablewire/messageclosure.go")
	data, err := os.ReadFile(srcPath)
	if err != nil {
		t.Fatal(err)
	}
	n := strings.Count(string(data), from)
	if n != 1 {
		t.Fatalf("negative control: %q occurs %d times in messageclosure.go, want once", from, n)
	}
	dir := t.TempDir()
	copyPath := filepath.Join(dir, "messageclosure.go")
	if err := os.WriteFile(copyPath, []byte(strings.Replace(string(data), from, to, 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	overlay, err := json.Marshal(map[string]any{"Replace": map[string]string{srcPath: copyPath}})
	if err != nil {
		t.Fatal(err)
	}
	overlayPath := filepath.Join(dir, "overlay.json")
	if err := os.WriteFile(overlayPath, overlay, 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), "go", "test", "-count=1", "-timeout", "15m", "-overlay", overlayPath, "-run", run, "./internal/tablewire")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	return string(out), err != nil
}
