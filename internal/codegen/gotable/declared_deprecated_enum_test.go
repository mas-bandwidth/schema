package gotable

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDeclaredDeprecatedEnumCook(t *testing.T) {
	t.Parallel()
	fixture := "../../../testdata/conformance/feature-witnesses/declared-deprecated-enum"
	schema, err := os.ReadFile(filepath.Join(fixture, "Probe.schema"))
	if err != nil {
		t.Fatal(err)
	}
	cases, err := os.ReadFile(filepath.Join(fixture, "cases.txt"))
	if err != nil {
		t.Fatal(err)
	}
	want := make(map[string]string)
	for _, line := range strings.Split(string(cases), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		if len(fields) != 2 || want[fields[0]] != "" {
			t.Fatalf("invalid expected row %q", line)
		}
		want[fields[0]] = fields[1]
	}

	runGenerated(t, string(schema), fmt.Sprintf(`package depr

import (
	"bytes"
	"encoding/hex"
	"strconv"
	"testing"
)

func TestFixedRootCook(t *testing.T) {
	expected := %#v
	for _, variant := range []struct {
		name string
		value Phase
	}{
		{"next", PhaseNext},
		{"old", PhaseOld},
	} {
		value := FixedRoot{Phase: variant.value}
		row := "sf26_cook_measure_" + variant.name
		want, err := strconv.ParseInt(expected[row], 10, 64)
		if err != nil || want != 88 {
			t.Fatalf("%%s: fixture has invalid extent %%q", row, expected[row])
		}
		if got := FixedRootCookMeasure(&value); got != want {
			t.Fatalf("%%s: Measure = %%d, want %%d", row, got, want)
		}
		if got, reason := FixedRootCookMeasureReason(&value); got != want || reason != nil {
			t.Fatalf("%%s: MeasureReason = (%%d, %%v), want (%%d, nil)", row, got, reason, want)
		}
		for _, byteOrder := range []struct {
			name string
			value TableByteOrder
		}{
			{"le", TableByteOrderLittle},
			{"be", TableByteOrderBig},
		} {
			row := "sf26_cook_" + variant.name + "_" + byteOrder.name
			t.Run(row, func(t *testing.T) {
				initial := bytes.Repeat([]byte{0x5a}, int(want))
				output := bytes.Clone(initial)
				ok := FixedRootCookFrom(&value, output, byteOrder.value)
				if variant.value == PhaseOld {
					if expected[row] != "refused" {
						t.Fatalf("invalid refusal fixture %%q", expected[row])
					}
					if ok || !bytes.Equal(output, initial) {
						t.Fatalf("CookFrom = %%t, output changed = %%t, want refusal with unchanged output", ok, !bytes.Equal(output, initial))
					}
					return
				}
				wire, err := hex.DecodeString(expected[row])
				if err != nil || len(wire) != len(output) {
					t.Fatalf("invalid fixture %%q", expected[row])
				}
				if !ok || !bytes.Equal(output, wire) {
					t.Fatalf("CookFrom = %%t, bytes = %%x, want %%x", ok, output, wire)
				}
			})
		}
	}
}
`, want))
}
