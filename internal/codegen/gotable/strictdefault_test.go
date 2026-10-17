package gotable

import (
	"fmt"
	"maps"
	"testing"
)

// TestGoDefaultReadIsStrict holds docs/SPEC-TABLES.md §6.5's rule that THE
// PLAIN NAME IS THE STRICT READ (Go first): every generated read entry point a
// caller reaches by the plain name refuses whatever ReadStrict refuses and
// hands back no recovered value, the typed reason reaching the caller through
// the report it passes (report.Err(), matched with errors.Is); the tolerant
// read keeps the recovered value and its counters under the explicit
// <Name>...Tolerant name. One subtest per class of table: a FIXED-class root
// (Load, FromJson, LoadText, FixedLoad, LoadMessages) and a VARIABLE root
// (the region Load, LoadBuilder, FromJson, LoadText), each over a clean, a
// clamped, a kind-mismatched and a truncated wire, and the variable root over a
// missing-required one. A fixed table cannot declare `| required` (§2.3: every
// fixed field is always written), so missing_required has no fixed wire.
func TestGoDefaultReadIsStrict(t *testing.T) {
	t.Parallel()
	t.Run("fixed", func(t *testing.T) {
		t.Parallel()
		const schema = `package probe
fixed table Fix { n int32 = 1 | min = 0, max = 10
 m int32 = 2 }
`
		// The fixed form's kind mismatch is reached through a LINEAGE plan only
		// (docs/FIXED-FORM-ALGORITHM.md §5.4): an older layout whose n is a
		// float32 is handed to the reader as data, and its file is forged here
		// from the same lock entry the emitter lays down.
		const older = `package probe
fixed table Fix { n float32
 m int32 = 2 }
`
		u := unitFrom(t, schema)
		old, ok := FixedLineageOf(unitFrom(t, older), "Fix")
		if !ok {
			t.Fatal("no lineage entry for the older Fix")
		}
		lineage := map[string][]FixedLineageEntry{"Fix": {old}}
		f := []byte{fixtureFormByte, 0, 0, 0, 0, 0, 0, 0}
		f = appendFixedU64(f, old.Wire)
		f = appendFixedU32(f, uint32(len(old.Layout)))
		f = append(f, old.Layout...)
		rec := make([]byte, old.Record)
		for i := 0; i < 8; i++ {
			rec[i] = byte(old.Wire >> (8 * i))
		}
		f = append(f, rec...)
		edit := func(files map[string][]byte) {
			tables, err := GenerateLineage(u, lineage)
			if err != nil {
				t.Fatalf("GenerateLineage: %v", err)
			}
			maps.Copy(files, tables)
		}
		src := fmt.Sprintf(strictDefaultFixedProbe, f)
		if out, err := runGeneratedUnit(t, u, src, edit, "-v", "-run", "TestFixedStrict"); err != nil {
			t.Fatalf("generated runtime: %v\n%s", err, out)
		}
	})
	t.Run("fixed_message_kind_mismatch", func(t *testing.T) {
		t.Parallel()
		// The message form's kind mismatch is a PEER's announcement (§3.3): the
		// older writer sends c as a float32 to a reader that declares an int32.
		runMessagePair(t, "package probe\nfixed table Root { a int32\n c float32 }\n", "package probe\nfixed table Root { a int32\n c int32 = 7 }\n", "\tw.A = 11\n\tw.C = 1.5", `
	if ok || count != 0 || report.Err() != error(new.TableRefuseKindMismatch) || report.KindMismatch != 1 {
		t.Fatalf("plain LoadMessages took a kind-mismatched batch: count=%d ok=%v %+v", count, ok, report)
	}
	if decoded[0].A != 0 || decoded[0].C != 7 {
		t.Fatalf("a refused batch handed back a recovered value: %+v", decoded[0])
	}
	var tolerant new.TableReport
	n, tok := new.RootLoadMessagesTolerant(decoded, &vocab, out, &tolerant)
	if !tok || n != 1 || tolerant.KindMismatch != 1 || decoded[0].A != 11 || decoded[0].C != 7 {
		t.Fatalf("LoadMessagesTolerant did not keep the recovered value: n=%d ok=%v %+v %+v", n, tok, tolerant, decoded[0])
	}
`)
	})
	t.Run("variable", func(t *testing.T) {
		t.Parallel()
		runGenerated(t, `package probe
table Var { n int32 = 1 | min = 0, max = 10
 req int32 | required }
`, strictDefaultVariableProbe)
	})
	// An `integrity` root (§2.14) emits its own tolerant twins behind the
	// trailer check; the plain Load and LoadBuilder over them are strict too.
	t.Run("integrity", func(t *testing.T) {
		t.Parallel()
		runGenerated(t, `package probe
table Seal | integrity = "crc32c" { n int32 = 1 | min = 0, max = 10
 m int32 = 2 }
`, strictDefaultIntegrityProbe)
	})
}

