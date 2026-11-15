package squirreltable_test

// A FLAGS MASK RIDES THE MESSAGE FORM, default or not, and its bytes are the
// Go engine's (docs/SPEC-TABLES.md §3.3, tablewire.EncodeMessage and
// DecodeMessages). Each case carries a non-default mask: a LoadoutConfig whose
// Perks is a value a default-only batch never writes, and the three conformance
// instances whose flags the corpus pins (`root_full.bin`, `archive.bin`,
// `message_arms.bin`). The Go subtest first proves the engine writes and reads
// the same mask; the Squirrel subtest proves the leg writes the engine's bytes
// and reads them back to the engine's value.

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

type sfrCase struct {
	name    string
	unitKey string
	paths   []string
	root    string
	wire    string
	perks   bool
}

func sfrFlagsField(f *ir.Field) bool {
	if f.Type.Kind != ir.TNamed {
		return false
	}
	_, ok := f.Type.Ref.(*ir.Flags)
	return ok
}

// sfrFlagValues collects every non-zero flags mask by its path, so a read that
// loses one leaves its key out and the comparison says which.
func sfrFlagValues(inst *tabletext.Instance) map[string]uint64 {
	out := map[string]uint64{}
	var walk func(*tabletext.Instance, string)
	walk = func(in *tabletext.Instance, path string) {
		for i := range in.Fields {
			f := &in.Fields[i]
			p := path + "." + f.Def.Name
			if sfrFlagsField(f.Def) {
				if f.Def.Array != ir.ArrayNone {
					for j := range f.Elems {
						if f.Elems[j].U != 0 {
							out[fmt.Sprintf("%s[%d]", p, j)] = f.Elems[j].U
						}
					}
				} else if f.Cell.U != 0 {
					out[p] = f.Cell.U
				}
			}
			if f.Cell.Arm != nil && sfrFlagsField(f.Cell.Arm.Def) {
				out[p+".<"+f.Cell.Arm.Def.Name+">"] = f.Cell.U
			}
			if f.Cell.Tab != nil {
				walk(f.Cell.Tab, p)
			}
			if f.Cell.Node != nil {
				walk(f.Cell.Node, p)
			}
			for j := range f.Elems {
				at := fmt.Sprintf("%s[%d]", p, j)
				if f.Elems[j].Tab != nil {
					walk(f.Elems[j].Tab, at)
				}
				if f.Elems[j].Arm != nil && sfrFlagsField(f.Elems[j].Arm.Def) {
					out[at+".<"+f.Elems[j].Arm.Def.Name+">"] = f.Elems[j].U
				}
				if f.Elems[j].Node != nil {
					walk(f.Elems[j].Node, at)
				}
			}
		}
	}
	walk(inst, inst.Def.Name)
	return out
}

