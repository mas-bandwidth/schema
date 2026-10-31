// The integrity check (docs/SPEC-TABLES.md §2.14): a table that declares
// `integrity = "crc32c"` saves its variable-form file with a CRC-32C trailer,
// and every read checks it before decoding any field, so no single-bit flip of
// any byte of a saved file decodes as a value. A table without the key writes
// the bytes it wrote before the key existed.
package squirreltable_test

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

const integritySchema = `package probe
table Ledger | integrity = "crc32c"
{
    n int64 | min = 0, max = 9007199254740993
    label string(16)
    count uint16
}
table Plain
{
    n int64 | min = 0, max = 9007199254740993
    label string(16)
    count uint16
}
table Big | integrity = "crc32c"
{
    n int64 | min = 0, max = 9007199254740993
}
`

// integrityProbe generates the module, saves all three tables, checks clean
// round trips, then drives the damaged-file loop: every single-byte flip of
// the Ledger file, every truncation of it, a keyless file under the keyed
// reader and a trailered file under the keyless reader. Every damaged file
// must be refused by ReadStrict, every one but the form-byte flips as
// integrity_mismatch; anything else throws, which fails the run (sqrun).
const integrityProbe = `local schema = dofile("probe/schema_module.nut", true);
local T = schema.require("ProbeTables");
local L = schema.require("schema_table_load");
local S = schema.require("schema_table_save");
local R = schema.require("schema_table_refusal");

local l = T.Ledger();
l.N = 1000; l.Label = "ok"; l.Count = 7;
local p = T.Plain();
p.N = 1000; p.Label = "ok"; p.Count = 7;
local b = T.Big();
b.N = 1000;

local lb = S.save(T, "Ledger", l);
local pb = S.save(T, "Plain", p);
local bb = S.save(T, "Big", b);
if (lb == null || pb == null || bb == null) throw "a save answered null";
if (S.measure(T, "Ledger", l) != lb.len()) throw "ledger measure";
if (S.measure(T, "Plain", p) != pb.len()) throw "plain measure";
if (S.measure(T, "Big", b) != bb.len()) throw "big measure";

local hex = function(bl) { local s = ""; for (local i = 0; i < bl.len(); i++) s += format("%02x", bl[i] & 0xff); return s; };
print("ledger " + lb.len() + " " + hex(lb) + "\n");
print("plain " + pb.len() + " " + hex(pb) + "\n");
print("big " + bb.len() + " " + hex(bb) + "\n");

local mkreport = function() {
  return { unknown = 0, kindMismatch = 0, widened = 0, clamped = 0,
           duplicate = 0, malformed = false, refused = false };
};
local r = mkreport();
local l2 = L.load(T, "Ledger", lb, r);
if (l2 == null || l2.N != 1000 || l2.Label != "ok" || l2.Count != 7 || r.malformed || r.refused) throw "tolerant round trip";
local r2 = {};
local l3 = R.ReadStrict(T, "Ledger", lb, r2);
if (l3 == null || l3.N != 1000 || r2.reason != null) throw "strict round trip N=" + (l3 == null ? "-" : l3.N.tostring()) + " reason=" + r2.reason;

local damaged = 0;
local integrity = 0;
for (local i = 0; i < lb.len(); i++) {
  foreach (x in [0x01, 0x02, 0x04, 0x08, 0x10, 0x20, 0x40, 0x80, 0xff]) {
    local bad = blob(lb.len());
    for (local k = 0; k < lb.len(); k++) bad[k] = lb[k];
    bad[i] = bad[i] ^ x;
    local rr = {};
    local v = R.ReadStrict(T, "Ledger", bad, rr);
    if (v != null) throw "flip read clean: byte " + i + " ^ " + x;
    damaged++;
    if (i == 0) continue;
    if (rr.reason != "integrity_mismatch") throw "flip reason " + rr.reason + " byte " + i;
    integrity++;
  }
}
for (local n = 1; n < lb.len(); n++) {
  local bad = blob(n);
  for (local k = 0; k < n; k++) bad[k] = lb[k];
  local rr = {};
  local v = R.ReadStrict(T, "Ledger", bad, rr);
  if (v != null) throw "truncation read clean len " + n;
  damaged++;
  if (rr.reason != "integrity_mismatch") throw "truncation reason " + rr.reason + " len " + n;
  integrity++;
}
local rr = {};
local v = R.ReadStrict(T, "Ledger", pb, rr);
if (v != null) throw "a keyless file under the keyed reader read clean";
damaged++;
if (rr.reason != "integrity_mismatch") throw "keyless-under-keyed reason " + rr.reason;
integrity++;
local rr = {};
v = R.ReadStrict(T, "Plain", lb, rr);
if (v != null) throw "a trailered file under the keyless reader read clean";
damaged++;
print("damaged " + damaged + " " + integrity + "\n");
`

