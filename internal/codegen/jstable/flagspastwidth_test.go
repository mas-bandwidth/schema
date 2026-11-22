package jstable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

// A TABLE LOAD HOLDS A MASK TO ITS WIDTH (docs/SPEC-TABLES.md §4, R17): the
// newer reader reads old data, never the other way. The JavaScript reader that
// meets a flags bit at or above its W (the variant count, or K under
// | max = K) refuses the WHOLE load out_of_range in the variable form, the
// fixed form (a mask forged past W in a record its own writer wrote) and the
// message form (a newer writer announces a wider mask, and each value is held
// to the reader's own W); the variable and fixed savers refuse such a mask in
// every build. The variable file and the message batch are the Go oracle's,
// written under a newer declaration with one more variant. Red on the emitter
// that kept the bit.
const jsFlagsWidthReader = `package probe

flags Caps { Jump, Fly }

fixed table M
{
    caps Caps
}

table V
{
    caps Caps
}
`

const jsFlagsWidthProbe = `import { readFileSync } from "node:fs";
import { M, InitM, MFixedSave, MFixedLoad, MFixedNewPlan, MFixedMeasure, TableFixedReport, TableFixedRefusal, TableVocabulary, AnnounceRead, MLoadMessages } from "./ProbeTable.js";
import { VLoad, VSave, TableVariableReport } from "./ProbeVariable.js";
const bytes = (p) => new Uint8Array(readFileSync(p));
const fresh = () => { const m = new M(); InitM(m); return m; };
console.log("variable_in " + VLoad(bytes("in.wire"), new TableVariableReport()).caps);
console.log("variable_past " + (VLoad(bytes("past.wire"), new TableVariableReport()) === null));
const voc = new TableVocabulary();
voc.Init(new Array(64));
if (!AnnounceRead(voc, bytes("newer.announce"), {})) { throw new Error("announce refused"); }
{ const m = fresh(); const rep = {}; const n = MLoadMessages([m], voc, bytes("in.batch"), rep); console.log("message_in " + n + " " + m.caps); }
{ const m = fresh(); const rep = {}; const n = MLoadMessages([m], voc, bytes("past.batch"), rep); console.log("message_past " + (n === 1) + " " + rep.Reason); }
const need = MFixedMeasure(1);
const fixed = new Uint8Array(need);
{ const m = fresh(); m.caps = 2n; console.log("fixed_save_in " + (MFixedSave([m], 1, fixed) === need)); }
{ const back = [fresh()]; const rep = new TableFixedReport(); const n = MFixedLoad(back, 1, fixed, fixed.length, MFixedNewPlan(4096, 4096), rep); console.log("fixed_in " + n + " " + back[0].caps); }
fixed[need - 8] |= 4;
{ const back = [fresh()]; const rep = new TableFixedReport(); const n = MFixedLoad(back, 1, fixed, fixed.length, MFixedNewPlan(4096, 4096), rep); console.log("fixed_past " + n + " " + (rep.refused === TableFixedRefusal.OutOfRange)); }
{ const m = fresh(); m.caps = 6n; const buf = new Uint8Array(need); console.log("fixed_save_past " + MFixedSave([m], 1, buf)); }
{ const v = VLoad(bytes("in.wire"), new TableVariableReport()); v.caps = 6n; console.log("variable_save_past " + (VSave(v) === null)); }
`

func TestJsFlagsPastWidthRefuse(t *testing.T) {
	slowtest.Gate(t, "node")
	t.Parallel()
	node := jsNode(t)
	newer := unitFrom(t, strings.Replace(jsFlagsWidthReader, "Jump, Fly", "Jump, Fly, Swim", 1))
	m := tabletext.NewModel(newer)
	place := func(table, text string) *tabletext.Instance {
		inst := m.New(m.Lookup(table))
		var r tabletext.Report
		if !m.Read(inst, []byte(text), &r) {
			t.Fatalf("the oracle did not place %s: %+v", text, r)
		}
		return inst
	}
	in, past := `{"caps": ["Fly"]}`, `{"caps": ["Jump", "Swim"]}`
	dir := t.TempDir()
	write := func(name string, data []byte, err error) {
		if err != nil {
			t.Fatalf("the oracle did not write %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for name, text := range map[string]string{"in": in, "past": past} {
		wire, err := tablewire.Encode(m, place("V", text))
		write(name+".wire", wire, err)
		batch, err := tablewire.EncodeMessages(m, []*tabletext.Instance{place("M", text)})
		write(name+".batch", batch, err)
	}
	write("newer.announce", tablewire.Announce(newer), nil)
	for name, data := range generatedTree(unitFrom(t, jsFlagsWidthReader)) {
		if !strings.Contains(name, "/") {
			write(name, data, nil)
		}
	}
	write("probe.mjs", []byte(jsFlagsWidthProbe), nil)
	cmd := testproc.Command(t, node, filepath.Join(dir, "probe.mjs"))
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node probe failed: %v\n%s", err, out)
	}
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		k, _, _ := strings.Cut(line, " ")
		got[k] = line
	}
	for _, w := range []string{
		"variable_in 2",
		"variable_past true",
		"message_in 1 2",
		"message_past false out_of_range",
		"fixed_save_in true",
		"fixed_in 1 2",
		"fixed_past -1 true",
		"fixed_save_past -1",
		"variable_save_past true",
	} {
		k, _, _ := strings.Cut(w, " ")
		if got[k] != w {
			t.Errorf("got %q, want %q\n%s", got[k], w, out)
		}
	}
}
