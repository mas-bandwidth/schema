package squirreltable_test

// A FIXED BOUND GROWN SINCE THE WRITER, IN THE MESSAGE FORM (docs/SPEC-TABLES.md
// §21.1, "a grown bound"; §3.3): `[N]T` sent by a peer whose bound was smaller
// reads as the peer's elements followed by ONE DEFAULT ELEMENT PER NEW SLOT —
// an empty union, a record at its declared defaults, a null enum, a zero
// scalar — a fresh element per slot, so the loaded value measures and saves
// again. The reader once filled the new slots with the element descriptor's
// def, null for a union and a record, and MeasureMessages refused the value
// it had just loaded (conformance row widenbound_msg).

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
)

const widenBoundReaderSchema = `package probe

enum Mode { Read, Write }

type Spot
{
    x uint16 = 5
}

fixed table Inner
{
    at Spot
    n  uint32 = 7
}

union Pick
{
    inner Inner
    tally uint32
    spans [3]int32
}

fixed table Msg
{
    seq    uint32
    picks  [3]Pick
    inners [3]Inner
    counts [3]uint32
    modes  ?[3]Mode
    body   Pick
}
`

// the older peer: every bound above narrowed from 3 to 1, nothing else
var widenBoundWriterSchema = strings.ReplaceAll(widenBoundReaderSchema, "[3]", "[1]")

func TestSquirrelMessageGrownBoundBornsOneDefaultPerSlot(t *testing.T) {
	sqrun.Require(t)
	t.Parallel()
	dir := t.TempDir()
	modW := emitUnit(t, dir, "w", unitFromSchema(t, widenBoundWriterSchema))
	modR := emitUnit(t, dir, "r", unitFromSchema(t, widenBoundReaderSchema))
	script := fmt.Sprintf(`local W = dofile("w/schema_module.nut", true).require(%q);
local R = dofile("r/schema_module.nut", true).require(%q);
local v = W.Msg();
v.Seq = 9;
v.Picks[0].Type = 2; v.Picks[0].Tally = 4;
v.Inners[0].N = 11;
v.Counts[0] = 6;
v.ModesPresent = true; v.Modes[0] = "Write";
v.Body.Type = 3; v.Body.Spans[0] = -3;
local b = blob(256);
local n = W.MsgSaveMessages([v], b, {});
if (n < 0) throw "the peer did not write";
local c = blob(n);
for (local i = 0; i < n; i++) c[i] = b[i];
local voc = {};
local ar = {};
if (!R.AnnounceRead(voc, W.TableMessageAnnouncement, ar)) throw "announce: " + ar.reason;
local storage = [R.Msg()];
local rr = {};
local k = R.MsgLoadMessages(storage, voc, c, rr);
print("load " + k + " " + (("refused" in rr) && rr.refused ? "refused" : "ok") + "\n");
if (k != 1) return;
local m = storage[0];
local show = function(x) { return x == null ? "null" : x.tostring(); };
print("picks " + m.Picks.len() + " " + m.Picks[0].Type + " " + m.Picks[0].Tally + " " + m.Picks[1].Type + " " + m.Picks[2].Type + " " + (m.Picks[1] == m.Picks[2] ? "shared" : "fresh") + "\n");
print("inners " + m.Inners.len() + " " + m.Inners[0].N + " " + m.Inners[1].N + " " + m.Inners[1].At.X + " " + m.Inners[2].N + " " + (m.Inners[1] == m.Inners[2] ? "shared" : "fresh") + "\n");
print("counts " + m.Counts.len() + " " + m.Counts[0] + " " + show(m.Counts[1]) + " " + show(m.Counts[2]) + "\n");
print("modes " + m.Modes.len() + " " + show(m.Modes[0]) + " " + show(m.Modes[1]) + " " + show(m.Modes[2]) + "\n");
print("spans " + m.Body.Spans.len() + " " + m.Body.Spans[0] + " " + show(m.Body.Spans[1]) + " " + show(m.Body.Spans[2]) + "\n");
local size = R.MsgMeasureMessages([m], {});
print("measure " + (size < 0 ? "refused" : "ok") + "\n");
if (size < 0) return;
local out = blob(size);
print("save " + (R.MsgSaveMessages([m], out, {}) == size ? "ok" : "short") + "\n");
`, modW, modR)
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(script))
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimRight(string(sqrun.Run(t, dir, "main.nut", nil)), "\n"), "\n") {
		if k, v, ok := strings.Cut(line, " "); ok {
			got[k] = v
		}
	}
	for key, want := range map[string]string{
		"load":    "1 ok",
		"picks":   "3 2 4 0 0 fresh",
		"inners":  "3 11 7 5 7 fresh",
		"counts":  "3 6 0 0",
		"modes":   "3 Write null null",
		"spans":   "3 -3 0 0",
		"measure": "ok",
		"save":    "ok",
	} {
		if got[key] != want {
			t.Errorf("%s: %q, want %q (docs/SPEC-TABLES.md §21.1, one default element per grown slot)", key, got[key], want)
		}
	}
}
