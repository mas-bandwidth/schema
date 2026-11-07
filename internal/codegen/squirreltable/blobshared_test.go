// A BLOB IS OWNED BY ONE SLOT (docs/SPEC-TABLES.md §2.5, §16.7): a second
// reference to the same blob node is `blob_shared`, refused by the strict
// reader and by every writer; a value holding two distinct blobs with equal
// bytes writes two nodes and round-trips.
package squirreltable_test

import (
	"bytes"
	"encoding/base64"
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
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestSquirrelBlobSharedMutantMatchesGo(t *testing.T) {
	t.Parallel()
	repo, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	u := loadUnit(t, repo, []string{"tables/blobs"})

	dir := t.TempDir()
	out := filepath.Join(dir, "blobdemo")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{}
	sf, err := squirrel.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, sf)
	tf, err := squirreltable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tf)
	for name, data := range files {
		writeFile(t, filepath.Join(out, name), data)
	}

	base, err := os.ReadFile(filepath.Join(repo, "testdata", "wire", "tables", "blob_shared.bin"))
	if err != nil {
		t.Fatal(err)
	}
	rng := &splitmix64{state: fuzzSeed ^ uint64(23)<<32 ^ uint64(37)}
	mutant := fuzzMutant(base, rng)

	leg, known := startFuzzLeg(t, dir, true, []fuzzRoot{{unit: "blobdemo", root: "Catalog"}})
	if !known[0] {
		t.Fatal("the leg did not carry blobdemo.Catalog")
	}
	got, err := leg.ask(0, mutant)
	if err != nil {
		t.Fatalf("the driver died on the mutant: %v\n%s", err, leg.stderr.String())
	}

	m := tabletext.NewModel(u)
	def := m.Lookup("Catalog")
	want, err := fuzzEngine(m, def, true, mutant)
	if err != nil {
		t.Fatal(err)
	}
	if v := fuzzVerdict(got, want); v != "" {
		t.Fatalf("%s\nsquirrel: %s\nengine:   %s", v, got, want)
	}
	t.Logf("mutant: %x", mutant)
}

const blobNamedTwiceSeed24Unit = `package probe
enum E0 { Alpha }
fixed table T0 {
 f0 int16
 f1 uint8
 f2 ?int32
 f3 [3]float32
 f4 [..3]int16
}
union U0 {
 ptr *bytes
 label *string
 tally int8 | min = -90, max = 106
}
table Root {
 f0 bool
 f1 bool
 f2 *bytes
 f3 uint32 = 2351968925 | min = 1691408928, max = 2403887662
 f4 string(4)
 f5 E0
 f6 int32 | min = -2147483648, max = 2147483647
 f7 int8 = 93 | min = 69, max = 94
 u0 U0
}
`

const blobNamedTwiceSeed24Wire = "01010101021103030820dad064041e05060401000080070246080f091101030a0c0d020b06c100ff2e7d080b02d11d00382842b5078b9808512d42b5078e98089e2b42b5078d9808042f42b5078f98088be145c2cb04da4f1d3442b5079298086a3242b507919808404667b5077ac408c9e19b56195cf677ffffffffffffffffe44f1c4f47c02e2f0b00000000000000"

const blobNamedTwiceAmpUnit = `package probe
table Item { b *bytes }
table Root { items [..4096]Item }
`

