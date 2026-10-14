package jstable

// fixedroadmap_t12 — the js leg's union-field-arms row of docs/roadmap.sexp's
// node `fixed-tables` (ROADMAP.md "NEW Fixed Tables"), one subtest per leaf this
// card proves: union-field-arms/js/valid-data (docs/roadmap-evidence/fixed-js.sexp
// line 78). Law: docs/SPEC-TABLES.md §2.6 (an arm is a field line: an arm's type
// is any type a field's is) and §3.4 (the fixed form's constant arm storage);
// docs/FIXED-FORM-ALGORITHM.md §2 (the walk's union row, kind 15) and §3 (the
// record's union: the tag then the widest arm).
//
// The row's remaining clause — "Direct scalar arms emitted; direct
// text/bytes/array/optional arms refused" — is this card's clause: the shared
// gate already admits a direct arm (ir/fixedform.go TableFixedSupported), but
// internal/codegen/js emitted no storage class for a direct-arm union, so the
// generated module died at `new Body()` with `ReferenceError: Body is not
// defined` and no direct text, bytes or array arm round-tripped. An optional arm
// is refused by name at the gate (§2.6, §3.4) and is not this card's.
//
// The js/W16, js/R28, utf16-values/js/valid-data and js/C5 leaves sit on the
// same cells but belong to the-guard-chain-answers-the-outer-tag and
// wide-text-in-table-closures-for-the-four-remaining-legs cards: no subtest, no
// code and no overlay edit for them here.

import (
	"bytes"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// t12Probe is one capability probe, the t09 shape: generate the fixture's
// module, run the probe body under the leg's node through jsRunVersionProbe
// (no older lineage), and assert the module answers `ok`.
func t12Probe(t *testing.T, src, table, body string) {
	t.Helper()
	node := jsNode(t)
	out, err := jsRunVersionProbe(t, node, src, nil, 0, table, body)
	if err != nil {
		t.Fatalf("capability probe: %v\n%s", err, out)
	}
}

// TestFixedRoadmapJsT12GuardsWideText holds one subtest per task this card
// proves: union-field-arms/js/valid-data, a direct text, bytes and array arm
// each written non-default and boundary and read back through the emitted
// module.
func TestFixedRoadmapJsT12GuardsWideText(t *testing.T) {
	t.Parallel()
	tests := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"union-field-arms/js/valid-data", t12UnionFieldArms},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			tc.run(t)
		})
	}
}

