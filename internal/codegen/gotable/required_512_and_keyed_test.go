package gotable

import (
	"fmt"
	"strings"
	"testing"
)

// TestGoRequiredKeyedArrayAtZeroRidesHeader pins that an enum-keyed array
// marked `| required` rides the wire with its keyed header (kind 16, element
// kind, zero pairs) even when every element is at its default, and that an
// empty record lacking the field is refused as missing_required (§2.3, §2.4).
// The same rule applies to a required nested table whose fields are all at
// default.
func TestGoRequiredKeyedArrayAtZeroRidesHeader(t *testing.T) {
	t.Parallel()
	const schema = `package probe
enum Key { A, B }
table Kid {
	x int32
}
table Keyed {
	values [Key]int32 | required
}
table Parent {
	kid Kid | required
}
table Other {
	ver uint8
}
table RequiredEmpty {
	value Other | required
}
`
	t.Run("emitted", func(t *testing.T) {
		t.Parallel()
		files, err := Generate(unitFrom(t, schema))
		if err != nil {
			t.Fatal(err)
		}
		var src strings.Builder
		for _, data := range files {
			src.Write(data)
		}
		s := src.String()
		for _, forbidden := range []string{"payload.Offset > 3", "if n > 1 {\n\t\tif w.Measuring"} {
			if strings.Contains(s, forbidden) {
				t.Errorf("required keyed array or nested table still elides when empty: emitted source contains %q", forbidden)
			}
		}
	})
	const body = `package probe
import (
	"encoding/hex"
	"errors"
	"testing"
)
func TestKeyedZeroWireAndStrictRefusal(t *testing.T) {
	var v Keyed
	n := KeyedMeasure(&v)
	if n <= 0 {
		t.Fatalf("KeyedMeasure at zero = %d, want > 0", n)
	}
	buf := make([]byte, n)
	if got := KeyedSave(&v, buf); got != n {
		t.Fatalf("KeyedSave = %d, want %d", got, n)
	}
	const wantHex = "01011002040000fb674d1acf7b27210100000000000000"
	if gotHex := hex.EncodeToString(buf); gotHex != wantHex {
		t.Fatalf("KeyedSave at zero = %s, want %s", gotHex, wantHex)
	}
	got, err := KeyedReadStrict(buf)
	if err != nil || got == nil || *got != v {
		t.Fatalf("KeyedReadStrict round-trip: got=%+v err=%v", got, err)
	}
	empty, _ := hex.DecodeString("01000000000000000000")
	if _, err := KeyedReadStrict(empty); err == nil {
		t.Fatal("KeyedReadStrict accepted empty wire missing required values")
	} else {
		var m TableMissingRequired
		if !errors.As(err, &m) || m.Fields != "values" {
			t.Fatalf("KeyedReadStrict empty wire err = %v (%T), want missing_required: values", err, err)
		}
	}

	var p Parent
	pn := ParentMeasure(&p)
	if pn <= 0 {
		t.Fatalf("ParentMeasure at zero = %d, want > 0", pn)
	}
	pbuf := make([]byte, pn)
	if got := ParentSave(&p, pbuf); got != pn {
		t.Fatalf("ParentSave = %d, want %d", got, pn)
	}
	pgot, perr := ParentReadStrict(pbuf)
	if perr != nil || pgot == nil || *pgot != p {
		t.Fatalf("ParentReadStrict round-trip: got=%+v err=%v", pgot, perr)
	}
	if _, err := ParentReadStrict(empty); err == nil {
		t.Fatal("ParentReadStrict accepted empty wire missing required kid")
	}

	var re RequiredEmpty
	RequiredEmptyReset(&re)
	ren := RequiredEmptyMeasure(&re)
	if ren <= 0 {
		t.Fatalf("RequiredEmptyMeasure after Reset = %d, want > 0", ren)
	}
	rebuf := make([]byte, ren)
	if got := RequiredEmptySave(&re, rebuf); got != ren {
		t.Fatalf("RequiredEmptySave = %d, want %d", got, ren)
	}
	const wantRequiredEmptyHex = "01010d010000ea0ce83094fde47c0100000000000000"
	if gotHex := hex.EncodeToString(rebuf); gotHex != wantRequiredEmptyHex {
		t.Fatalf("RequiredEmptySave after Reset = %s, want %s", gotHex, wantRequiredEmptyHex)
	}
	regot, reerr := RequiredEmptyReadStrict(rebuf)
	if reerr != nil || regot == nil || *regot != re {
		t.Fatalf("RequiredEmptyReadStrict round-trip: got=%+v err=%v", regot, reerr)
	}
}
`
	t.Run("compiled", func(t *testing.T) {
		t.Parallel()
		runGenerated(t, schema, body)
	})
}