func TestSquirrelMessageFlagsRoundTrip(t *testing.T) {
	t.Parallel()
	sqrun.Require(t)
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	cases := []sfrCase{
		{name: "loadout", unitKey: "tabledemo", paths: []string{"tables/examples"}, root: "LoadoutConfig", perks: true},
		{name: "root_full", unitKey: "tabledemo", paths: []string{"tables/examples"}, root: "RootConfig", wire: "testdata/conformance/tables/wire/root_full.bin"},
		{name: "archive", unitKey: "tabledemo", paths: []string{"tables/examples"}, root: "ArchiveConfig", wire: "testdata/conformance/tables/wire/archive.bin"},
		{name: "message_arms", unitKey: "messagedemo", paths: []string{"tables/messages"}, root: "ToolMessage", wire: "testdata/conformance/tables/wire/message_arms.bin"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			u := loadUnit(t, root, c.paths)
			model := tabletext.NewModel(u)
			st := model.Lookup(c.root)
			if st == nil {
				t.Fatalf("%s declares no table %s", c.unitKey, c.root)
			}
			value := model.New(st)
			var wire []byte
			if c.perks {
				nfField(t, value, "perks").Cell.U = 1
			} else {
				wire = mustRead(t, filepath.Join(root, c.wire))
				var report tabletext.Report
				ok, err := tablewire.Decode(model, value, wire, &report)
				if err != nil || !ok || report.Refused {
					t.Fatalf("%s: the engine refused its own wire: %v %v %q", c.name, ok, err, report.Reason)
				}
			}
			before := sfrFlagValues(value)
			if len(before) == 0 {
				t.Fatalf("%s carries no non-default flags mask", c.name)
			}

			// (Go) the engine writes the value and reads it back.
			batch, err := tablewire.EncodeMessage(model, value)
			if err != nil {
				t.Fatalf("%s: EncodeMessage refused: %v", c.name, err)
			}
			var vocab tablewire.Vocabulary
			var announced tabletext.Report
			if err := vocab.AnnounceRead(tablewire.Announce(u), &announced); err != nil {
				t.Fatalf("%s: the announcement refused: %v", c.name, err)
			}
			engineRead := model.New(st)
			var readReport tabletext.Report
			n, ok, err := tablewire.DecodeMessages(model, []*tabletext.Instance{engineRead}, batch, &vocab, &readReport)
			if err != nil || !ok || n != 1 || readReport.Refused {
				t.Fatalf("%s: DecodeMessages answered %d %v %v %q", c.name, n, ok, err, readReport.Reason)
			}
			if after := sfrFlagValues(engineRead); !maps.Equal(before, after) {
				t.Fatalf("%s: the engine's round trip moved the mask: %v -> %v", c.name, before, after)
			}
			if again, err := tablewire.EncodeMessage(model, engineRead); err != nil || !bytes.Equal(again, batch) {
				t.Fatalf("%s: the engine's round trip moved the value: %v", c.name, err)
			}

			// (Squirrel) SaveMessages writes the engine's bytes, and
			// LoadMessages reads the engine's batch to the same value.
			dir := t.TempDir()
			sub := filepath.Join(dir, c.unitKey)
			if err := os.MkdirAll(sub, 0o755); err != nil {
				t.Fatal(err)
			}
			for name, body := range squirrelMessageFiles(t, u, nil) {
				if !strings.Contains(name, "/") {
					writeFile(t, filepath.Join(sub, name), body)
				}
			}
			module := squirreltable.ModuleName(u)
			inputs := map[string][]byte{"go": batch}
			if wire != nil {
				inputs["wire"] = wire
			}
			var script strings.Builder
			script.WriteString(`local I=dofile("inputs.nut",true);
local hex=function(b,n){local s="";for(local i=0;i<n;i++)s+=format("%02x",b[i]&0xff);return s;};
local S=dofile("` + c.unitKey + `/schema_module.nut",true);
local T=S.require(` + fmt.Sprintf("%q", module) + `);
local L=S.require("schema_table_load");
`)
			if c.perks {
				fmt.Fprintf(&script, "local x=T.%s(); x.%s = 1;\n", c.root, squirrel.Member(nfField(t, value, "perks").Def))
			} else {
				script.WriteString(`local lr={unknown=0,mismatch=0,widened=0,oor=0,repeated=0,bodyDamaged=false,refused=false};
local x=L.load(T, "` + c.root + `", I["wire"], lr);
if(x==null || lr.refused) throw "strict load refused: "+lr.reason;
`)
			}
			script.WriteString(`local mr={};
local size=T.` + c.root + `MeasureMessages([x],mr);
if(size<0) throw "measure refused";
local b=blob(size);
local sr={};
local w=T.` + c.root + `SaveMessages([x],b,sr);
if(w<0) throw "save refused: "+sr.reason;
print("save "+hex(b,w)+"\n");
local v={}; local ar={};
if(!T.AnnounceRead(v,T.TableMessageAnnouncement,ar)) throw "announce refused: "+ar.reason;
local out=[T.` + c.root + `()];
local rr={};
local got=T.` + c.root + `LoadMessages(out,v,I["go"],rr);
if(got<0) throw "load refused: "+rr.reason;
local mr2={};
local size2=T.` + c.root + `MeasureMessages(out,mr2);
local b2=blob(size2);
local sr2={};
local w2=T.` + c.root + `SaveMessages(out,b2,sr2);
if(w2<0) throw "load save refused: "+sr2.reason;
print("load "+hex(b2,w2)+"\n");
`)
			writeFile(t, filepath.Join(dir, "main.nut"), []byte(script.String()))
			out := map[string]string{}
			for line := range strings.SplitSeq(strings.TrimRight(string(sqrun.Run(t, dir, "main.nut", inputs)), "\n"), "\n") {
				key, rest, _ := strings.Cut(line, " ")
				out[key] = rest
			}
			for _, key := range []string{"save", "load"} {
				encoded := out[key]
				if encoded == "" {
					t.Fatalf("%s: the leg printed no %s batch", c.name, key)
				}
				legBatch, err := hex.DecodeString(encoded)
				if err != nil {
					t.Fatalf("%s: the leg's %s batch is not hex: %v", c.name, key, err)
				}
				fromLeg := model.New(st)
				var legReport tabletext.Report
				n, ok, err := tablewire.DecodeMessages(model, []*tabletext.Instance{fromLeg}, legBatch, &vocab, &legReport)
				if err != nil || !ok || n != 1 || legReport.Refused {
					t.Fatalf("%s: the engine refused the leg's %s batch: %d %v %v %q", c.name, key, n, ok, err, legReport.Reason)
				}
				if after := sfrFlagValues(fromLeg); !maps.Equal(before, after) {
					t.Errorf("%s: the leg's %s batch carries %v, the engine's value is %v", c.name, key, after, before)
				}
				if again, err := tablewire.EncodeMessage(model, fromLeg); err != nil || !bytes.Equal(again, batch) {
					t.Errorf("%s: the leg's %s batch does not decode to the engine's value: %v", c.name, key, err)
				}
			}
		})
	}
}
