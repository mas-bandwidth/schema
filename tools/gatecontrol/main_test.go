package main

import (
	"errors"
	"os"
	"path/filepath"
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

// TestEveryRefuseMatchesOnlyAPassLine: a control's -refuse (make/widen-18-
// gate-controls.mk) matches the leg's pass line and nothing else. A gate's
// own red names the token it did not get ("squirrel did not print
// \"TABLEGEN_OK squirrel 12\""): an unanchored -refuse matched that quote
// and failed the tablegen control on squirrel, a leg that was red on its
// planted case (fullrun ab1e1e195a).
func TestEveryRefuseMatchesOnlyAPassLine(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile(filepath.Join("..", "..", "make", "widen-18-gate-controls.mk"))
	if err != nil {
		t.Fatal(err)
	}
	refuses := regexp.MustCompile(`-refuse '([^']*)'`).FindAllStringSubmatch(string(src), -1)
	if len(refuses) == 0 {
		t.Fatal("make/widen-18-gate-controls.mk gives no control a -refuse")
	}
	for _, m := range refuses {
		pattern := strings.ReplaceAll(m[1], "$(1)", "squirrel")
		refuse, err := regexp.Compile(pattern)
		if err != nil {
			t.Fatalf("-refuse %q: %v", m[1], err)
		}
		token := strings.TrimPrefix(pattern, "(?m)^")
		pass := "PLANTED x\n" + token + "12\n"
		quoted := "PLANTED x\nFAILED: tg3:case\nsquirrel did not print \"" + token + "12\":\nFAILED: tg3:case\n"
		if !refuse.MatchString(pass) {
			t.Errorf("-refuse %q misses the pass line %q", m[1], token+"12")
		}
		if refuse.MatchString(quoted) {
			t.Errorf("-refuse %q matches the gate's own red quoting the token it did not get", m[1])
		}
	}
}
