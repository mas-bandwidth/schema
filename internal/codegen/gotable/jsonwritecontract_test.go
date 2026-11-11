package gotable

import "testing"

// TestGoJSONWriterHoldsWriteContract holds the generated Go text writer to the
// binary writers' write contract (docs/SPEC.md §5.1 rows 3, 4 and 10,
// docs/SPEC-TABLES.md §16.3): a counted array's count past its bound or
// negative, a bounded string's or bytes' length past its bound or negative,
// and a compressed or ranged float off its range are REFUSED (-1) rather than
// clamped or truncated. The values on the bound and at each end of the range
// are written and read back, so a writer that refused everything would fail.
func TestGoJSONWriterHoldsWriteContract(t *testing.T) {
	t.Parallel()
	runGeneratedTables(t, `package probe
fixed table Contract {
    name  string(8)
    marks [..3]int32
    q     float32 | min = 0, max = 10, resolution = 0.01
}
`, `package probe
import "testing"

func contractValue() Contract {
	var v Contract
	ContractReset(&v)
	v.NameLength = 3
	copy(v.Name[:], "abc")
	v.MarksCount = 2
	v.Marks[0], v.Marks[1] = 1, 2
	v.Q = 1
	return v
}

func TestContractWriter(t *testing.T) {
	refused := []struct {
		what string
		edit func(*Contract)
	}{
		{"length past N", func(v *Contract) { v.NameLength = 9 }},
		{"length far past N", func(v *Contract) { v.NameLength = 1000 }},
		{"negative length", func(v *Contract) { v.NameLength = -1 }},
		{"count past Max", func(v *Contract) { v.MarksCount = 4 }},
		{"negative count", func(v *Contract) { v.MarksCount = -1 }},
		{"q above max", func(v *Contract) { v.Q = 11 }},
		{"q below min", func(v *Contract) { v.Q = -1 }},
	}
	for _, c := range refused {
		v := contractValue()
		c.edit(&v)
		if n := ContractToJSONMeasure(&v); n != -1 {
			t.Fatalf("%s: ToJSONMeasure = %d, want the refusal -1", c.what, n)
		}
		var sink [4096]byte
		if n := ContractToJSON(&v, sink[:]); n != -1 {
			t.Fatalf("%s: ToJSON = %d, want the refusal -1", c.what, n)
		}
		if text, err := ContractSaveText(&v); err == nil || text != "" {
			t.Fatalf("%s: SaveText = %q, %v, want the refusal", c.what, text, err)
		}
	}

	accepted := []struct {
		what string
		edit func(*Contract)
	}{
		{"empty", func(v *Contract) { v.NameLength = 0; v.MarksCount = 0; v.Q = 0 }},
		{"length on N", func(v *Contract) { v.NameLength = 8; copy(v.Name[:], "12345678"); v.Q = 0 }},
		{"count on Max", func(v *Contract) { v.MarksCount = 3; v.Marks[2] = 3; v.Q = 0 }},
		{"q on min", func(v *Contract) { v.Q = 0 }},
		{"q on max", func(v *Contract) { v.Q = 10 }},
	}
	for _, c := range accepted {
		v := contractValue()
		c.edit(&v)
		n := ContractToJSONMeasure(&v)
		if n <= 0 {
			t.Fatalf("%s: ToJSONMeasure = %d, want the written size", c.what, n)
		}
		out := make([]byte, n)
		if got := ContractToJSON(&v, out); got != n {
			t.Fatalf("%s: ToJSON = %d, want the measured %d", c.what, got, n)
		}
		var got Contract
		var report TableReport
		if !ContractFromJSON(&got, out, &report) {
			t.Fatalf("%s: FromJSON refused its own text: %+v\n%s", c.what, report, out)
		}
		if got.NameLength != v.NameLength || got.MarksCount != v.MarksCount || got.Q != v.Q {
			t.Fatalf("%s: read back %+v, want %+v", c.what, got, v)
		}
		for i := int32(0); i < v.MarksCount; i++ {
			if got.Marks[i] != v.Marks[i] {
				t.Fatalf("%s: marks[%d] read back %d, want %d", c.what, i, got.Marks[i], v.Marks[i])
			}
		}
		if got.NameLength > 0 && string(got.Name[:got.NameLength]) != string(v.Name[:v.NameLength]) {
			t.Fatalf("%s: name read back %q, want %q", c.what, got.Name[:got.NameLength], v.Name[:v.NameLength])
		}
		if text, err := ContractSaveText(&v); err != nil || text == "" {
			t.Fatalf("%s: SaveText = %q, %v, want the writer's text", c.what, text, err)
		}
	}
}
`)
}
