// The one-shot migration mode's own pin (SPEC §7.4 rule 3b): retired
// spellings in, current canonical form out — the default moved before the
// pipe, a qualified declaration's brace moved down, [<= N] respelled — and
// the wrapped-block refusal.
package format

import (
	"strings"
	"testing"
)

func TestMigrateRewritesRetiredSpellings(t *testing.T) {
	old := "package t\n" +
		"\n" +
		"enum Weapon [max = 15] { Laser, Missile }\n" +
		"\n" +
		"type T {\n" +
		"    health int16 [min = 0, max = 100]\n" +
		"    invulnerable bool [local] = true\n" +
		"    w fixed(2, 30) [min = -1, max = 1] = 1.0\n" +
		"    other uint8 [min = 0, max = 5] // trailing note\n" +
		"    arr [<= 4]uint8\n" +
		"}\n"
	want := "package t\n" +
		"\n" +
		"enum Weapon | max = 15\n" +
		"{ Laser, Missile }\n" +
		"\n" +
		"type T {\n" +
		"    health       int16              | min = 0, max = 100\n" +
		"    invulnerable bool = true        | local\n" +
		"    w            fixed(2, 30) = 1.0 | min = -1, max = 1\n" +
		"    other        uint8              | min = 0, max = 5 // trailing note\n" +
		"    arr          [..4]uint8\n" +
		"}\n"
	got, err := Migrate("Old.schema", []byte(old))
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if string(got) != want {
		t.Errorf("migrate output diverges from the canonical form\n---- got ----\n%s---- want ----\n%s", got, want)
	}

	// idempotent over its own output, and plain Format agrees with it
	again, err := Migrate("Old.schema", got)
	if err != nil || string(again) != string(got) {
		t.Errorf("migrate is not idempotent (err %v)", err)
	}
	plain, err := Format("Old.schema", got)
	if err != nil || string(plain) != string(got) {
		t.Errorf("plain fmt disagrees with migrated output (err %v)", err)
	}
}

func TestMigrateRefusesWrappedAttributeBlock(t *testing.T) {
	old := "package t\n\ntype T {\n    h int16 [\n        min = 0,\n        max = 100\n    ]\n}\n"
	_, err := Migrate("Old.schema", []byte(old))
	if err == nil || !strings.Contains(err.Error(), "unwrap the attribute block first") {
		t.Errorf("a wrapped attribute block must be refused with the unwrap instruction, got %v", err)
	}
}
