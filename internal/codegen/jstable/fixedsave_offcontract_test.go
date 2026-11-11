package jstable

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestJsFixedSaveRefusesOffContract holds the fixed-form writer to docs/SPEC.md
// §5.1 rows 1 to 8: every value a field's declaration does not admit is refused
// with -1 before the first byte is written, and the caller's buffer is left
// untouched. The storage rows — a counted array's count and a text's used
// length — are checked for every record before the template is laid down, so no
// store can run past the storage it describes. JavaScript is untyped, so a
// value of the wrong storage type is a refusal too, never a thrown TypeError.
//
// The boundary values themselves must be written and round-trip through the
// module's own reader: a writer that refused everything would fail here.
func TestJsFixedSaveRefusesOffContract(t *testing.T) {
	t.Parallel()
	node := jsNode(t)
	u := unitFrom(t, jsFixedSaveOffContractSchema)
	files := generatedTree(u)
	dir := t.TempDir()
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "probe.mjs"), []byte(jsFixedSaveOffContractProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, filepath.Join(dir, "probe.mjs"))
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	var fails []string
	for line := range strings.SplitSeq(string(out), "\n") {
		if strings.HasPrefix(line, "FAIL: ") {
			fails = append(fails, line)
		}
	}
	if len(fails) != 0 || err != nil || strings.TrimSpace(string(out)) != "ok" {
		t.Fatalf("the fixed writer did not hold rows 1-8:\n%s", out)
	}
}

const jsFixedSaveOffContractSchema = `package probe

enum Kind
{
    Alpha, Beta, Gamma
}

type Inner
{
    level int32 | min = 0, max = 10
    flag bits(3)
}

union Pick
{
    number int32 | min = 0, max = 10
    label string(8)
}

fixed table Row
{
    value int32 = 1 | min = 1, max = 100
    full int32 | min = -2147483648, max = 2147483647
    full64 int64 | min = -9223372036854775808, max = 9223372036854775807
    mask bits(5)
    mask32 bits(32)
    mask64 bits(64)
    kind Kind
    pick Pick
    items [..4]int32 | min = -5, max = 5
    name string(8)
    data bytes(8)
    wide uint64
    inner Inner
}
`

