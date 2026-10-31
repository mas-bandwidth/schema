// The integrity trailer on the fixed form (docs/SPEC-TABLES.md §2.14, §3.4
// THE FRAMING): a `fixed table` that declares `integrity = "crc32c"` saves its
// form-3 file with one CRC-32C trailer after the last record, and FixedLoad
// checks it after the form byte and before the layout or any record, so no
// single-bit flip of any byte of a saved file lands a value. A fixed table
// without the key writes the bytes it wrote before the key existed. The
// expected bytes and verdicts are schema-go's own: the same unit's Go
// fixed-form surface's goldens (TestGoWireIntegrityFixedForm), pinned here.
package squirreltable_test

import (
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// squirrelFixedIntegritySchema is the oracle's schema (TestGoWireIntegrityFixedForm),
// with the `integrity` key stripped so the shared checker (which still refuses
// it on a fixed table at this base) accepts it; the test sets it on the IR
// before generating, which is the state the emitter reads.
const squirrelFixedIntegritySchema = `package probe
fixed table Reading
{
    x int32
    y int16
    label string(8)
}
fixed table PlainReading
{
    x int32
    y int16
    label string(8)
}
`

// integrityFixedPlainGolden is PlainReadingFixedSave of the three records of
// fixedRecords for the keyless table: the schema-go oracle's file, byte for byte.
const integrityFixedPlainGolden = "030000000000000064f3fa8daeaf844448000000040000000deeaf527dc1c0970d1200000003000000071702864cf563af040400000000000000541502864cf463af0302000000000000003d62cb8fecfcf7390c0c0000000000000064f3fa8daeaf8444e80300000000020000006f6b00000000000064f3fa8daeaf8444e9030000f9ff00000000000000000000000064f3fa8daeaf8444ea030000f2ff080000006569676874206368"

// integrityFixedGolden is ReadingFixedSave of the same three records: the keyed
// table's form-3 file, then the CRC-32C of every byte before it, little-endian.
const integrityFixedGolden = "03000000000000006938893b0968270148000000040000007b8c6cd3fc19b9b80d1200000003000000071702864cf563af040400000000000000541502864cf463af0302000000000000003d62cb8fecfcf7390c0c000000000000006938893b09682701e80300000000020000006f6b0000000000006938893b09682701e9030000f9ff0000000000000000000000006938893b09682701ea030000f2ff0800000065696768742063682854cdb3"

const squirrelFixedIntegrityProbe = `dofile("inputs.nut", true);
local S = dofile("probe/schema_module.nut", true); local T = S.require("ProbeTables");

local words = ["ok", "", "eight ch"];

local function reading(k) {
  local r = T.Reading();
  r.X = 1000 + k;
  r.Y = -7 * k;
  r.Label = words[k];
  return r;
}

local function plain(k) {
  local p = T.PlainReading();
  p.X = 1000 + k;
  p.Y = -7 * k;
  p.Label = words[k];
  return p;
}

local recs = []; local pls = [];
for (local k = 0; k < 3; k++) { recs.append(reading(k)); pls.append(plain(k)); }

local keyed = blob(256); local n = T.ReadingFixedSave(keyed, recs);
local keyedB = blob(n); for (local i = 0; i < n; i++) keyedB[i] = keyed[i];
::hexline(keyedB);

local keyless = blob(256); local m = T.PlainReadingFixedSave(keyless, pls);
local keylessB = blob(m); for (local i = 0; i < m; i++) keylessB[i] = keyless[i];
::hexline(keylessB);

local back = []; for (local k = 0; k < 3; k++) back.append(T.Reading());
local got = T.ReadingFixedLoadTolerant(keyedB, back, null);
print("round " + got + ":" + T.FixedReason + "\n");

local flips = [0x01, 0x02, 0x04, 0x08, 0x10, 0x20, 0x40, 0x80, 0xff];
for (local i = 0; i < n; i++) {
  foreach (x in flips) {
    local bad = blob(n); for (local j = 0; j < n; j++) bad[j] = keyedB[j];
    bad[i] = bad[i] ^ x;
    local v = []; for (local k = 0; k < 3; k++) v.append(T.Reading());
    local c = T.ReadingFixedLoadTolerant(bad, v, null);
    print("flip " + i + " " + x + " " + (c >= 0 ? "read" : T.FixedReason) + "\n");
  }
}

local body = blob(n - 4); for (local j = 0; j < n - 4; j++) body[j] = keyedB[j];
local bv = []; for (local k = 0; k < 3; k++) bv.append(T.Reading());
local bc = T.ReadingFixedLoadTolerant(body, bv, null);
print("notrailer " + (bc >= 0 ? "read" : T.FixedReason) + "\n");

for (local t = 1; t < n; t++) {
  local cut = blob(t); for (local j = 0; j < t; j++) cut[j] = keyedB[j];
  local v = []; for (local k = 0; k < 3; k++) v.append(T.Reading());
  local c = T.ReadingFixedLoadTolerant(cut, v, null);
  print("trunc " + t + " " + (c >= 0 ? "read" : T.FixedReason) + "\n");
}
`

func loadSquirrelUnit(t *testing.T, src string) *ir.Unit {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "Probe.schema")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestSquirrelWireIntegrityFixedForm(t *testing.T) {
	t.Parallel()
	u := loadSquirrelUnit(t, squirrelFixedIntegritySchema)
	u.Tables["Reading"].Integrity = "crc32c"
	dir := t.TempDir()
	files, err := squirrel.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := squirreltable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	if err := os.MkdirAll(filepath.Join(dir, "probe"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, "probe", name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	main := filepath.Join(dir, "main.nut")
	if err := os.WriteFile(main, []byte(squirrelFixedIntegrityProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	out := string(sqrun.Run(t, dir, "main.nut", nil))

	var keyedHex, keylessHex string
	flips := map[string]string{}
	truncs := map[string]string{}
	noTrailer, round := "", ""
	for line := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		switch f[0] {
		case "round":
			round = strings.SplitN(f[1], ":", 2)[0]
		case "flip":
			flips[f[1]+" "+f[2]] = f[3]
		case "trunc":
			truncs[f[1]] = f[2]
		case "notrailer":
			noTrailer = f[1]
		default:
			if keyedHex == "" {
				keyedHex = line
			} else if keylessHex == "" {
				keylessHex = line
			}
		}
	}
	if round != "3" {
		t.Fatalf("round trip count %q, want 3", round)
	}
	if keylessHex != integrityFixedPlainGolden {
		t.Errorf("keyless FixedSave differs from the Go oracle:\n got %s\nwant %s", keylessHex, integrityFixedPlainGolden)
	}
	if keyedHex != integrityFixedGolden {
		t.Errorf("keyed FixedSave differs from the Go oracle:\n got %s\nwant %s", keyedHex, integrityFixedGolden)
	}
	keyed, err := hex.DecodeString(keyedHex)
	if err != nil {
		t.Fatalf("keyed hex: %v", err)
	}
	silent := 0
	for i := range keyed {
		for _, x := range []byte{0x01, 0x02, 0x04, 0x08, 0x10, 0x20, 0x40, 0x80, 0xff} {
			reason := flips[fmt.Sprintf("%d %d", i, x)]
			if reason == "read" {
				silent++
				t.Errorf("byte %d ^ 0x%02x read clean", i, x)
				continue
			}
			// the form byte is refused by its §3.4 name (here layout_malformed,
			// the Squirrel registry's word for a form this reader does not carry);
			// every other byte is integrity_mismatch, the trailer's own included.
			if i != 0 && reason != "integrity_mismatch" {
				t.Errorf("byte %d ^ 0x%02x refused %q, want integrity_mismatch", i, x, reason)
			}
		}
	}
	// EVERY TRUNCATION IS REFUSED BY NAME, as the oracle does: a file too
	// short to hold the trailer (1..4 bytes) and every longer prefix of the
	// file are integrity_mismatch, and the file without its last four bytes
	// is too.
	for n := 1; n < len(keyed); n++ {
		if reason := truncs[fmt.Sprintf("%d", n)]; reason != "integrity_mismatch" {
			t.Errorf("%d-byte truncation refused %q, want integrity_mismatch", n, reason)
		}
	}
	if noTrailer != "integrity_mismatch" {
		t.Errorf("trailerless file refused %q, want integrity_mismatch", noTrailer)
	}
	t.Logf("Reading: %d bytes, %d flips, %d truncations, %d silent wrong decodes", len(keyed), len(flips), len(truncs), silent)
	if silent != 0 {
		t.Fatalf("%d silent wrong decodes", silent)
	}
}
