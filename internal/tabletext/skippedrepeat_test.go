package tabletext_test

import "testing"

// A REPEATED KEY REFUSES IN EVERY OBJECT OF THE TEXT (docs/SPEC-TABLES.md
// §16.2, §16.12), the objects the walk skips included: a value under an
// unknown key is still JSON this layer reads, and a repeated name in it is
// the layer's `malformed`. The controls skip the same objects with distinct
// keys and count the unknown.
func TestRepeatedKeyInASkippedValueRefuses(t *testing.T) {
	for _, tc := range []struct{ repeat, control string }{
		{`{ "mystery": { "a": 1, "a": 2 } }`, `{ "mystery": { "a": 1, "b": 2 } }`},
		{`{ "mystery": [ { "a": 1, "a": 1 } ] }`, `{ "mystery": [ { "a": 1 }, { "a": 1 } ] }`},
		{`{ "mystery": { "x": { "a": {}, "a": {} } } }`, `{ "mystery": { "x": { "a": {} }, "a": {} } }`},
	} {
		_, _, r := read(t, "GlobalSettings", tc.repeat)
		refuses(t, r, "malformed")
		m, inst, r := read(t, "GlobalSettings", tc.control)
		if r.Refused || r.Unknown != 1 {
			t.Fatalf("%s: want one unknown, got %+v", tc.control, r)
		}
		if _, err := m.Write(inst); err != nil {
			t.Fatal(err)
		}
	}
}
