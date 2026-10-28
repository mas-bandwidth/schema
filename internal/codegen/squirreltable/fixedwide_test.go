package squirreltable_test

import (
	"encoding/binary"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestSquirrelFixedForm128BitLeaves pins the sixteen-byte leaf store of the
// fixed form (docs/SPEC-TABLES.md §3.4): an int128, a uint128 and a
// fixed(112,16) field each ride as the [lo, hi] pair the Squirrel table
// surface uses, low 64-bit half first, and the Squirrel bytes equal the Go
// leg's (the oracle for every expected answer). FixedLoad round-trips the
// record, and the test prints the record count.
func TestSquirrelFixedForm128BitLeaves(t *testing.T) {
	t.Parallel()
	const src = `package wide
fixed table Wide128 {
    a int128 | min = -170141183460469231731687303715884105728, max = 170141183460469231731687303715884105727
    b uint128
    c fixed(112, 16) | min = -1000000, max = 1000000
}
`
	u := fixedSynthetic(t, src)
	dir := t.TempDir()
	writeGenerated(t, dir, u)

	aMax := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 127), big.NewInt(1))
	bMax := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 128), big.NewInt(1))
	cVal := big.NewInt(1000000)
	cRaw := new(big.Int).Lsh(cVal, 16)
	aLo, aHi := split128(aMax)
	bLo, bHi := split128(bMax)
	cLo, cHi := split128(cRaw)

	module := filepath.Join(dir, "WideTables.nut")
	data, err := os.ReadFile(module)
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	layoutHex := regExtract(t, body, `Wide128FixedLayout <- "([0-9a-f]+)"`)
	layout, err := decodeHex(layoutHex)
	if err != nil {
		t.Fatal(err)
	}
	rec, err := strconv.Atoi(regExtract(t, body, `Wide128FixedRecord <- (\d+)`))
	if err != nil {
		t.Fatal(err)
	}
	st, ok := u.Tables["Wide128"]
	if !ok {
		t.Fatalf("the unit carries no table Wide128")
	}
	hash := ir.TableFixedLayoutHash(layout, st)
	if rec < 8 {
		t.Fatalf("the emitted record %d is shorter than its hash", rec)
	}

	// The byte oracle is the Go leg's writer for the same unit: the low
	// 64-bit half of each 128-bit leaf lands first in its sixteen-byte store
	// (docs/SPEC-TABLES.md §3.4).
	want := []byte{3, 0, 0, 0, 0, 0, 0, 0}
	want = appendU64le(want, hash)
	want = appendU32le(want, uint32(len(layout)))
	want = append(want, layout...)
	want = appendU64le(want, hash)
	bodyBytes := make([]byte, rec-8)
	binary.LittleEndian.PutUint64(bodyBytes[0:], aLo)
	binary.LittleEndian.PutUint64(bodyBytes[8:], aHi)
	binary.LittleEndian.PutUint64(bodyBytes[16:], bLo)
	binary.LittleEndian.PutUint64(bodyBytes[24:], bHi)
	binary.LittleEndian.PutUint64(bodyBytes[32:], cLo)
	binary.LittleEndian.PutUint64(bodyBytes[40:], cHi)
	want = append(want, bodyBytes...)

	main := fmt.Sprintf(`local S = dofile("schema_module.nut", true);
local T = S.require("WideTables");
local r = T.Wide128();
r.A = %s;
r.B = %s;
r.C = %s;
local b = blob(1024);
local n = T.Wide128FixedSave(b, [r]);
local h = "";
for (local i = 0; i < n; i++) h += format("%%02x", b[i] & 0xff);
print("SAVE:" + h + "\n");
local bf = blob(n);
for (local i = 0; i < n; i++) bf[i] = b[i];
local back = [T.Wide128()];
local got = T.Wide128FixedLoadTolerant(bf, back, null);
print("COUNT:" + got + "\n");
local b2 = blob(1024);
local n2 = T.Wide128FixedSave(b2, back);
local h2 = "";
for (local i = 0; i < n2; i++) h2 += format("%%02x", b2[i] & 0xff);
print("RESAVE:" + h2 + "\n");
`, squirrel.Int128Lit(aMax), squirrel.Int128Lit(bMax), squirrel.Int128Lit(cRaw))
	if err := os.WriteFile(filepath.Join(dir, "main.nut"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	out := string(sqrun.Run(t, dir, "main.nut", nil))

	var saveHex, resaveHex string
	count := -1
	for line := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
		if rest, ok := strings.CutPrefix(line, "SAVE:"); ok {
			saveHex = rest
		} else if rest, ok := strings.CutPrefix(line, "RESAVE:"); ok {
			resaveHex = rest
		} else if rest, ok := strings.CutPrefix(line, "COUNT:"); ok {
			n, err := strconv.Atoi(rest)
			if err != nil {
				t.Fatalf("COUNT line %q: %v", line, err)
			}
			count = n
		}
	}
	t.Logf("record count = %d", count)
	if count != 1 {
		t.Fatalf("FixedLoad returned %d, want 1", count)
	}
	got, err := decodeHex(saveHex)
	if err != nil {
		t.Fatalf("SAVE line %q: %v", saveHex, err)
	}
	if fmt.Sprintf("%x", got) != fmt.Sprintf("%x", want) {
		t.Errorf("Squirrel FixedSave bytes differ from the Go leg's: got %x want %x", got, want)
	}
	if resaveHex != saveHex {
		t.Errorf("FixedLoad round-trip changed the bytes: resaved %x original %x", resaveHex, saveHex)
	}
}

func split128(v *big.Int) (lo, hi uint64) {
	m := new(big.Int).Set(v)
	if m.Sign() < 0 {
		m.Add(m, new(big.Int).Lsh(big.NewInt(1), 128))
	}
	mask := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 64), big.NewInt(1))
	lo = new(big.Int).And(m, mask).Uint64()
	hi = new(big.Int).And(new(big.Int).Rsh(m, 64), mask).Uint64()
	return
}

func appendU64le(b []byte, v uint64) []byte {
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], v)
	return append(b, buf[:]...)
}

func appendU32le(b []byte, v uint32) []byte {
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], v)
	return append(b, buf[:]...)
}

func regExtract(t *testing.T, body, pattern string) string {
	t.Helper()
	re := regexp.MustCompile(pattern)
	m := re.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("the generated module lacks %q", pattern)
	}
	return m[1]
}
