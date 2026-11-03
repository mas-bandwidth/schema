package compiler

import "testing"

func init() {
	nonGoCarriers["js"] = append(nonGoCarriers["js"], carryOneSidedRange, carryOptionalMap)
}

// Native value/byte contracts are held by jstable's OneSidedRangeConstruct,
// OneSidedRangeFixedConstruct, and OptionalMapConstruct tests. This check holds
// the public compiler entry point, which must not refuse those implemented
// constructs before the backend gets a chance to generate them.
func TestJavaScriptCarriesOneSidedRangesAndOptionalMaps(t *testing.T) {
	for _, tc := range []struct{ name, feature, source string }{
		{"one-sided-variable", carryOneSidedRange, "package probe\ntable Root {\n a int64 | min = 0\n b uint16 | max = 9\n}\n"},
		{"one-sided-fixed", carryOneSidedRange, "package probe\nfixed table Root {\n a int64 | min = 0\n b uint16 | max = 9\n}\n"},
		{"optional-map", carryOptionalMap, "package probe\ntable Root {\n tags ?map[string(8)]int32\n nested ?map[uint8]map[uint8]int32\n}\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			u := unitFromSource(t, tc.source)
			files, err := New().Generate(u, "js", nil)
			if err != nil || len(files) == 0 {
				t.Fatalf("public js generator refused %s: files=%d err=%v", tc.feature, len(files), err)
			}
			if !carries("js", tc.feature) {
				t.Fatalf("js omitted its implemented %s registration", tc.feature)
			}
		})
	}
}