// strictDefaultFixedProbe runs inside the generated fixed unit; %#v is the
// forged form-3 file of the older layout.
const strictDefaultFixedProbe = `package probe
import ("encoding/base64";"errors";"testing")
var olderFile = %#v
func field(kind uint8, payload []byte) []byte {var ids TableIds;buf:=make([]byte,64);w:=TableWriter{Buffer:buf,Ids:&ids};w.Put8(1);w.Id(FixTableFields[0].Id);w.Put8(kind);for _,b:=range payload{w.Put8(b)};w.Put8(0);w.Trailer();return buf[:w.Offset]}
func defaults() Fix {var v Fix;FixReset(&v);return v}
func vocab(t *testing.T) *TableVocabulary {v:=new(TableVocabulary);v.Init(make([]TableMessageEntry,TableMessageEntriesHere));a:=make([]byte,AnnounceMeasure());if Announce(a)!=int64(len(a)){t.Fatal("announce")};var r TableReport;if !AnnounceRead(v,a,&r){t.Fatalf("announce read %%+v",r)};return v}
func TestFixedStrict(t *testing.T) {
	t.Parallel()
	clean := Fix{N: 3, M: 4}
	wire := make([]byte, FixMeasure(&clean))
	if FixSave(&clean, wire) != int64(len(wire)) {t.Fatal("save")}
	type wcase struct {name string; wire []byte; want error; counted func(TableReport) bool}
	wires := []wcase{
		{"clamped", field(4, []byte{100, 0, 0, 0}), TableRefuseClamped, func(r TableReport) bool {return r.Clamped == 1}},
		{"kind_mismatch", field(9, []byte{1, 0, 0, 0, 0, 0, 0, 0}), TableRefuseKindMismatch, func(r TableReport) bool {return r.KindMismatch == 1}},
		{"truncated", wire[:len(wire)-1], TableRefuseWireDamaged, func(r TableReport) bool {return r.Malformed}},
	}
	// THE PLAIN Load, clean then refused, and the tolerant twin keeping the value.
	var v Fix; var r TableReport
	if !FixLoad(&v, wire, &r) || v != clean || r.Err() != nil {t.Fatalf("clean Load: %%+v %%+v", v, r)}
	if got, err := FixLoadText(base64.StdEncoding.EncodeToString(wire), nil); err != nil || *got != clean {t.Fatalf("clean LoadText: %%v", err)}
	if got, err := FixReadStrict(wire); err != nil || *got != clean {t.Fatalf("clean ReadStrict: %%v", err)}
	for _, c := range wires {
		v = Fix{N: 9, M: 9}; r = TableReport{Unknown: 5}
		if FixLoad(&v, c.wire, &r) || !errors.Is(r.Err(), c.want) || v != defaults() || r.Unknown != 0 || !c.counted(r) {t.Fatalf("%%s: plain Load did not refuse by name: %%+v %%+v", c.name, v, r)}
		if FixLoad(&v, c.wire, nil) {t.Fatalf("%%s: plain Load with a nil report took it", c.name)}
		if got, err := FixLoadText(base64.StdEncoding.EncodeToString(c.wire), &r); got != nil || !errors.Is(err, c.want) {t.Fatalf("%%s: LoadText %%v", c.name, err)}
		if got, err := FixReadStrict(c.wire); got != nil || !errors.Is(err, c.want) {t.Fatalf("%%s: ReadStrict %%v", c.name, err)}
		var tr TableReport
		ok := FixLoadTolerant(&v, c.wire, &tr)
		if c.name == "truncated" {
			if ok || !c.counted(tr) {t.Fatalf("truncated: the tolerant read took framing damage: %%+v", tr)}
		} else if !ok || !c.counted(tr) || v.M != 2 {t.Fatalf("%%s: LoadTolerant did not keep the recovered value: %%+v %%+v", c.name, v, tr)}
	}
	// FromJson: the same four over the text form.
	texts := []struct {name, text string; want error; counted func(TableReport) bool}{
		{"clamped", "{\"n\":100}", TableRefuseClamped, func(r TableReport) bool {return r.Clamped == 1}},
		{"kind_mismatch", "{\"n\":\"x\"}", TableRefuseKindMismatch, func(r TableReport) bool {return r.KindMismatch == 1}},
		{"truncated", "{\"n\":3", TableRefuseMalformed, func(r TableReport) bool {return r.Malformed}},
	}
	v = Fix{}
	if !FixFromJson(&v, []byte("{\"n\":3,\"m\":4}"), &r) || v != clean || r.Err() != nil {t.Fatalf("clean FromJson: %%+v %%+v", v, r)}
	for _, c := range texts {
		v = Fix{N: 9, M: 9}
		if FixFromJson(&v, []byte(c.text), &r) || !errors.Is(r.Err(), c.want) || v != defaults() || !c.counted(r) {t.Fatalf("%%s: plain FromJson did not refuse by name: %%+v %%+v", c.name, v, r)}
		var tr TableReport
		ok := FixFromJsonTolerant(&v, []byte(c.text), &tr)
		if c.name == "truncated" {
			if ok || !c.counted(tr) {t.Fatalf("truncated text: %%+v", tr)}
		} else if !ok || !c.counted(tr) {t.Fatalf("%%s: FromJsonTolerant did not keep the recovered value: %%+v %%+v", c.name, v, tr)}
	}
	// FixedLoad: the form-3 file, clamped by a forged body, kind-mismatched by
	// the older layout's lineage plan, and truncated by one byte.
	plan := make([]TableFixedEntry, 256)
	form3 := make([]byte, FixFixedMeasure(1))
	if FixFixedSave([]Fix{clean}, form3) != int64(len(form3)) {t.Fatal("fixed save")}
	got := make([]Fix, 1)
	if n := FixFixedLoad(got, form3, plan, &r); n != 1 || got[0] != clean || r.Err() != nil {t.Fatalf("clean FixedLoad: %%d %%+v %%+v", n, got[0], r)}
	clamped := append([]byte(nil), form3...)
	body := clamped[len(clamped)-FixFixedRecordBytes+8:]
	body[0], body[1], body[2], body[3] = 100, 0, 0, 0
	fixed := []wcase{
		{"clamped", clamped, TableRefuseClamped, func(r TableReport) bool {return r.Clamped == 1}},
		{"kind_mismatch", olderFile, TableRefuseKindMismatch, func(r TableReport) bool {return r.KindMismatch == 1}},
		{"truncated", form3[:len(form3)-1], TableRefuseMalformed, func(r TableReport) bool {return r.Malformed}},
	}
	for _, c := range fixed {
		got[0] = Fix{N: 9, M: 9}
		if n := FixFixedLoad(got, c.wire, plan, &r); n != -1 || !errors.Is(r.Err(), c.want) || !c.counted(r) || (c.name != "truncated" && got[0] != defaults()) {t.Fatalf("%%s: plain FixedLoad did not refuse by name: %%d %%+v %%+v", c.name, n, got[0], r)}
		var tr TableReport
		n := FixFixedLoadTolerant(got, c.wire, plan, &tr)
		if c.name == "truncated" {
			if n != -1 || !c.counted(tr) {t.Fatalf("truncated form 3: %%d %%+v", n, tr)}
		} else if n != 1 || !c.counted(tr) {t.Fatalf("%%s: FixedLoadTolerant did not keep the recovered record: %%d %%+v %%+v", c.name, n, got[0], tr)}
	}
	// LoadMessages: a clean batch, one whose n is forged past max (bits 3..6
	// of the first body byte carry it), and one cut short.
	voc := vocab(t)
	zero := Fix{N: 0, M: 4}
	msg := make([]byte, FixMeasureMessages([]*Fix{&clean}, nil))
	if FixSaveMessages([]*Fix{&clean}, msg, nil) != int64(len(msg)) {t.Fatal("save messages")}
	if n, ok := FixLoadMessages(got, voc, msg, &r); !ok || n != 1 || got[0] != clean || r.Err() != nil {t.Fatalf("clean LoadMessages: %%d %%v %%+v", n, ok, r)}
	forged := make([]byte, FixMeasureMessages([]*Fix{&zero}, nil))
	if FixSaveMessages([]*Fix{&zero}, forged, nil) != int64(len(forged)) {t.Fatal("save messages")}
	forged[2] = forged[2]&^0x78 | 0x78
	messages := []wcase{
		{"clamped", forged, TableRefuseClamped, func(r TableReport) bool {return r.Clamped == 1}},
		{"truncated", msg[:1], TableRefuseMalformed, func(r TableReport) bool {return r.Malformed}},
	}
	for _, c := range messages {
		got[0] = Fix{N: 9, M: 9}
		n, ok := FixLoadMessages(got, voc, c.wire, &r)
		if ok || !errors.Is(r.Err(), c.want) || !c.counted(r) || (c.name == "clamped" && (n != 0 || got[0] != defaults())) {t.Fatalf("%%s: plain LoadMessages did not refuse by name: %%d %%v %%+v %%+v err=%%v", c.name, n, ok, got[0], r, r.Err())}
		var tr TableReport
		n, ok = FixLoadMessagesTolerant(got, voc, c.wire, &tr)
		if c.name == "truncated" {
			if ok {t.Fatalf("truncated batch: %%+v", tr)}
		} else if !ok || n != 1 || !c.counted(tr) || got[0].N != 10 {t.Fatalf("%%s: LoadMessagesTolerant did not keep the recovered value: %%d %%+v %%+v", c.name, n, got[0], tr)}
	}
}
`

