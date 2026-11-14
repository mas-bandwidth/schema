package squirreltable_test

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// lebTenBytes is one canonical ten-byte LEB128 (docs/SPEC-TABLES.md §3): nine
// continuation bytes and a tenth carrying the value's sixty-fourth bit.
func lebTenBytes(v uint64) []byte {
	out := make([]byte, 0, 10)
	for i := 0; i < 10; i++ {
		b := byte(v & 0x7f)
		v >>= 7
		if i < 9 {
			b |= 0x80
		}
		out = append(out, b)
	}
	return out
}

func writeUnitFiles(t *testing.T, dir string, u *ir.Unit) {
	t.Helper()
	for name, b := range squirrelMessageFiles(t, u, nil) {
		if !strings.Contains(name, "/") {
			writeFile(t, filepath.Join(dir, name), b)
		}
	}
}

// TestSquirrelAnnounceLebKeepsBit63 holds the announcement's LEB128 reader to
// the file's rule (docs/SPEC-TABLES.md §3, §3.3): canonical, at most ten
// bytes, and the tenth byte's bit kept as the value's sixty-fourth bit. A
// reference or a bound at or above 2^63 is that value and is out of range; a
// ranged base whose zigzag needs that bit is the base its declaration names.
func TestSquirrelAnnounceLebKeepsBit63(t *testing.T) {
	t.Parallel()
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}

	ranges := loadUnit(t, root, []string{"tables/examples/Ranges.schema"})
	rangesDir := t.TempDir()
	writeUnitFiles(t, rangesDir, ranges)
	rangesEntries := tablewire.MessageVocabulary(ranges)
	honest := announcementWithEntries(ranges, rangesEntries)
	hostileRef := append([]byte{honest[0]}, lebTenBytes(uint64(1)<<63|1)...)
	hostileRef = append(hostileRef, honest[2:]...)
	var slot uint64
	for i, e := range rangesEntries {
		if e.Id == ir.TableWireId("i64_low") {
			slot = uint64(i + 1)
		}
	}
	if slot == 0 {
		t.Fatal("i64_low vocabulary field missing")
	}
	batch := messageBatchScalar(ir.TableMessageRefBits(len(rangesEntries)), slot, 0, 64)
	rangesScript := fmt.Sprintf(`local I=dofile("inputs.nut",true);
local T=dofile("schema_module.nut",true).require(%q);
local v={}; local ar={};
if(!T.AnnounceRead(v,I["honest"],ar)) { print("honest:announce:"+ar.reason+"\n"); }
else {
  local out=[T.RangedSigned()];
  local rr={};
  local n=T.RangedSignedLoadMessages(out,v,I["batch"],rr);
  print("honest:"+n+":"+out[0].I64Low.tostring()+":"+rr.reason+":"+rr.bodyDamaged+"\n");
}
local r={}; local rr={};
print("ref:"+(T.AnnounceRead(r,I["ref"],rr)?"accept":"refused")+":"+rr.bodyDamaged+"\n");
`, squirreltable.ModuleName(ranges))
	writeFile(t, filepath.Join(rangesDir, "main.nut"), []byte(rangesScript))
	out := string(sqrun.Run(t, rangesDir, "main.nut", map[string][]byte{
		"honest": honest, "ref": hostileRef, "batch": batch,
	}))
	if !strings.Contains(out, "honest:1:-9223372036854775808:null:false") {
		t.Errorf("a ranged base at the sixty-fourth bit did not read: %s", out)
	}
	if !strings.Contains(out, "ref:refused") {
		t.Errorf("a reference past 2^63 was not refused: %s", out)
	}

	pack := loadUnit(t, root, []string{"tables/examples"})
	packDir := t.TempDir()
	writeUnitFiles(t, packDir, pack)
	packEntries := tablewire.MessageVocabulary(pack)
	var stringEntry ir.TableVocabularyEntry
	for _, e := range packEntries {
		if e.Kind == ir.TableKindString {
			stringEntry = e
			break
		}
	}
	if stringEntry.Id == 0 {
		t.Fatal("no string entry in the message vocabulary")
	}
	tooWide := stringEntry
	wide := uint64(1)<<63 | 8
	tooWide.Shape = ir.TableMessageShape{Max: int64(wide)}
	hostileMax := announcementWithChangedEntry(pack, stringEntry, tooWide)
	packScript := fmt.Sprintf(`local I=dofile("inputs.nut",true);
local T=dofile("schema_module.nut",true).require(%q);
local v={}; local ar={};
print("max:"+(T.AnnounceRead(v,I["max"],ar)?"accept":"refused")+":"+ar.bodyDamaged+"\n");
`, squirreltable.ModuleName(pack))
	writeFile(t, filepath.Join(packDir, "main.nut"), []byte(packScript))
	out = string(sqrun.Run(t, packDir, "main.nut", map[string][]byte{"max": hostileMax}))
	if !strings.Contains(out, "max:refused") {
		t.Errorf("a string max past 2^63 was not refused: %s", out)
	}
}
