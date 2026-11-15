package rusttable

// The emitted message-form module is held by the clippy gate like every other
// generated module: it carries no crate-wide allow that switches the gate off,
// and it emits no state an allow would otherwise hide. The body walker emits
// its widen counter only for a field kind that can widen, an unsigned value
// carries a lower clamp only when its declared lower bound can be violated,
// and the form-1 array loader emits each extent guard once.

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
)

func generatedMessageModule(t *testing.T, src string) string {
	t.Helper()
	files := generate(t, src)
	for name, data := range files {
		if strings.HasSuffix(name, "_message.rs") {
			return string(data)
		}
	}
	return ""
}

func lineContaining(src, needle string) string {
	for _, line := range strings.Split(src, "\n") {
		if strings.Contains(line, needle) {
			return line
		}
	}
	return "(not found)"
}

func TestMessageFormModuleHoldsTheClippyRule(t *testing.T) {
	t.Parallel()

	const noWiden = `package msgclippy

fixed table Unit { v uint8 }

fixed table Plain
{
    flag  bool
    small uint8
    name  string(8)
    units [..4]Unit
    nums  [..4]uint8
}
`
	t.Run("no crate-wide allow and no dead widen", func(t *testing.T) {
		t.Parallel()
		msg := generatedMessageModule(t, noWiden)
		if msg == "" {
			t.Fatal("the unit emitted no message module")
		}
		if strings.Contains(msg, "#![allow") {
			t.Errorf("message module carries a crate-wide allow: %s", lineContaining(msg, "#![allow"))
		}
		for _, dead := range []string{"let mut widened", "if widened {"} {
			if strings.Contains(msg, dead) {
				t.Errorf("message module emits %q for a field kind that cannot widen: %s", dead, lineContaining(msg, dead))
			}
		}
	})

	const lowerClamp = `package msgclippy

fixed table Plain
{
    v uint16 | min = 0, max = 1000
}
`
	t.Run("unsigned lower clamp only where it can fire", func(t *testing.T) {
		t.Parallel()
		msg := generatedMessageModule(t, lowerClamp)
		if msg == "" {
			t.Fatal("the unit emitted no message module")
		}
		if strings.Contains(msg, "x < 0") {
			t.Errorf("message module clamps an unsigned value below zero: %s", lineContaining(msg, "x < 0"))
		}
		if !strings.Contains(msg, "x > 1000") {
			t.Error("message module lost the upper clamp the declared range needs")
		}
	})

	const listRoot = `package msgclippy

table Holder
{
    data []uint8
    tags []uint32
}
`
	t.Run("array extent guards emitted once", func(t *testing.T) {
		t.Parallel()
		files := generate(t, listRoot)
		data, ok := files["probe_table.rs"]
		if !ok {
			names := make([]string, 0, len(files))
			for name := range files {
				names = append(names, name)
			}
			t.Fatalf("generated files do not include probe_table.rs: %v", names)
		}
		src := string(data)
		for _, guard := range []string{
			`if *off >= sub { report.damaged = true; break; }`,
			`if *off != sub { report.damaged = true; }`,
		} {
			twice := regexp.MustCompile(regexp.QuoteMeta(guard) + `\s*\n\s*` + regexp.QuoteMeta(guard))
			if twice.MatchString(src) {
				t.Errorf("array loader emits %s twice in a row", fmt.Sprintf("%q", guard))
			}
		}
	})
}