// strictDefaultVariableProbe runs inside the generated variable unit.
const strictDefaultVariableProbe = `package probe
import ("encoding/base64";"errors";"testing";"unsafe")
func file(fields ...[]byte) []byte {var ids TableIds;buf:=make([]byte,128);w:=TableWriter{Buffer:buf,Ids:&ids};w.Put8(1);for _,f:=range fields{w.Id(VarTableFields[f[0]].Id);w.Put8(f[1]);for _,b:=range f[2:]{w.Put8(b)}};w.Put8(0);w.Trailer();return buf[:w.Offset]}
func region(n int64) []byte {raw:=make([]byte,n+64);off:=int((64-uintptr(unsafe.Pointer(&raw[0]))%64)%64);return raw[off:off+int(n)]}
func TestVariableStrict(t *testing.T) {
	t.Parallel()
	n3 := []byte{0, 4, 3, 0, 0, 0}
	req := []byte{1, 4, 4, 0, 0, 0}
	clean := file(n3, req)
	type wcase struct {name string; wire []byte; text string; want error; counted func(TableReport) bool}
	cases := []wcase{
		{"clamped", file([]byte{0, 4, 100, 0, 0, 0}, req), "{\"n\":100,\"req\":4}", TableRefuseClamped, func(r TableReport) bool {return r.Clamped == 1}},
		{"kind_mismatch", file([]byte{0, 9, 1, 0, 0, 0, 0, 0, 0, 0}, req), "{\"n\":\"x\",\"req\":4}", TableRefuseKindMismatch, func(r TableReport) bool {return r.KindMismatch == 1}},
		{"missing_required", file(n3), "{\"n\":3}", TableMissingRequired{}, func(r TableReport) bool {return r.MissingRequired == 1}},
		{"truncated", clean[:len(clean)-1], "{\"n\":3,\"req\":4", TableRefuseWireDamaged, func(r TableReport) bool {return r.Malformed}},
	}
	match := func(err, want error) bool {if _, ok := want.(TableMissingRequired); ok {var m TableMissingRequired;return errors.As(err, &m) && m.Fields == "req"};return errors.Is(err, want)}
	var r TableReport
	// the clean wire, through every plain entry point
	if root := VarLoad(region(VarLoadMeasure(clean)), clean, &r); root == nil || root.N != 3 || root.Req != 4 || r.Err() != nil {t.Fatalf("clean Load: %+v", r)}
	{var b VarBuilder;if !b.Init(){t.Fatal("init")};if !VarLoadBuilder(&b, clean, &r) || b.GetRoot().N != 3 || r.Err() != nil {t.Fatalf("clean LoadBuilder: %+v", r)};b.Shutdown()}
	{var b VarBuilder;if !b.Init(){t.Fatal("init")};if !VarFromJson(&b, []byte("{\"n\":3,\"req\":4}"), &r) || b.GetRoot().N != 3 || b.GetRoot().Req != 4 || r.Err() != nil {t.Fatalf("clean FromJson: %+v", r)};b.Shutdown()}
	if got, err := VarLoadText(base64.StdEncoding.EncodeToString(clean), nil); err != nil || got.N != 3 {t.Fatalf("clean LoadText: %v", err)}
	if got, err := VarReadStrict(clean); err != nil || got.N != 3 {t.Fatalf("clean ReadStrict: %v", err)}
	for _, c := range cases {
		size := VarLoadMeasure(c.wire)
		if size < 0 {size = 64}
		r = TableReport{Unknown: 5}
		if root := VarLoad(region(size), c.wire, &r); root != nil || !match(r.Err(), c.want) || r.Unknown != 0 || !c.counted(r) {t.Fatalf("%s: plain Load did not refuse by name: %+v err=%v", c.name, r, r.Err())}
		if root := VarLoad(region(size), c.wire, nil); root != nil {t.Fatalf("%s: plain Load with a nil report took it", c.name)}
		{var b VarBuilder;if !b.Init(){t.Fatal("init")};if VarLoadBuilder(&b, c.wire, &r) || !match(r.Err(), c.want) || !c.counted(r) {t.Fatalf("%s: plain LoadBuilder did not refuse by name: %+v", c.name, r)};b.Shutdown()}
		{var b VarBuilder;if !b.Init(){t.Fatal("init")};tr:=TableReport{};if VarFromJson(&b, []byte(c.text), &tr) || !c.counted(tr) {t.Fatalf("%s: plain FromJson did not refuse: %+v", c.name, tr)};want:=c.want;if c.name=="truncated"{want=TableRefuseMalformed};if !match(tr.Err(), want){t.Fatalf("%s: FromJson reason %v", c.name, tr.Err())};b.Shutdown()}
		if got, err := VarLoadText(base64.StdEncoding.EncodeToString(c.wire), nil); got != nil || !match(err, c.want) {t.Fatalf("%s: LoadText %v", c.name, err)}
		if got, err := VarReadStrict(c.wire); got != nil || !match(err, c.want) {t.Fatalf("%s: ReadStrict %v", c.name, err)}
		// the tolerant twins keep the recovered value, with the report counted
		var tr TableReport
		root := VarLoadTolerant(region(size), c.wire, &tr)
		var b VarBuilder;if !b.Init(){t.Fatal("init")}
		var br TableReport
		ok := VarLoadBuilderTolerant(&b, c.wire, &br)
		var jb VarBuilder;if !jb.Init(){t.Fatal("init")}
		var jr TableReport
		jok := VarFromJsonTolerant(&jb, []byte(c.text), &jr)
		if c.name == "truncated" {
			if root != nil || ok || jok || !c.counted(tr) || !c.counted(br) || !c.counted(jr) {t.Fatalf("truncated: a tolerant read took framing damage: %+v %+v %+v", tr, br, jr)}
		} else if c.name == "missing_required" {
			// §2.3: the binary walk stops on a record missing a required field, as
			// it did before this rule (the region read still hands back its root,
			// the builder read answers false), and the text read refuses it the
			// same way, since §2.3 refuses the absence in the text form too.
			if root == nil || ok || jok || !c.counted(tr) || !c.counted(br) || !c.counted(jr) {t.Fatalf("missing_required: tolerant reads: %+v %+v %+v", tr, br, jr)}
		} else if root == nil || !ok || !jok || !c.counted(tr) || !c.counted(br) || !c.counted(jr) {t.Fatalf("%s: a tolerant read did not keep the recovered value: %+v %+v %+v", c.name, tr, br, jr)}
		b.Shutdown();jb.Shutdown()
	}
}
`

