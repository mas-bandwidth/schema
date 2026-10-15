package gotable

import "testing"

// TestGoTableWritersRefuseWhatTheirReadersRefuse pins the rule of
// docs/USAGE.md "Writes are the caller's responsibility" (the Go row) and
// docs/FIXED-FORM-ALGORITHM.md §3.1: a Go table save, variable and fixed,
// refuses by name what its own reader would clamp or refuse. Out-of-contract
// values are a value off its min/max, a count past Max, a length past N,
// ill-formed UTF-8 and a NUL in text. A refused save returns -1 and its Reason
// twin returns TableRefuseInvalidValue. Every save it ACCEPTS reloads with no
// clamp and no refusal, so the writer and the reader hold one contract (the
// not-vacuous control).
func TestGoTableWritersRefuseWhatTheirReadersRefuse(t *testing.T) {
	t.Parallel()
	t.Run("variable", func(t *testing.T) {
		t.Parallel()
		runGenerated(t, `package probe
table Rec {
    health int32 | min = 0, max = 1000
    name   string(8)
}
`, `package probe
import ("testing")

func good() Rec {
	var v Rec
	RecReset(&v)
	v.Health = 1000
	v.NameLength = 3
	copy(v.Name[:], "abc")
	return v
}

func TestVariableWriterRefuses(t *testing.T) {
	// THE NOT-VACUOUS CONTROL: lawful saves round-trip with no clamp, no refusal.
	for _, name := range []string{"abc", "\u00e9\u00e9", "12345678", ""} {
		v := good()
		v.NameLength = int32(len(name))
		copy(v.Name[:], name)
		b := make([]byte, 4096)
		n := RecSave(&v, b)
		if n <= 0 {
			t.Fatalf("lawful save %q refused: %d", name, n)
		}
		if size, err := RecMeasureReason(&v); err != nil || size != n {
			t.Fatalf("lawful measure %q: %d %v, save wrote %d", name, size, err, n)
		}
		var r TableReport
		got := RecLoad(make([]byte, 4096), b[:n], &r)
		if got == nil || r != (TableReport{}) || got.Health != 1000 || string(got.Name[:got.NameLength]) != name {
			t.Fatalf("lawful %q did not reload clean: %+v %+v", name, got, r)
		}
	}
	for _, edge := range []int32{0, 1000} {
		v := good()
		v.Health = edge
		if n := RecSave(&v, make([]byte, 4096)); n <= 0 {
			t.Fatalf("health %d is on the bound and must save, got %d", edge, n)
		}
	}
	cases := []struct {
		what string
		edit func(*Rec)
	}{
		{"health over max", func(v *Rec) { v.Health = 2000 }},
		{"health under min", func(v *Rec) { v.Health = -1 }},
		{"length past N", func(v *Rec) { v.NameLength = 12 }},
		{"negative length", func(v *Rec) { v.NameLength = -1 }},
		{"ill-formed UTF-8", func(v *Rec) { v.NameLength = 2; v.Name[0], v.Name[1] = 0xff, 0xfe }},
		{"truncated UTF-8", func(v *Rec) { v.NameLength = 1; v.Name[0] = 0xc3 }},
		{"a NUL in text", func(v *Rec) { v.NameLength = 3; copy(v.Name[:], "a\x00b") }},
	}
	for _, c := range cases {
		v := good()
		c.edit(&v)
		if n := RecSave(&v, make([]byte, 4096)); n != -1 {
			t.Fatalf("variable save accepted %s (returned %d); its reader clamps or refuses it", c.what, n)
		}
		if size, err := RecMeasureReason(&v); size != -1 || err != TableRefuseInvalidValue {
			t.Fatalf("variable measure of %s must be refused by name: %d %v", c.what, size, err)
		}
	}
}
`)
	})
	t.Run("fixed", func(t *testing.T) {
		t.Parallel()
		runGenerated(t, `package probe
fixed table Host {
    level int32 | min = 0, max = 1000
    marks [..4]int32
    name  string(8)
    tail  int32 = 7
}
`, `package probe
import ("testing")

func good() Host {
	var v Host
	HostReset(&v)
	v.Level = 1000
	v.MarksCount = 2
	v.Marks[0], v.Marks[1] = 11, 22
	v.NameLength = 3
	copy(v.Name[:], "abc")
	return v
}

func saves(v Host) (fixed, variable int64) {
	return HostFixedSave([]Host{v}, make([]byte, HostFixedMeasure(1))), HostSave(&v, make([]byte, 4096))
}

func TestFixedWriterRefuses(t *testing.T) {
	// THE NOT-VACUOUS CONTROL.
	for _, name := range []string{"abc", "\u00e9\u00e9", "12345678", ""} {
		v := good()
		v.NameLength = int32(len(name))
		copy(v.Name[:], name)
		b := make([]byte, HostFixedMeasure(1))
		if n := HostFixedSave([]Host{v}, b); n != int64(len(b)) {
			t.Fatalf("lawful fixed save %q refused: %d", name, n)
		}
		got := make([]Host, 1)
		var r TableReport
		if n := HostFixedLoad(got, b, make([]TableFixedEntry, 256), &r); n != 1 || r != (TableReport{}) || got[0].MarksCount != 2 || got[0].Level != 1000 || string(got[0].Name[:got[0].NameLength]) != name {
			t.Fatalf("lawful %q did not reload clean: %d %+v %+v", name, n, got[0], r)
		}
		vb := make([]byte, 4096)
		vn := HostSave(&v, vb)
		var vgot Host
		var vr TableReport
		if vn <= 0 || !HostLoad(&vgot, vb[:vn], &vr) || vr != (TableReport{}) || string(vgot.Name[:vgot.NameLength]) != name {
			t.Fatalf("lawful form-1 %q did not reload clean: %d %+v %+v", name, vn, vgot, vr)
		}
	}
	for _, count := range []int32{0, 4} {
		v := good()
		v.MarksCount = count
		if f, w := saves(v); f <= 0 || w <= 0 {
			t.Fatalf("count %d is within Max and must save: %d %d", count, f, w)
		}
	}
	cases := []struct {
		what string
		edit func(*Host)
	}{
		{"level over max", func(v *Host) { v.Level = 2000 }},
		{"level under min", func(v *Host) { v.Level = -5 }},
		{"count past Max", func(v *Host) { v.MarksCount = 9 }},
		{"negative count", func(v *Host) { v.MarksCount = -1 }},
		{"length past N", func(v *Host) { v.NameLength = 12 }},
		{"negative length", func(v *Host) { v.NameLength = -1 }},
		{"ill-formed UTF-8", func(v *Host) { v.NameLength = 2; v.Name[0], v.Name[1] = 0xff, 0xfe }},
		{"a NUL in text", func(v *Host) { v.NameLength = 3; copy(v.Name[:], "a\x00b") }},
	}
	for _, c := range cases {
		v := good()
		c.edit(&v)
		f, w := saves(v)
		if f != -1 {
			t.Fatalf("fixed save accepted %s (returned %d); its reader clamps or refuses it", c.what, f)
		}
		if w != -1 {
			t.Fatalf("form-1 save of a fixed table accepted %s (returned %d)", c.what, w)
		}
		if size, err := HostMeasureReason(&v); size != -1 || err != TableRefuseInvalidValue {
			t.Fatalf("measure of %s must be refused by name: %d %v", c.what, size, err)
		}
	}
	// A bad record anywhere in a batch refuses the batch, and writes nothing.
	batch := []Host{good(), good(), good()}
	batch[2].MarksCount = 9
	buf := make([]byte, HostFixedMeasure(3))
	if n := HostFixedSave(batch, buf); n != -1 {
		t.Fatalf("a batch with one out-of-contract record was accepted: %d", n)
	}
	for i, b := range buf {
		if b != 0 {
			t.Fatalf("a refused batch must leave the buffer untouched; byte %d is %#x", i, b)
		}
	}
}
`)
	})
}