func TestSquirrelWireIntegrityCheck(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "probe.schema")
	writeFile(t, schemaPath, []byte(integritySchema))
	paths, err := compiler.GatherPaths([]string{schemaPath})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatalf("load probe schema: %v", err)
	}
	files, err := squirrel.Generate(u)
	if err != nil {
		t.Fatalf("the type surface refused: %v", err)
	}
	tables, err := squirreltable.Generate(u)
	if err != nil {
		t.Fatalf("the table surface refused: %v", err)
	}
	out := filepath.Join(dir, "probe")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		writeFile(t, filepath.Join(out, name), data)
	}
	for name, data := range tables {
		writeFile(t, filepath.Join(out, name), data)
	}
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(integrityProbe))

	stdout := sqrun.Run(t, dir, "main.nut", map[string][]byte{})

	rows := map[string]struct {
		measure int
		hex     string
	}{}
	damaged := ""
	for line := range strings.SplitSeq(strings.TrimRight(string(stdout), "\n"), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			t.Fatalf("probe line %q is not a row", line)
		}
		if f[0] == "damaged" {
			damaged = strings.Join(f[1:], " ")
			continue
		}
		var m int
		if _, err := fmt.Sscanf(f[1], "%d", &m); err != nil {
			t.Fatalf("%s: measure %q is not a number", f[0], f[1])
		}
		rows[f[0]] = struct {
			measure int
			hex     string
		}{measure: m, hex: f[2]}
	}
	ledger, err := hex.DecodeString(rows["ledger"].hex)
	if err != nil {
		t.Fatalf("ledger is not hex: %v", err)
	}
	plain, err := hex.DecodeString(rows["plain"].hex)
	if err != nil {
		t.Fatalf("plain is not hex: %v", err)
	}
	big, err := hex.DecodeString(rows["big"].hex)
	if err != nil {
		t.Fatalf("big is not hex: %v", err)
	}
	if rows["ledger"].measure != len(ledger) || rows["plain"].measure != len(plain) || rows["big"].measure != len(big) {
		t.Fatalf("a measure does not match its saved length")
	}

	// the oracle's bytes: internal/tablewire's save of the same value, then,
	// for a declaring table, the CRC-32C trailer of §2.14 in Go.
	model := tabletext.NewModel(u)
	keyless := func(table string, set func(*tabletext.Instance)) []byte {
		t.Helper()
		inst := model.New(u.Tables[table])
		set(inst)
		body, err := tablewire.Encode(model, inst)
		if err != nil {
			t.Fatalf("%s: schema-go encode: %v", table, err)
		}
		return body
	}
	ledgerBody := keyless("Ledger", func(v *tabletext.Instance) {
		v.Fields[0].Cell.I = 1000
		v.Fields[0].Cell.U = 1000
		v.Fields[1].Cell.Str = []byte("ok")
		v.Fields[2].Cell.U = 7
	})
	plainBody := keyless("Plain", func(v *tabletext.Instance) {
		v.Fields[0].Cell.I = 1000
		v.Fields[0].Cell.U = 1000
		v.Fields[1].Cell.Str = []byte("ok")
		v.Fields[2].Cell.U = 7
	})
	bigBody := keyless("Big", func(v *tabletext.Instance) {
		v.Fields[0].Cell.I = 1000
		v.Fields[0].Cell.U = 1000
	})
	table := crc32.MakeTable(crc32.Castagnoli)
	seal := func(body []byte) []byte {
		out := append(append([]byte(nil), body...), 0, 0, 0, 0)
		binary.LittleEndian.PutUint32(out[len(body):], crc32.Checksum(body, table))
		return out
	}
	if got, want := hex.EncodeToString(plain), hex.EncodeToString(plainBody); got != want {
		t.Fatalf("the plain table moved from schema-go's keyless save:\n got %s\nwant %s", got, want)
	}
	if got, want := hex.EncodeToString(ledger), hex.EncodeToString(seal(ledgerBody)); got != want {
		t.Fatalf("Ledger is not schema-go's save plus the CRC-32C trailer:\n got %s\nwant %s", got, want)
	}
	if got, want := hex.EncodeToString(big), hex.EncodeToString(seal(bigBody)); got != want {
		t.Fatalf("Big is not schema-go's save plus the CRC-32C trailer:\n got %s\nwant %s", got, want)
	}
	for name, w := range map[string][]byte{"Ledger": ledger, "Big": big} {
		if binary.LittleEndian.Uint32(w[len(w)-4:]) != crc32.Checksum(w[:len(w)-4], table) {
			t.Fatalf("%s trailer is not the CRC-32C of the bytes before it: %x", name, w)
		}
	}
	// every byte flipped and every truncation, plus the two cross reads
	wantDamaged := len(ledger)*9 + (len(ledger) - 1) + 2
	wantIntegrity := len(ledger)*9 - 9 + (len(ledger) - 1) + 1
	if want := fmt.Sprintf("%d %d", wantDamaged, wantIntegrity); damaged != want {
		t.Fatalf("damaged files = %q, want %q", damaged, want)
	}
	t.Logf("damaged files: %s", damaged)
}