// TestGoRequiredPast512Fields pins that JSON required-field enforcement,
// guarded required fields, variable-class JSON reads, and duplicate-key
// tracking work for tables with more than 512 fields without silently skipping
// indices >= 512.
func TestGoRequiredPast512Fields(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	sb.WriteString("package probe\ntable Leaf {\n\tv int32 | required\n}\ntable Wide {\n\tgate bool\n")
	for i := 1; i <= 514; i++ {
		fmt.Fprintf(&sb, "\tf%d int32\n", i)
	}
	sb.WriteString("\treq515 int32 | required\n")
	sb.WriteString("\tif gate {\n\t\tguarded516 int32 | required\n\t}\n")
	sb.WriteString("\tptr517 *Leaf\n")
	sb.WriteString("\treq518 int32 | required\n")
	sb.WriteString("}\n")
	schema := sb.String()

	t.Run("emitted", func(t *testing.T) {
		t.Parallel()
		files, err := Generate(unitFrom(t, schema))
		if err != nil {
			t.Fatal(err)
		}
		j := string(files["ProbeTableJson.go"])
		for _, forbidden := range []string{"i >= 512", "index < 512", "seen *[8]uint64"} {
			if strings.Contains(j, forbidden) {
				t.Errorf("JSON required walker still caps at 512 fields: found %q", forbidden)
			}
		}
		for _, want := range []string{"seen []uint64", "make([]uint64, words)"} {
			if !strings.Contains(j, want) {
				t.Errorf("JSON required walker missing %q", want)
			}
		}
	})

	const body = `package probe
import (
	"testing"
)
func TestWideRequiredPast512(t *testing.T) {
	// 1. Missing req515 and req518 on {} must refuse naming both in declaration order.
	{
		var b WideBuilder
		if !b.Init() { t.Fatal("init") }
		defer b.Shutdown()
		var r TableReport
		if WideFromJSON(&b, []byte("{}"), &r) {
			t.Fatal("WideFromJSON({}) succeeded, want missing_required refusal")
		}
		if !r.Refused() || r.Reason != "missing_required: req515, req518" {
			t.Fatalf("WideFromJSON({}) report = %+v, want MissingRequired=1 reason \"missing_required: req515, req518\"", r)
		}
	}
	// 2. Missing only req518 (index >= 512) must refuse naming req518.
	{
		var b WideBuilder
		if !b.Init() { t.Fatal("init") }
		defer b.Shutdown()
		var r TableReport
		if WideFromJSON(&b, []byte("{\"req515\":10}"), &r) {
			t.Fatal("WideFromJSON({\"req515\":10}) succeeded, want missing_required: req518")
		}
		if !r.Refused() || r.Reason != "missing_required: req518" {
			t.Fatalf("WideFromJSON({\"req515\":10}) report = %+v, want missing_required: req518", r)
		}
	}
	// 3. Guard off (gate=false): req515 and req518 present, guarded516 absent -> succeeds.
	{
		var b WideBuilder
		if !b.Init() { t.Fatal("init") }
		defer b.Shutdown()
		var r TableReport
		if !WideFromJSON(&b, []byte("{\"req515\":10,\"req518\":20}"), &r) || r != (TableReport{}) {
			t.Fatalf("WideFromJSON with guard off failed: report=%+v", r)
		}
		v := b.GetRoot()
		if v.Req515 != 10 || v.Req518 != 20 {
			t.Fatalf("got Req515=%d Req518=%d", v.Req515, v.Req518)
		}
	}
	// 4. Guard on (gate=true): guarded516 (index 516) missing -> refuses naming guarded516.
	{
		var b WideBuilder
		if !b.Init() { t.Fatal("init") }
		defer b.Shutdown()
		var r TableReport
		if WideFromJSON(&b, []byte("{\"gate\":true,\"req515\":10,\"req518\":20}"), &r) {
			t.Fatal("WideFromJSON with gate=true and missing guarded516 succeeded")
		}
		if !r.Refused() || r.Reason != "missing_required: guarded516" {
			t.Fatalf("WideFromJSON missing guarded516 report = %+v, want missing_required: guarded516", r)
		}
	}
	// 5. Guard on (gate=true) and guarded516 present, plus variable pointer ptr517 missing required v -> refuses.
	{
		var b WideBuilder
		if !b.Init() { t.Fatal("init") }
		defer b.Shutdown()
		var r TableReport
		if WideFromJSON(&b, []byte("{\"gate\":true,\"req515\":10,\"guarded516\":30,\"ptr517\":{},\"req518\":20}"), &r) {
			t.Fatal("WideFromJSON with empty ptr517 succeeded, want missing_required: v")
		}
		if !r.Refused() || r.Reason != "missing_required: v" {
			t.Fatalf("WideFromJSON empty ptr517 report = %+v, want missing_required: v", r)
		}
	}
	// 6. Duplicate key at index >= 512 is last-wins and counted on r.Refusal.
	{
		var b WideBuilder
		if !b.Init() { t.Fatal("init") }
		defer b.Shutdown()
		var r TableReport
		if WideFromJSON(&b, []byte("{\"req515\":10,\"req515\":11,\"req518\":20}"), &r) || r.Err() == nil {
			t.Fatalf("WideFromJSON took a repeated key: %+v", r)
		}

	}
}
`
	t.Run("compiled", func(t *testing.T) {
		t.Parallel()
		runGenerated(t, schema, body)
	})
}
