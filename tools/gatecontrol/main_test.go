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
