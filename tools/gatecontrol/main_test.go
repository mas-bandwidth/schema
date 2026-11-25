package main

import (
	"errors"
	"regexp"
	"strings"
	"testing"
)

// The control is green only on a red run that carries the plant and, when
// asked, the planted case's own line; every other run is named.
func TestVerdictNamesEveryWayAControlFails(t *testing.T) {
	t.Parallel()
	red := errors.New("exit status 2")
	want := regexp.MustCompile(`go / report: fixedevo_v1_as_v3`)
	refuse := regexp.MustCompile(`TABLEGEN_OK go `)
	cases := []struct {
		name string
		err  error
		log  string
		want string // a substring of the verdict; "" is a pass
	}{
		{"green gate", nil, "PLANTED fixed\n", "stayed green"},
		{"red before the plant", red, "build failed\n", "before the fixed plant"},
		{"red elsewhere", red, "PLANTED fixed\n  go / report: other_case\n", "not on the planted case"},
		{"leg passed", red, "PLANTED fixed\n  go / report: fixedevo_v1_as_v3 x\nTABLEGEN_OK go 4\n", "reported a pass"},
		{"planted red", red, "PLANTED fixed\n  go / report: fixedevo_v1_as_v3 x\n", ""},
	}
	for _, c := range cases {
		got := verdict("fixed", c.err, []byte(c.log), want, refuse)
		if (c.want == "") != (got == "") || !strings.Contains(got, c.want) {
			t.Errorf("%s: verdict %q, want %q", c.name, got, c.want)
		}
	}
}

// The reviewer's plant (2026-11-24): node swapped for /bin/false. The plant is
// applied, the leg dies before it compares anything, and the run is red; the
// bitphase control must fail, because no line names a planted vector.
func TestARedThatComparedNothingFailsTheControl(t *testing.T) {
	t.Parallel()
	red := errors.New("exit status 2")
	want := regexp.MustCompile(`FAILED: .*(valid_p[0-9]+_v0|hostile_align_p[0-9]+)\.bin`)
	refuse := regexp.MustCompile(`BITPHASE_OK js `)
	dead := "PLANTED bitphase: 4 valid and hostile vectors traded bytes (valid_p0_v0.bin<->hostile_align_p0.bin); every leg must now go red\njs checked: exit status 1\n"
	if got := verdict("bitphase", red, []byte(dead), want, refuse); !strings.Contains(got, "not on the planted case") {
		t.Errorf("a leg that died before comparing passed its control: verdict %q", got)
	}
	caught := dead + "FAILED: stream read valid_p0_v0.bin\n"
	if got := verdict("bitphase", red, []byte(caught), want, refuse); got != "" {
		t.Errorf("a leg red on its planted vector failed its control: %q", got)
	}
}
