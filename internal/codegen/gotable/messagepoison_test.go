package gotable

// A REFUSED MESSAGE BATCH LEAVES THE CALLER'S BUFFER UNTOUCHED (docs/SPEC.md
// §5.1): SaveMessages measures the whole batch before its first byte, so a
// value its declaration does not admit (here the second value's ranged
// integer, after a first value that is lawful), a count or length past its
// bound, or a buffer too short for the batch answers -1 and every byte of the
// buffer keeps the poison the caller put there. Both the plain and the
// integrity form are held.

import (
	"strings"
	"testing"
)

const messagePoisonSchema = `package probe
fixed table Plain
{
    label string(8)
    level int32 | min = 0, max = 10
    picks [..4]int32
}
fixed table Sealed | integrity = "crc32c"
{
    label string(8)
    level int32 | min = 0, max = 10
    picks [..4]int32
}
`

const messagePoisonProbe = `package probe

import "testing"

func poisoned(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = 0xA5
	}
	return b
}

func intact(t *testing.T, what string, b []byte) {
	t.Helper()
	for i, c := range b {
		if c != 0xA5 {
			t.Fatalf("%s: the refused batch wrote byte %d (0x%02x): the poison is gone", what, i, c)
		}
	}
}

func TestPlainRefusalLeavesThePoison(t *testing.T) {
	good := &Plain{Level: 3, LabelLength: 2, PicksCount: 1}
	copy(good.Label[:], "ok")
	for _, c := range []struct {
		name string
		set  func(v *Plain)
	}{
		{"level", func(v *Plain) { v.Level = 11 }},
		{"picks", func(v *Plain) { v.PicksCount = 5 }},
		{"label", func(v *Plain) { v.LabelLength = 9 }},
	} {
		bad := *good
		c.set(&bad)
		values := []*Plain{good, &bad}
		b := poisoned(4096)
		var rep TableReport
		if n := PlainSaveMessages(values, b, &rep); n != -1 || rep.Reason != "invalid_value" {
			t.Fatalf("%s: SaveMessages answered %d (%q), wants -1 invalid_value", c.name, n, rep.Reason)
		}
		intact(t, "plain "+c.name, b)
	}
	values := []*Plain{good, good}
	need := PlainMeasureMessages(values, nil)
	if need <= 0 {
		t.Fatalf("measure %d", need)
	}
	short := poisoned(int(need) - 1)
	if n := PlainSaveMessages(values, short, nil); n != -1 {
		t.Fatalf("a short buffer answered %d", n)
	}
	intact(t, "plain short", short)
	if n := PlainSaveMessages(values, poisoned(int(need)), nil); n != need {
		t.Fatalf("an exact buffer answered %d, wants %d", n, need)
	}
}

func TestSealedRefusalLeavesThePoison(t *testing.T) {
	good := &Sealed{Level: 3, LabelLength: 2, PicksCount: 1}
	copy(good.Label[:], "ok")
	bad := *good
	bad.Level = 11
	b := poisoned(4096)
	if n := SealedSaveMessages([]*Sealed{good, &bad}, b, nil); n != -1 {
		t.Fatalf("SaveMessages answered %d, wants -1", n)
	}
	intact(t, "sealed level", b)
	values := []*Sealed{good}
	need := SealedMeasureMessages(values, nil)
	short := poisoned(int(need) - 1)
	if n := SealedSaveMessages(values, short, nil); n != -1 {
		t.Fatalf("a short buffer answered %d", n)
	}
	intact(t, "sealed short", short)
	if n := SealedSaveMessages(values, poisoned(int(need)), nil); n != need {
		t.Fatalf("an exact buffer answered %d, wants %d", n, need)
	}
}
`

func TestGoMessageRefusalLeavesTheBufferUntouched(t *testing.T) {
	t.Parallel()
	runGenerated(t, messagePoisonSchema, messagePoisonProbe)
}

// TestGoMessageRefusalNegativeControl removes the measure that runs before
// the first byte, in the generated unit: the save then writes the lawful first
// value and the form and count bytes before the second value refuses, and the
// probe must go red on the poison.
func TestGoMessageRefusalNegativeControl(t *testing.T) {
	t.Parallel()
	out, err := runGeneratedEdited(t, messagePoisonSchema, messagePoisonProbe, func(files map[string][]byte) {
		patch(t, files, "need < 0 || int64(len(buffer)) < need {", "false && need < 0 || false && int64(len(buffer)) < need { /* SABOTAGED */")
	})
	if err == nil {
		t.Fatalf("NEGATIVE CONTROL FAILED: SaveMessages no longer measures first and the probe stayed GREEN\n%s", out)
	}
	if !strings.Contains(string(out), "the poison is gone") {
		t.Fatalf("NEGATIVE CONTROL FAILED: the probe went red, but not on the poison\n%s", out)
	}
}