// strictDefaultIntegrityProbe runs inside the generated integrity unit: a
// clamped wire under a good trailer, and a clean wire whose trailer is broken.
const strictDefaultIntegrityProbe = `package probe
import ("errors";"testing";"unsafe")
func sealed(n byte) []byte {var ids TableIds;buf:=make([]byte,128);w:=TableWriter{Buffer:buf,Ids:&ids};w.Put8(1);w.Id(SealTableFields[0].Id);w.Put8(4);for _,b:=range []byte{n,0,0,0}{w.Put8(b)};w.Put8(0);w.Trailer();m:=tableIntegritySeal(buf,int64(w.Offset));return buf[:m]}
func region(n int64) []byte {raw:=make([]byte,n+64);off:=int((64-uintptr(unsafe.Pointer(&raw[0]))%64)%64);return raw[off:off+int(n)]}
func TestIntegrityStrict(t *testing.T) {
	t.Parallel()
	clean := sealed(3)
	broken := append([]byte(nil), clean...)
	broken[len(broken)-1] ^= 0x01
	var r TableReport
	if root := SealLoad(region(SealLoadMeasure(clean)), clean, &r); root == nil || root.N != 3 || r.Err() != nil {t.Fatalf("clean Load: %+v", r)}
	{var b SealBuilder;if !b.Init(){t.Fatal("init")};if !SealLoadBuilder(&b, clean, &r) || b.GetRoot().N != 3 || r.Err() != nil {t.Fatalf("clean LoadBuilder: %+v", r)};b.Shutdown()}
	if got, err := SealReadStrict(clean); err != nil || got.N != 3 {t.Fatalf("clean ReadStrict: %v", err)}
	for _, c := range []struct{name string; wire []byte; want error; kept bool}{
		{"clamped", sealed(100), TableRefuseClamped, true},
		{"integrity_mismatch", broken, TableRefuseIntegrityMismatch, false},
	} {
		r = TableReport{}
		if root := SealLoad(region(256), c.wire, &r); root != nil || !errors.Is(r.Err(), c.want) {t.Fatalf("%s: plain Load did not refuse by name: %+v err=%v", c.name, r, r.Err())}
		{var b SealBuilder;if !b.Init(){t.Fatal("init")};r = TableReport{};if SealLoadBuilder(&b, c.wire, &r) || !errors.Is(r.Err(), c.want) {t.Fatalf("%s: plain LoadBuilder did not refuse by name: %+v", c.name, r)};b.Shutdown()}
		if got, err := SealReadStrict(c.wire); got != nil || !errors.Is(err, c.want) {t.Fatalf("%s: ReadStrict %v", c.name, err)}
		var tr, br TableReport
		root := SealLoadTolerant(region(256), c.wire, &tr)
		var b SealBuilder;if !b.Init(){t.Fatal("init")}
		ok := SealLoadBuilderTolerant(&b, c.wire, &br)
		if c.kept {
			if root == nil || !ok || tr.Clamped != 1 || br.Clamped != 1 || root.N != 10 {t.Fatalf("%s: a tolerant read did not keep the recovered value: %+v %+v", c.name, tr, br)}
		} else if root != nil || ok || !errors.Is(tr.Err(), c.want) || !errors.Is(br.Err(), c.want) {t.Fatalf("%s: a tolerant read took a broken trailer: %+v %+v", c.name, tr, br)}
		b.Shutdown()
	}
}
`
