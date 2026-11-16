package luautable_test

// A MESSAGE BODY CARRIES A QUANTIZED FLOAT32 AND AN ENUM-KEYED ARRAY
// (docs/SPEC-TABLES.md §3.3): the float rides its step index in
// bits_required(0, count) bits (packing 2), and the keyed array rides the
// number of present slots then one (key reference, element) pair per slot
// (kind 16). The walk that finds the body boundaries reads both at their
// announced widths, so a fixed table with both round-trips to the Go reference
// leg's bytes, and a reader built without the float skips it at the announced
// width and still reproduces the batch.

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// mqaSchema writes one schema beside root and returns the path buildMessageUnit
// and the compiler take.
func mqaSchema(t *testing.T, name, src string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(abs, p)
	if err != nil {
		t.Fatal(err)
	}
	return rel
}

// mqaUnit loads one schema file into a unit.
func mqaUnit(t *testing.T, rel string) *ir.Unit {
	t.Helper()
	files, err := compiler.GatherPaths(prefix([]string{rel}))
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(files)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

// mqaGoBatch is the Go reference leg's bytes for one body of the unit.
func mqaGoBatch(t *testing.T, u *ir.Unit, text string) []byte {
	t.Helper()
	model := tabletext.NewModel(u)
	inst := model.New(model.Lookup("Root"))
	var report tabletext.Report
	if !model.Read(inst, []byte(text), &report) || !report.Silent() {
		t.Fatalf("source %s: %+v", text, report)
	}
	batch, err := tablewire.EncodeMessages(model, []*tabletext.Instance{inst})
	if err != nil {
		t.Fatal(err)
	}
	return batch
}

func TestMessageQuantizedFloatAndEnumKeyedArray(t *testing.T) {
	t.Parallel()
	senderRel := mqaSchema(t, "Sender.schema", `package probe

enum Slot { Alpha, Beta, Gamma }

fixed table Root
{
    cf     float32 | min = 0.0, max = 10.0, resolution = 0.01
    tokens [Slot]int32
}
`)
	senderUnit := mqaUnit(t, senderRel)
	announce := tablewire.Announce(senderUnit)
	batch := mqaGoBatch(t, senderUnit, `{"cf":3.5,"tokens":{"Alpha":101,"Gamma":303}}`)

	// the sender walks its own batch and reproduces the Go bytes
	dir, mods, _ := buildMessageUnit(t, []string{senderRel})
	out := messageMain(t, dir, mods, "Root", announce, batch)
	lines := strings.Split(out, "\n")
	if len(lines) < 2 || !strings.HasPrefix(lines[0], "count 1 malformed false") {
		t.Fatalf("the sender's walk: %q", out)
	}
	got, err := hex.DecodeString(strings.TrimSpace(lines[len(lines)-1]))
	if err != nil {
		t.Fatalf("the sender's walk printed %q: %v", out, err)
	}
	if string(got) != string(batch) {
		t.Fatalf("the sender re-saved different bytes: got %x, want %x", got, batch)
	}

	// a reader built without the float skips it at its announced width and
	// still reproduces the batch
	receiverRel := mqaSchema(t, "Receiver.schema", `package receiver

enum Slot { Alpha, Beta, Gamma }

fixed table Root
{
    tokens [Slot]int32
}
`)
	rdir, rmods, _ := buildMessageUnit(t, []string{receiverRel})
	rout := messageMain(t, rdir, rmods, "Root", announce, batch)
	rlines := strings.Split(rout, "\n")
	if len(rlines) < 2 || !strings.HasPrefix(rlines[0], "count 1 malformed false") {
		t.Fatalf("a reader without the float: %q", rout)
	}
	rgot, err := hex.DecodeString(strings.TrimSpace(rlines[len(rlines)-1]))
	if err != nil {
		t.Fatalf("a reader without the float printed %q: %v", rout, err)
	}
	if string(rgot) != string(batch) {
		t.Fatalf("a reader without the float re-saved different bytes: got %x, want %x", rgot, batch)
	}
}