func writeGeneratedUnit(t *testing.T, dir, sub, schemaSrc string) (*ir.Unit, string) {
	t.Helper()
	srcPath := filepath.Join(dir, sub+".schema")
	if err := os.WriteFile(srcPath, []byte(schemaSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load([]string{srcPath})
	if err != nil {
		t.Fatalf("%s: load failed: %v", sub, err)
	}
	out := filepath.Join(dir, sub)
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	sf, err := squirrel.Generate(u)
	if err != nil {
		t.Fatalf("%s: squirrel.Generate: %v", sub, err)
	}
	tf, err := squirreltable.Generate(u)
	if err != nil {
		t.Fatalf("%s: squirreltable.Generate: %v", sub, err)
	}
	for name, data := range sf {
		writeFile(t, filepath.Join(out, name), data)
	}
	for name, data := range tf {
		writeFile(t, filepath.Join(out, name), data)
	}
	return u, squirreltable.ModuleName(u)
}

func appendLeb(dst []byte, v uint64) []byte {
	for {
		b := byte(v & 0x7f)
		v >>= 7
		if v != 0 {
			b |= 0x80
		}
		dst = append(dst, b)
		if v == 0 {
			return dst
		}
	}
}

func appendU64LE(dst []byte, v uint64) []byte {
	for i := range 8 {
		dst = append(dst, byte(v>>(8*i)))
	}
	return dst
}

// buildHostileAmplificationWire constructs the 4096-slot shared-blob wire of
// TestBlobNamedTwiceAmplification (docs/SPEC-TABLES.md §2.5): Root.items holds
// 4096 Item elements whose `b` slots all name node index 2 (record 1 in the
// node table, a single 64 KiB *bytes blob).
func buildHostileAmplificationWire(t *testing.T, u *ir.Unit, data []byte) []byte {
	t.Helper()
	rootSt := u.Tables["Root"]
	itemSt := u.Tables["Item"]
	itemsId := ir.TableFieldWireId(rootSt.Fields[0])
	bId := ir.TableFieldWireId(itemSt.Fields[0])
	ids := []uint64{itemsId, bId, ir.TableNodeWireId, ir.BytesWireTypeId}

	itemBody := []byte{2, 17, 2, 0}
	var arrBody []byte
	arrBody = append(arrBody, 13)
	arrBody = appendLeb(arrBody, 4096)
	for range 4096 {
		arrBody = appendLeb(arrBody, uint64(len(itemBody)))
		arrBody = append(arrBody, itemBody...)
	}

	var nodePayload []byte
	nodePayload = appendLeb(nodePayload, 1)
	nodePayload = appendLeb(nodePayload, 4)
	nodePayload = appendLeb(nodePayload, uint64(len(data)))
	nodePayload = append(nodePayload, data...)

	var wire []byte
	wire = append(wire, 1)
	wire = appendLeb(wire, 1)
	wire = append(wire, 14)
	wire = appendLeb(wire, uint64(len(arrBody)))
	wire = append(wire, arrBody...)
	wire = appendLeb(wire, 3)
	wire = append(wire, 12)
	wire = appendLeb(wire, uint64(len(nodePayload)))
	wire = append(wire, nodePayload...)
	wire = append(wire, 0)
	for _, id := range ids {
		wire = appendU64LE(wire, id)
	}
	wire = appendU64LE(wire, uint64(len(ids)))
	return wire
}

// TestSquirrelBlobNamedTwice cuts TestBlobNamedTwiceSeed24,
// TestBlobNamedTwiceWriters and TestBlobNamedTwiceAmplification
// (internal/codegen/gotable/blobnamedtwice_test.go) to Squirrel's surface and
// holds every refusal name, count and saved byte to the Go oracle
// (docs/SPEC-TABLES.md §2.5, §16.7).
func TestSquirrelBlobNamedTwice(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	u24, mod24 := writeGeneratedUnit(t, dir, "seed24", blobNamedTwiceSeed24Unit)
	uAmp, modAmp := writeGeneratedUnit(t, dir, "amp", blobNamedTwiceAmpUnit)

	damaged, err := hex.DecodeString(blobNamedTwiceSeed24Wire)
	if err != nil {
		t.Fatal(err)
	}
	clean := append([]byte(nil), damaged...)
	clean[6] ^= 0x01

	m24 := tabletext.NewModel(u24)
	root24 := m24.Lookup("Root")
	dInst := m24.New(root24)
	var dRep tabletext.Report
	if ok, err := tablewire.Decode(m24, dInst, damaged, &dRep); !ok || err != nil {
		t.Fatalf("oracle decode damaged: ok=%v err=%v", ok, err)
	}
	if dRep.BlobShared != 1 || dRep.Malformed {
		t.Fatalf("oracle damaged report: %+v, want BlobShared=1", dRep)
	}
	if _, err := tablewire.Encode(m24, dInst); err == nil || !strings.Contains(err.Error(), "blob_shared") {
		t.Fatalf("oracle encode damaged = %v, want blob_shared", err)
	}

	cInst := m24.New(root24)
	var cRep tabletext.Report
	if ok, err := tablewire.Decode(m24, cInst, clean, &cRep); !ok || err != nil || !cRep.Silent() {
		t.Fatalf("oracle decode clean: ok=%v err=%v rep=%+v", ok, err, cRep)
	}
	cleanSaved, err := tablewire.Encode(m24, cInst)
	if err != nil || !bytes.Equal(cleanSaved, clean) {
		t.Fatalf("oracle clean round-trip: err=%v", err)
	}

	wShared := m24.New(root24)
	oneBlob := &tabletext.Blob{Data: []byte("abc")}
	u0 := u24.TableUnions["U0"]
	for i := range wShared.Fields {
		switch wShared.Fields[i].Def.Name {
		case "f2":
			wShared.Fields[i].Cell.Blob = oneBlob
		case "u0":
			wShared.Fields[i].Cell.U = 1
			wShared.Fields[i].Cell.Arm = &tabletext.Field{Def: u0.Variants[0].F, Cell: tabletext.Cell{Blob: oneBlob}}
		}
	}
	if _, err := tablewire.Encode(m24, wShared); err == nil || !strings.Contains(err.Error(), "blob_shared") {
		t.Fatalf("oracle writers shared encode = %v, want blob_shared", err)
	}

	wTwo := m24.New(root24)
	for i := range wTwo.Fields {
		switch wTwo.Fields[i].Def.Name {
		case "f2":
			wTwo.Fields[i].Cell.Blob = &tabletext.Blob{Data: []byte("abc")}
		case "u0":
			wTwo.Fields[i].Cell.U = 1
			wTwo.Fields[i].Cell.Arm = &tabletext.Field{Def: u0.Variants[0].F, Cell: tabletext.Cell{Blob: &tabletext.Blob{Data: []byte("abc")}}}
		}
	}
	twoWire, err := tablewire.Encode(m24, wTwo)
	if err != nil || len(twoWire) == 0 {
		t.Fatalf("oracle two blobs encode: %v", err)
	}
	twoJsonBody, err := m24.Write(wTwo)
	if err != nil {
		t.Fatalf("oracle two blobs write json: %v", err)
	}
	twoJsonBody = append(twoJsonBody, '\n')
	twoJson := twoJsonBody

	ampData := make([]byte, 65536)
	for i := range ampData {
		ampData[i] = byte(i * 7)
	}
	ampWire := buildHostileAmplificationWire(t, uAmp, ampData)
	if len(ampWire) > 1<<17 {
		t.Fatalf("hostile wire is %d bytes", len(ampWire))
	}
	mAmp := tabletext.NewModel(uAmp)
	ampInst := mAmp.New(mAmp.Lookup("Root"))
	var ampRep tabletext.Report
	if ok, err := tablewire.Decode(mAmp, ampInst, ampWire, &ampRep); !ok || err != nil {
		t.Fatalf("oracle decode amplification: ok=%v err=%v", ok, err)
	}
	if ampRep.BlobShared != 4095 || ampRep.Malformed {
		t.Fatalf("oracle amplification report: %+v, want BlobShared=4095", ampRep)
	}
	if _, err := tablewire.Encode(mAmp, ampInst); err == nil || !strings.Contains(err.Error(), "blob_shared") {
		t.Fatalf("oracle amplification encode = %v, want blob_shared", err)
	}

	probe := fmt.Sprintf(`local inputs = dofile("inputs.nut", true);
local HEXB = array(256);
for (local i = 0; i < 256; i++) HEXB[i] = format("%%02x", i);
local join = function(a) {
  if (a.len() == 0) return "";
  while (a.len() > 1) {
    local b = array((a.len() + 1) / 2);
    for (local i = 0; i < b.len(); i++) b[i] = (2 * i + 1 < a.len()) ? a[2 * i] + a[2 * i + 1] : a[2 * i];
    a = b;
  }
  return a[0];
};
local hexOf = function(b) {
  if (b == null) return "null";
  local parts = array(b.len());
  for (local i = 0; i < b.len(); i++) parts[i] = HEXB[b[i] & 0xff];
  return join(parts);
};
local strOf = function(b) {
  local parts = array(b.len());
  for (local i = 0; i < b.len(); i++) parts[i] = format("%%c", b[i] & 0xff);
  return join(parts);
};
local nn = function(x) { return x == null ? "-" : x.tostring(); };

local fresh = function() {
  return { unknown = 0, kindMismatch = 0, widened = 0, clamped = 0, duplicate = 0, malformed = false, refused = false };
};

// 1. Seed24
{
  local schema = dofile("seed24/schema_module.nut", true);
  local T = schema.require(%q);
  local L = schema.require("schema_table_load");
  local S = schema.require("schema_table_save");
  local J = schema.require("schema_table_json");
  local R = schema.require("schema_table_refusal");
  local X = schema.require("schema_table_text");

  local sr = {};
  local sv = R.ReadStrict(T, "Root", inputs["damaged"], sr);
  print("seed24_strict|" + nn(sv) + "|" + nn(sr.reason) + "|" + nn(sr.field) + "|" + nn(("blobShared" in sr) ? sr.blobShared : 0) + "\n");

  local ltr = {};
  local ltv = X.LoadText(T, "Root", strOf(inputs["damaged_b64"]), ltr);
  print("seed24_loadtext|" + nn(ltv) + "|" + nn(ltr.reason) + "|" + nn(ltr.field) + "|" + nn(("blobShared" in ltr) ? ltr.blobShared : 0) + "\n");

  local tr = fresh();
  local tv = L.loadTolerant(T, "Root", inputs["damaged"], tr);
  local sameNode = tv != null && tv.F2 != null && tv.F2.len() > 0 && tv.F2 == tv.U0.Ptr;
  local jm = J.ToJsonMeasure(T, "Root", tv);
  local jr = ("ToJsonReason" in J) ? J.ToJsonReason : null;
  local jw = J.ToJson(T, "Root", tv);
  local sm = S.measure(T, "Root", tv);
  local sreason = ("SaveReason" in S) ? S.SaveReason : null;
  local sw = S.save(T, "Root", tv);
  print("seed24_tolerant|" + (tv != null) + "|" + tr.malformed + "|" + nn(("blobShared" in tr) ? tr.blobShared : 0) + "|" +
        nn(("reason" in tr) ? tr.reason : null) + "|" + nn(("field" in tr) ? tr.field : null) + "|" + sameNode + "|" +
        jm + "|" + nn(jr) + "|" + nn(jw) + "|" + sm + "|" + nn(sreason) + "|" + nn(sw) + "\n");

  local cr = {};
  local cv = R.ReadStrict(T, "Root", inputs["clean"], cr);
  local cm = cv == null ? -1 : S.measure(T, "Root", cv);
  local cw = cv == null ? null : S.save(T, "Root", cv);
  print("seed24_clean|" + nn(cr.reason) + "|" + cm + "|" + hexOf(cw) + "\n");

  // 2. Writers
  local b = blob(3); b[0] = 'a'; b[1] = 'b'; b[2] = 'c';
  local r = T.Root();
  r.F2 = b; r.U0.Type = 1; r.U0.Ptr = b;
  local wm = S.measure(T, "Root", r);
  local wmReason = ("SaveReason" in S) ? S.SaveReason : null;
  local ws = S.save(T, "Root", r);
  local wst = X.SaveText(T, "Root", r);
  local wstReason = ("SaveReason" in S) ? S.SaveReason : null;
  local wjm = J.ToJsonMeasure(T, "Root", r);
  local wjmReason = ("ToJsonReason" in J) ? J.ToJsonReason : null;
  local wj = J.ToJson(T, "Root", r);
  print("writers_shared|" + wm + "|" + nn(wmReason) + "|" + nn(ws) + "|" + nn(wst) + "|" + nn(wstReason) + "|" +
        wjm + "|" + nn(wjmReason) + "|" + nn(wj) + "\n");

  local b1 = blob(3); b1[0] = 'a'; b1[1] = 'b'; b1[2] = 'c';
  local b2 = blob(3); b2[0] = 'a'; b2[1] = 'b'; b2[2] = 'c';
  local q = T.Root();
  q.F2 = b1; q.U0.Type = 1; q.U0.Ptr = b2;
  local qm = S.measure(T, "Root", q);
  local qs = S.save(T, "Root", q);
  local qst = X.SaveText(T, "Root", q);
  local qvr = {};
  local qv = qs == null ? null : R.ReadStrict(T, "Root", qs, qvr);
  local qvBack = qv == null ? null : S.save(T, "Root", qv);
  local qjm = qv == null ? -1 : J.ToJsonMeasure(T, "Root", qv);
  local qj = qv == null ? null : J.ToJson(T, "Root", qv);
  local qfr = fresh();
  local qf = qj == null ? null : J.FromJsonTolerant(T, "Root", qj, qfr);
  local qj2 = (qf == null || qfr.malformed) ? null : J.ToJson(T, "Root", qf);
  print("writers_two|" + qm + "|" + hexOf(qs) + "|" + nn(qst) + "|" + nn(qvr.reason) + "|" +
        hexOf(qvBack) + "|" + qjm + "|" + hexOf(qj) + "|" + hexOf(qj2) + "\n");
}

// 3. Amplification
{
  local schema = dofile("amp/schema_module.nut", true);
  local T = schema.require(%q);
  local L = schema.require("schema_table_load");
  local S = schema.require("schema_table_save");
  local J = schema.require("schema_table_json");
  local R = schema.require("schema_table_refusal");

  local data = blob(65536);
  for (local i = 0; i < 65536; i++) data[i] = (i * 7) & 0xff;
  local r = T.Root();
  for (local i = 0; i < 4096; i++) {
    local it = T.Item();
    it.B = data;
    r.Items.append(it);
  }
  local bm = S.measure(T, "Root", r);
  local bmReason = ("SaveReason" in S) ? S.SaveReason : null;
  local bs = S.save(T, "Root", r);
  local bjm = J.ToJsonMeasure(T, "Root", r);
  local bjmReason = ("ToJsonReason" in J) ? J.ToJsonReason : null;
  print("amp_builder|" + bm + "|" + nn(bmReason) + "|" + nn(bs) + "|" + bjm + "|" + nn(bjmReason) + "\n");

  local sr = {};
  local sv = R.ReadStrict(T, "Root", inputs["amp"], sr);
  print("amp_strict|" + nn(sv) + "|" + nn(sr.reason) + "|" + nn(sr.field) + "|" + nn(("blobShared" in sr) ? sr.blobShared : 0) + "\n");

  local tr = fresh();
  local tv = L.loadTolerant(T, "Root", inputs["amp"], tr);
  local allSame = tv != null && tv.Items.len() == 4096 && tv.Items[0].B != null && tv.Items[0].B.len() == 65536;
  if (allSame) {
    local first = tv.Items[0].B;
    for (local i = 0; i < 65536; i++) {
      if ((first[i] & 0xff) != ((i * 7) & 0xff)) { allSame = false; break; }
    }
    for (local i = 1; i < 4096; i++) {
      if (tv.Items[i].B != first) { allSame = false; break; }
    }
  }
  local ajm = tv == null ? 0 : J.ToJsonMeasure(T, "Root", tv);
  local ajReason = ("ToJsonReason" in J) ? J.ToJsonReason : null;
  local ajw = tv == null ? "non-null" : J.ToJson(T, "Root", tv);
  local asm = tv == null ? 0 : S.measure(T, "Root", tv);
  local asReason = ("SaveReason" in S) ? S.SaveReason : null;
  local asw = tv == null ? "non-null" : S.save(T, "Root", tv);
  print("amp_tolerant|" + (tv != null) + "|" + tr.malformed + "|" + nn(("blobShared" in tr) ? tr.blobShared : 0) + "|" +
        nn(("reason" in tr) ? tr.reason : null) + "|" + nn(("field" in tr) ? tr.field : null) + "|" + allSame + "|" +
        ajm + "|" + nn(ajReason) + "|" + nn(ajw) + "|" + asm + "|" + nn(asReason) + "|" + nn(asw) + "\n");
}
`, mod24, modAmp)

	writeFile(t, filepath.Join(dir, "main.nut"), []byte(probe))
	inputs := map[string][]byte{
		"damaged":     damaged,
		"damaged_b64": []byte(base64.StdEncoding.EncodeToString(damaged)),
		"clean":       clean,
		"amp":         ampWire,
	}
	stdout := sqrun.Run(t, dir, "main.nut", inputs)
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimRight(string(stdout), "\n"), "\n") {
		k, v, ok := strings.Cut(line, "|")
		if !ok {
			t.Fatalf("unexpected probe line %q", line)
		}
		got[k] = v
	}

	twoHex := hex.EncodeToString(twoWire)
	twoB64 := base64.StdEncoding.EncodeToString(twoWire)
	twoJsonHex := hex.EncodeToString(twoJson)
	cleanHex := hex.EncodeToString(clean)

	wants := []struct {
		key, want string
	}{
		{"seed24_strict", "-|blob_shared|ptr|1"},
		{"seed24_loadtext", "-|blob_shared|ptr|1"},
		{"seed24_tolerant", "true|false|1|blob_shared|ptr|true|-1|blob_shared|-|-1|blob_shared|-"},
		{"seed24_clean", fmt.Sprintf("-|%d|%s", len(clean), cleanHex)},
		{"writers_shared", "-1|blob_shared|-|-|blob_shared|-1|blob_shared|-"},
		{"writers_two", fmt.Sprintf("%d|%s|%s|-|%s|%d|%s|%s", len(twoWire), twoHex, twoB64, twoHex, len(twoJson), twoJsonHex, twoJsonHex)},
		{"amp_builder", "-1|blob_shared|-|-1|blob_shared"},
		{"amp_strict", "-|blob_shared|b|4095"},
		{"amp_tolerant", "true|false|4095|blob_shared|b|true|-1|blob_shared|-|-1|blob_shared|-"},
	}

	for _, w := range wants {
		if got[w.key] != w.want {
			t.Errorf("%s:\n got %s\nwant %s", w.key, got[w.key], w.want)
		}
	}
	t.Logf("compared %d blob_shared cases against Go oracle", len(wants))
}