// t12UnionFieldArms: union-field-arms [capability] "Union arms holding scalar,
// text or array fields" (docs/FIXED-FORM-ALGORITHM.md §3: "a UNION writes the
// tag and the taken arm only, zero behind a narrower one"; SPEC-TABLES §2.6: "an
// arm's type is any type a field's is"). The subtest writes a DIRECT text arm,
// a DIRECT bytes arm and a DIRECT array arm, each at a boundary value, and
// proves every one round-trips through the emitted module under the leg's own
// node — the arm the note named as dying at `new Body()`. A same-build read is
// one identity copy, so the union's LAYOUT ENTRY is asserted on its own: the
// leg's layout bytes are the reference walk's (kind 15, the tag then the widest
// arm).
func t12UnionFieldArms(t *testing.T) {
	const src = `package probe

union Body
{
    text string(8)
    data bytes(4)
    list [3]int32
}

fixed table T { body Body }
`
	u := jsUnitOf(t, src)
	lineage, ok := FixedLineageOf(u, "T")
	if !ok {
		t.Fatal("union-field-arms: the leg emits no fixed form for T")
	}
	var root *ir.Struct
	for _, st := range jsFixedUnitRoots(u) {
		if st.Name == "T" {
			root = st
		}
	}
	walk := ir.TableFixedWalkRoot(root)
	if want := ir.TableFixedLayoutBytes(walk); !bytes.Equal(lineage.Layout, want) {
		t.Fatalf("union-field-arms: the leg's layout is not the reference walk's\n leg %x\n ref %x", lineage.Layout, want)
	}
	// THE UNION'S OWN ENTRY, kind 15: the tag then the widest arm (§3). The
	// arms follow it in declared order, a direct text/bytes/array arm walked as
	// its own field type under the arm's position (§2.6).
	sawUnion := false
	for _, e := range walk {
		if e.Kind != ir.TableKindUnion {
			continue
		}
		sawUnion = true
		if e.Children != 3 {
			t.Errorf("union-field-arms: the union walks %d children, want 3", e.Children)
		}
	}
	if !sawUnion {
		t.Fatal("union-field-arms: the walk carries no union entry")
	}

	const probe = `
const { BodyType } = await import("./Probe.js");

function roundtrip(label, setup, check) {
  const v = new T(); InitT(v);
  setup(v);
  const buf = new Uint8Array(TMeasure(1));
  if (TSave([v], 1, buf) !== buf.length) { fail(label + ": save"); }
  const back = [new T()]; InitT(back[0]);
  const report = new TableFixedReport();
  const n = TLoad(back, 1, buf, buf.length, TNewPlan(4096, 4096), report);
  if (!(n === 1 && !report.malformed)) { fail(label + ": load " + reason(report)); }
  check(back[0]);
}

roundtrip("union-field-arms/text", (v) => {
  v.Body.Type = BodyType.Text;
  const s = "abcdefgh";
  for (let i = 0; i < s.length; i++) { v.Body.Text[i] = s.charCodeAt(i); }
  v.Body.TextLength = s.length;
}, (b) => {
  if (!(b.Body.Type === BodyType.Text && b.Body.TextLength === 8)) {
    fail("union-field-arms: the text arm's tag and length did not round trip");
    return;
  }
  let got = "";
  for (let i = 0; i < b.Body.TextLength; i++) { got += String.fromCharCode(b.Body.Text[i]); }
  if (!(got === "abcdefgh")) { fail("union-field-arms: the text arm's bytes did not round trip, got " + JSON.stringify(got)); }
});

roundtrip("union-field-arms/bytes", (v) => {
  v.Body.Type = BodyType.Data;
  const bytes = [0x00, 0xff, 0x7f, 0x80];
  for (let i = 0; i < bytes.length; i++) { v.Body.Data[i] = bytes[i]; }
  v.Body.DataLength = bytes.length;
}, (b) => {
  if (!(b.Body.Type === BodyType.Data && b.Body.DataLength === 4)) {
    fail("union-field-arms: the bytes arm's tag and length did not round trip");
    return;
  }
  if (!(b.Body.Data[0] === 0x00 && b.Body.Data[1] === 0xff && b.Body.Data[2] === 0x7f && b.Body.Data[3] === 0x80)) {
    fail("union-field-arms: every byte of the bytes arm did not round trip");
  }
});

roundtrip("union-field-arms/array", (v) => {
  v.Body.Type = BodyType.List;
  v.Body.List[0] = -2147483648;
  v.Body.List[1] = 0;
  v.Body.List[2] = 2147483647;
}, (b) => {
  if (!(b.Body.Type === BodyType.List &&
        b.Body.List[0] === -2147483648 && b.Body.List[1] === 0 && b.Body.List[2] === 2147483647)) {
    fail("union-field-arms: the array arm's elements did not round trip");
  }
});

const none = new T(); InitT(none);
if (!(none.Body.Type === BodyType.None)) { fail("union-field-arms: a default union is not None"); }
const noneBuf = new Uint8Array(TMeasure(1));
if (TSave([none], 1, noneBuf) !== noneBuf.length) { fail("union-field-arms: save none"); }
const noneBack = [new T()]; InitT(noneBack[0]);
const noneReport = new TableFixedReport();
const noneN = TLoad(noneBack, 1, noneBuf, noneBuf.length, TNewPlan(4096, 4096), noneReport);
if (!(noneN === 1 && !noneReport.malformed)) { fail("union-field-arms: load none " + reason(noneReport)); }
if (!(noneBack[0].Body.Type === BodyType.None)) { fail("union-field-arms: None did not round trip"); }
if (BodyType.Text === BodyType.Data || BodyType.Text === BodyType.List || BodyType.Data === BodyType.List) {
  fail("union-field-arms: two direct arms collide in the tag enum");
}
`
	t12Probe(t, src, "T", probe)
}
