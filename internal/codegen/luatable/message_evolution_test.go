package luatable

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

func TestLuaMessageAnnouncedShapes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, sender, receiver, json, check string }{
		{"zero_base", "x int32 | min = 0, max = 7", "x int32 | min = 0, max = 1000", `{"x":5,"tail":17}`, `assert(v.x == 5 and r.clamped == 0)`},
		{"signed_base", "x int32 = -10 | min = -10, max = -3", "x int32 = -10 | min = -100, max = 100", `{"x":-4,"tail":17}`, `assert(v.x == -4 and r.clamped == 0)`},
		{"clamp", "x uint32 | min = 0, max = 255", "x uint32 | min = 0, max = 7", `{"x":42,"tail":17}`, `assert(v.x == 7 and r.clamped == 1)`},
		{"widen", "x uint8", "x uint32", `{"x":42,"tail":17}`, `assert(v.x == 42 and r.widened == 1)`},
		{"unknown", "x uint32", "", `{"x":42,"tail":17}`, `assert(r.unknown == 1)`},
		{"mismatch", "x uint32", "x bool", `{"x":42,"tail":17}`, `assert(v.x == false and r.kindMismatch == 1)`},
		{"text_bound", "x string(32)", "x string(8)", `{"x":"abcdeféz","tail":17}`, `assert(v.x == "abcdefé" and r.clamped == 1)`},
		{"unknown_array", "x [..16]uint32", "", `{"x":[1,2,3],"tail":17}`, `assert(r.unknown == 1)`},
		{"unknown_text", "x string(32)", "", `{"x":"hello","tail":17}`, `assert(r.unknown == 1)`},
		{"unknown_wide", "x uint128", "", `{"x":18446744073709551618,"tail":17}`, `assert(r.unknown == 1)`},
		{"signed_widen", "x int8", "x int64", `{"x":-42,"tail":17}`, `assert(v.x == -42 and r.widened == 1)`},
		{"quantized", "x float32 | min = -1, max = 1, resolution = 0.01", "x float32", `{"x":0.5,"tail":17}`, `assert(math.abs(v.x - 0.5) < 0.00001)`},
		{"array_bound", "x [..16]uint32", "x [..2]uint32", `{"x":[1,2,3],"tail":17}`, `assert(#v.x == 2 and v.x[2] == 2 and r.clamped == 1)`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			senderPath := filepath.Join(dir, "Sender.schema")
			writeFile(t, senderPath, "package sender\nfixed table Root {\n"+tc.sender+"\ntail uint32\n}\n")
			sender := luarun.LoadUnit(t, senderPath)
			model := tabletext.NewModel(sender)
			inst := model.New(model.Lookup("Root"))
			var r tabletext.Report
			if !model.Read(inst, []byte(tc.json), &r) || !r.Silent() {
				t.Fatalf("source: %+v", r)
			}
			batch, err := tablewire.EncodeMessages(model, []*tabletext.Instance{inst, inst})
			if err != nil {
				t.Fatal(err)
			}
			receiverPath := filepath.Join(dir, "Receiver.schema")
			writeFile(t, receiverPath, "package receiver\nfixed table Root {\n"+tc.receiver+"\ntail uint32\n}\n")
			receiver := luarun.LoadUnit(t, receiverPath)
			files, err := Generate(receiver)
			if err != nil {
				t.Fatal(err)
			}
			probe := fmt.Sprintf(`local T = require(%q)
local inputs = require("inputs")
local vocab = {}
assert(T.AnnounceRead(vocab, inputs.announce, {}))
local r = { unknown=0, kindMismatch=0, clamped=0, widened=0 }
local values = T.LoadMessagesTolerant("Root", vocab, inputs.batch, r)
assert(values and #values == 2 and not r.malformed, "announced shape damaged the batch")
-- Counters aggregate the two identical bodies; check the per-body count below.
for _, k in ipairs({"unknown","kindMismatch","clamped","widened"}) do r[k] = r[k] / 2 end
for _, v in ipairs(values) do
assert(v.tail == 17, "payload width displaced the following field")
%s
end
`, ModuleName(receiver), tc.check)
			out, stderr, err := try(t, files, probe, map[string][]byte{"announce": tablewire.Announce(sender), "batch": batch})
			if err != nil {
				t.Fatalf("%v\n%s\n%s", err, stderr, out)
			}
		})
	}
}
