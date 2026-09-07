package compiler

import (
	"strings"
	"testing"
)

// The Rust cross-language fixture found two missing C++ declarations when
// the only wide storage was in an arm. Its build/run gate exercises both.
func TestCppWideOnlyArmsCarryStorageAndRange(t *testing.T) {
	u := unitFromSource(t, `package probe
union Value {
 number uint128
 fraction ufixed(1, 127) | min = 0, max = 1
}
table Root { value Value }
`)
	out, err := New().Generate(u, "cpp", nil)
	if err != nil {
		t.Fatal(err)
	}
	header := string(out["ProbeTable.h"])
	for _, want := range []string{`#include "serialize.h"`, `static const TableWideRange Value_fraction_wide =`, `&Value_fraction_wide`} {
		if !strings.Contains(header, want) {
			t.Errorf("wide-only arm header lacks %s", want)
		}
	}
}
