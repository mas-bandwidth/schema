package luatable

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// The sender adds unused fields before the shared field. This changes both
// its slot and the reference width without changing the shared payload shape.
// The wire comes from the Go engine, not the Lua writer under test.
func TestLuaMessageCrossBuildSlots(t *testing.T) {
	t.Parallel()
	load := func(name, schema string) *ir.Unit {
		path := filepath.Join(t.TempDir(), name+".schema")
		writeFile(t, path, schema)
		return luarun.LoadUnit(t, path)
	}
	sender := load("Sender", `package sender
fixed table Root {
 a uint32
 b uint32
 c uint32
 d uint32
 e uint32
 f uint32
 g uint32
 h uint32
 i uint32
 j uint32
 k uint32
 z uint32
}
`)
	receiver := load("Receiver", "package receiver\nfixed table Root { z uint32 }\n")
	if ir.TableMessageRefBits(len(ir.TableVocabulary(sender))) == ir.TableMessageRefBits(len(ir.TableVocabulary(receiver))) {
		t.Fatal("fixture does not change reference width")
	}
	model := tabletext.NewModel(sender)
	inst := model.New(model.Lookup("Root"))
	var report tabletext.Report
	if !model.Read(inst, []byte(`{"z":123456789}`), &report) || !report.Silent() {
		t.Fatalf("source: %+v", report)
	}
	batch, err := tablewire.EncodeMessages(model, []*tabletext.Instance{inst, inst})
	if err != nil {
		t.Fatal(err)
	}
	files, err := Generate(receiver)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	writeFile(t, filepath.Join(dir, "main.lua"), fmt.Sprintf(`local T = require(%q)
local inputs = require("inputs")
local vocab, report = {}, {}
assert(T.AnnounceRead(vocab, inputs.announce, report))
assert(vocab.count == %d, "receiver substituted its own vocabulary")
assert(vocab.refBits == %d, "receiver substituted its own reference width")
local values = T.LoadMessages("Root", vocab, inputs.batch, report)
assert(not report.malformed and values and #values == 2, "cross-build batch damaged")
assert(values[1].z == 123456789 and values[2].z == 123456789, "cross-build field lost")
`, ModuleName(receiver), len(ir.TableVocabulary(sender)), ir.TableMessageRefBits(len(ir.TableVocabulary(sender)))))
	luarun.Run(t, dir, "main.lua", map[string][]byte{"announce": ir.TableAnnouncement(sender), "batch": batch})
}