const jsFixedSaveOffContractProbe = `import {
  Row, InitRow, RowFixedSave, RowFixedLoad, RowFixedNewPlan, RowFixedMeasure,
  TableFixedReport,
} from "./ProbeTable.js";

let failures = 0;
function fail(why) { console.error("FAIL: " + why); failures++; }

function fresh() {
  const v = new Row();
  InitRow(v);
  v.value = 1;
  return v;
}

function refuses(label, mutate) {
  const v = fresh();
  mutate(v);
  const need = RowFixedMeasure(1);
  const bytes = new Uint8Array(need + 64);
  bytes.fill(0xa5);
  let got;
  try {
    got = RowFixedSave([v], 1, bytes);
  } catch (e) {
    fail(label + " threw " + e.name + " instead of answering -1");
    return;
  }
  if (got !== -1) { fail(label + " was written (" + got + ")"); return; }
  for (let i = 0; i < bytes.length; i++) {
    if (bytes[i] !== 0xa5) { fail(label + " changed the buffer at byte " + i); return; }
  }
}

function roundTrips(label, mutate, check) {
  const v = fresh();
  mutate(v);
  const need = RowFixedMeasure(1);
  const bytes = new Uint8Array(need);
  const got = RowFixedSave([v], 1, bytes);
  if (got !== need) { fail(label + " boundary was refused (" + got + ")"); return; }
  const back = [fresh()];
  const report = new TableFixedReport();
  const n = RowFixedLoad(back, 1, bytes, bytes.length, RowFixedNewPlan(4096, 4096), report);
  if (n !== 1) { fail(label + " boundary did not load (n=" + n + ", refused=" + report.refused + ")"); return; }
  const again = new Uint8Array(need);
  if (RowFixedSave(back, 1, again) !== need) { fail(label + " boundary did not write back"); return; }
  for (let i = 0; i < need; i++) {
    if (bytes[i] !== again[i]) { fail(label + " write-read-write differs at byte " + i); return; }
  }
  check(back[0]);
}

// rows 1 and 2: a ranged integer off either end, a non-integer, a bits value
// wider than its width.
refuses("value below its minimum", (v) => { v.value = 0; });
refuses("value above its maximum", (v) => { v.value = 101; });
refuses("non-integer in an integer field", (v) => { v.value = 1.5; });
refuses("bits value wider than its width", (v) => { v.mask = 32; });

// a declared range that reaches the storage width's own ends is still a range:
// a value one past either end is off contract even though no loaded value
// could fail the bound.
refuses("full-width value above its maximum", (v) => { v.full = 2147483648; });
refuses("full-width value below its minimum", (v) => { v.full = -2147483649; });
refuses("full-width 64-bit value above its maximum", (v) => { v.full64 = 9223372036854775808n; });
refuses("full-width 64-bit value below its minimum", (v) => { v.full64 = -9223372036854775809n; });
refuses("bits(32) value at 2^N", (v) => { v.mask32 = 4294967296; });
refuses("bits(32) negative", (v) => { v.mask32 = -1; });
refuses("bits(64) value at 2^N", (v) => { v.mask64 = 18446744073709551616n; });
refuses("bits(64) negative", (v) => { v.mask64 = -1n; });

// row 5: an enum ordinal past the last variant.
refuses("enum ordinal past the last variant", (v) => { v.kind = 4; });

// row 6: a union tag outside the arm set.
refuses("unknown union tag", (v) => { v.pick.type = 3; });

// row 3: a counted array's count past Max or negative, and the wrong type.
refuses("count past Max", (v) => { v.itemsCount = 5; });
refuses("negative count", (v) => { v.itemsCount = -1; });
refuses("non-array where an array belongs", (v) => { v.items = 7; });

// row 4: a text or byte buffer length past N or negative, and a buffer whose
// own storage is shorter than the used length it names.
refuses("string length past N", (v) => { v.nameLength = 9; });
refuses("negative string length", (v) => { v.nameLength = -1; });
refuses("string buffer shorter than its length", (v) => {
  const short = new Uint8Array(3);
  short.set([0x61, 0x62, 0x63]);
  v.name = short;
  v.nameLength = 5;
});
refuses("bytes length past N", (v) => { v.dataLength = 9; });
refuses("negative bytes length", (v) => { v.dataLength = -1; });
refuses("bytes buffer shorter than its length", (v) => {
  v.data = new Uint8Array(3);
  v.dataLength = 5;
});

// row 8: ill-formed text — a lone surrogate's bytes and a zero byte.
refuses("lone surrogate in text", (v) => { v.name.set([0xed, 0xa0, 0x80]); v.nameLength = 3; });
refuses("zero byte in text", (v) => { v.name.set([0x61, 0x00, 0x62]); v.nameLength = 3; });

// the storage type of a BigInt field, and the nested record's own rows.
refuses("Number where a BigInt is stored", (v) => { v.wide = 5; });
refuses("nested ranged value above its maximum", (v) => { v.inner.level = 11; });
refuses("nested bits value wider than its width", (v) => { v.inner.flag = 8; });

// THE LAWFUL TWINS AT THE SAME BOUNDARIES: each is written and read back.
roundTrips("ranged minimum and zero slack", (v) => { v.value = 1; }, (v) => {
  if (v.value !== 1) fail("ranged minimum did not load");
});
roundTrips("ranged maximum, bits maximum, enum top", (v) => {
  v.value = 100;
  v.mask = 31;
  v.kind = 3;
}, (v) => {
  if (v.value !== 100 || v.mask !== 31 || v.kind !== 3) fail("the scalar boundaries did not load");
});
roundTrips("full-width maxima and bits tops", (v) => {
  v.full = 2147483647;
  v.full64 = 9223372036854775807n;
  v.mask32 = 4294967295;
  v.mask64 = 18446744073709551615n;
}, (v) => {
  if (v.full !== 2147483647 || v.full64 !== 9223372036854775807n) fail("the full-width maxima did not load");
  if (v.mask32 !== 4294967295 || v.mask64 !== 18446744073709551615n) fail("the bits tops did not load");
});
roundTrips("full-width minima", (v) => {
  v.full = -2147483648;
  v.full64 = -9223372036854775808n;
}, (v) => {
  if (v.full !== -2147483648 || v.full64 !== -9223372036854775808n) fail("the full-width minima did not load");
});
roundTrips("union top arm and its text", (v) => {
  v.pick.type = 2;
  v.pick.label.set([0x61, 0x62, 0x63]);
  v.pick.labelLength = 3;
}, (v) => {
  if (v.pick.type !== 2 || v.pick.labelLength !== 3) fail("the union boundary did not load");
});
roundTrips("counted array at Max", (v) => {
  v.itemsCount = 4;
  for (let i = 0; i < 4; i++) { v.items[i] = i === 0 ? -5 : 5; }
}, (v) => {
  if (v.itemsCount !== 4 || v.items[0] !== -5 || v.items[3] !== 5) fail("the array boundary did not load");
});
roundTrips("string at N and bytes at N", (v) => {
  v.name.set([0x61, 0x62, 0x63, 0x64, 0x65, 0x66, 0x67, 0x68]);
  v.nameLength = 8;
  v.data.set([1, 2, 3, 4, 5, 6, 7, 8]);
  v.dataLength = 8;
}, (v) => {
  if (v.nameLength !== 8 || v.dataLength !== 8) fail("the text boundaries did not load");
  if (v.name[7] !== 0x68 || v.data[7] !== 8) fail("the text boundary bytes did not load");
});
roundTrips("BigInt field and nested boundaries", (v) => {
  v.wide = 5n;
  v.inner.level = 10;
  v.inner.flag = 7;
}, (v) => {
  if (v.wide !== 5n || v.inner.level !== 10 || v.inner.flag !== 7) fail("the wide and nested boundaries did not load");
});

if (failures !== 0) { process.exit(1); }
console.log("ok");
`
