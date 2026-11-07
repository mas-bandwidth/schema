// The Luau target registers `timestamp` for itself (docs/SPEC-TABLES.md §2.11):
// the same probe unit compiler/timestamp_test.go uses generates files and no
// error for Luau, while every non-carrying target keeps refusing it by the
// same name.
package compiler

import (
	"strings"
	"testing"
)

func init() {
	nonGoCarriers["luau"] = append(nonGoCarriers["luau"], carryTimestamp)
}

func TestLuauCarriesTimestamp(t *testing.T) {
	t.Parallel()
	if !carries("luau", carryTimestamp) {
		t.Fatal("luau does not carry timestamp")
	}
	u := unitFromSource(t, timestampUnit)
	c := New()
	files, err := c.Generate(u, "luau", Options{})
	if err != nil || len(files) == 0 {
		t.Fatalf("luau must carry timestamp: files=%d err=%v", len(files), err)
	}
	for _, target := range c.Targets() {
		if carries(target, carryTimestamp) {
			continue
		}
		target := target
		t.Run(target, func(t *testing.T) {
			_, err := c.Generate(u, target, Options{})
			if err == nil {
				t.Fatalf("target %q accepted timestamp", target)
			}
			for _, want := range []string{target + " target", "timestamp", "Event.opened_at", "Session.started", "Go carries it first"} {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("target %s: the refusal lacks %q: %v", target, want, err)
				}
			}
		})
	}
}
