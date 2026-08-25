// The C++ integer extremes: issue #95 guarded every fold site the corpus
// could reach — emitConst's ull suffix, cppInt64Lit, tableIntLit, the table
// clamps — but the MEMBER INITIALIZER path (initializer -> renderInt, plain
// and table structs both) still spelled a uint64 default above INT64_MAX as
// a bare decimal: `uint64_t huge = 18446744073709551615;` deduces unsigned
// with -Wimplicitly-unsigned-literal, a -Werror build break in the
// consumer's tree (issue #100). These tests pin the NSDMI spellings the way
// c/expr_test.go pins the C fold sites.
package cpp

import (
	"strings"
	"testing"
)

// extremesCorpus mirrors the C backend's extremes unit corpus: the two
// values with no direct decimal spelling, at every default site the C++
// backend emits — plain struct NSDMIs and table struct NSDMIs (one
// emission path, but both shapes pinned so a split regresses loudly).
const extremesCorpus = `package extremetest

const FloorLimit = -9223372036854775808
const CeilingCount uint64 = 18446744073709551615

type ExtremeProbe {
    floor_bound int64 | min = -9223372036854775808, max = 100
    ceiling_range uint64 | min = 1, max = 18446744073709551615
    floor_default int64 = -9223372036854775808
    ceiling_default uint64 = 18446744073709551615
    doubled_floor int64 | min = --FloorLimit, max = 100
}

table ExtremeRow {
    clamped_floor int64 | min = -9223372036854775808, max = 100
    clamped_ceiling uint64 | min = 1, max = 18446744073709551614
    floor_def int64 = -9223372036854775808
    ceiling_def uint64 = 18446744073709551615
}
`

func generateExtremesCorpus(t *testing.T) (data, wire, table string) {
	t.Helper()
	u := unitFromSources(t, map[string]string{"Extreme.schema": extremesCorpus})
	files, err := Generate(u, Options{})
	if err != nil {
		t.Fatal(err)
	}
	tables, err := GenerateTable(u)
	if err != nil {
		t.Fatal(err)
	}
	return string(files["Extreme.h"]), string(files["ExtremeWire.h"]), string(tables["ExtremeTable.h"])
}

// TestExtremeDefaultInitializers pins the NSDMI spellings: an above-INT64_MAX
// uint64 default carries the ull suffix (the same guard emitConst applies to
// a constant of the same value), and the INT64_MIN default keeps its guarded
// arithmetic form — in the plain struct and the table struct alike.
func TestExtremeDefaultInitializers(t *testing.T) {
	data, _, _ := generateExtremesCorpus(t)
	for _, want := range []string{
		// the constant of the same value: emitConst's guard, the positive control
		"inline constexpr uint64_t CeilingCount = 18446744073709551615ull; // = 18446744073709551615",
		// plain struct NSDMIs
		"int64_t floor_default = ( -9223372036854775807ll - 1 );",
		"uint64_t ceiling_default = 18446744073709551615ull;",
		// table struct NSDMIs — same emission path, pinned separately
		"int64_t floor_def = ( -9223372036854775807ll - 1 );",
		"uint64_t ceiling_def = 18446744073709551615ull;",
	} {
		if !strings.Contains(data, want) {
			t.Errorf("generated data header must contain %q", want)
		}
	}
}

// TestExtremeSpellingsAbsentCpp proves the broken spellings appear NOWHERE in
// the generated CODE: the raw INT64_MIN decimal (whose literal half is out of
// long long range no matter the suffix), and any above-INT64_MAX decimal not
// immediately suffixed ull. Comments are stripped first — the wire-range
// comments legitimately carry the bare decimals as documentation.
func TestExtremeSpellingsAbsentCpp(t *testing.T) {
	data, wire, table := generateExtremesCorpus(t)
	for name, text := range map[string]string{"data": data, "wire": wire, "table": table} {
		code := stripLineComments(text)
		if strings.Contains(code, "-9223372036854775808") {
			t.Errorf("%s header spells the unspellable INT64_MIN literal", name)
		}
		for _, huge := range []string{"18446744073709551615", "18446744073709551614", "9223372036854775808"} {
			for at := 0; ; {
				i := strings.Index(code[at:], huge)
				if i < 0 {
					break
				}
				at += i + len(huge)
				if at >= len(code) || code[at] != 'u' {
					t.Errorf("%s header carries the above-INT64_MAX decimal %s without the ull suffix", name, huge)
				}
			}
		}
	}
}

// stripLineComments drops everything from `//` to end of line — the C++
// headers carry the folded bounds in prose comments, which are not code.
func stripLineComments(text string) string {
	var b strings.Builder
	for line := range strings.SplitSeq(text, "\n") {
		if i := strings.Index(line, "//"); i >= 0 {
			line = line[:i]
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}
