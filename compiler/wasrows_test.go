// A `was` on an enum variant, a union arm or a type's field (docs/SPEC-TABLES.md
// §5) is carried by C++, C, C#, Go and Dart; targets without the form refuse it by name.
package compiler

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

const wasRowsUnit = `package wrows

enum Grade
{
    Bronze,
    Argent | was = "Silver"
    Gold
}

type Buff
{
    mult float32 = 1.0 | was = "multiplier"
}

type Ward
{
    charge float32 = 0.0
}

union Effect
{
    shield Ward | was = "ward"
    pong | was = "ping"
    tally int32
}

fixed table Cfg
{
    grade  Grade
    effect Effect
    buff   Buff
}
`

func TestDartWasRowsCarriers(t *testing.T) {
	t.Parallel()
	out, err := New().Generate(unitFromSource(t, wasRowsUnit), "dart", Options{})
	if err != nil {
		t.Fatalf("dart refused the was rows: %v", err)
	}
	var all strings.Builder
	for _, b := range out {
		all.Write(b)
	}
	for _, old := range []string{"Silver", "ward", "ping", "multiplier"} {
		want := fmt.Sprintf("%d", int64(ir.TableWireId(old)))
		if !strings.Contains(all.String(), want) {
			t.Errorf("dart output lacks the old wire id of %q: %s", old, want)
		}
	}
}

func TestWasRowsCarriers(t *testing.T) {
	u := unitFromSource(t, wasRowsUnit)
	c := New()
	for _, target := range c.Targets() {
		out, err := c.Generate(u, target, Options{})
		if target == "squirrel" {
			// docs/SPEC-TABLES.md §5: squirrel carries the three rows. The
			// unit above also holds a union in a table closure, which the
			// target still refuses as a different, named follow-on; only a
			// refusal of THIS construct would be wrong.
			if err != nil && strings.Contains(err.Error(), "was on an enum variant") {
				t.Errorf("squirrel refused the was rows it carries: %v", err)
			}
			continue
		}
		if target == "cpp" || target == "c" || target == "cs" || target == "dart" || target == "go" || target == "elixir" || target == "js" || target == "rust" || target == "java" || target == "lobster" || target == "lua" || target == "luau" {
			if err != nil {
				t.Fatalf("%s carries the was rows and refused: %v", target, err)
			}
			var all strings.Builder
			for _, b := range out {
				all.Write(b)
			}
			// every id the renamed things ride under is the OLD name's hash;
			// the spelling's case is the target's (Elixir's formatter writes
			// hex upper case), so compare the output lower-cased.
			emitted := strings.ToLower(all.String())
			for _, old := range []string{"Silver", "ward", "ping", "multiplier"} {
				want := fmt.Sprintf("0x%016x", ir.TableWireId(old))
				if target == "dart" {
					want = fmt.Sprintf("%d", int64(ir.TableWireId(old)))
				}
				if target == "luau" {
					// Luau spells an id below 2^63 in decimal and the rest in
					// hex, both as integer literals (0x..i / ..i)
					if id := ir.TableWireId(old); id < 1<<63 {
						want = fmt.Sprintf("%di", id)
					} else {
						want += "i"
					}
				}
				if !strings.Contains(emitted, want) {
					t.Errorf("%s output lacks the id of %q, %s", target, old, want)
				}
			}
			continue
		}
		if err == nil {
			t.Errorf("%s emitted a unit with variant, arm and type-field was instead of refusing it", target)
			continue
		}
		if !strings.Contains(err.Error(), "Buff.mult, Effect.pong, Effect.shield and Grade.Argent") || !strings.Contains(err.Error(), "--lang cpp") {
			t.Errorf("%s refused without naming the rows and the carrier: %v", target, err)
		}
	}
}

func TestJavaWasRowsCarrier(t *testing.T) {
	u := unitFromSource(t, wasRowsUnit)
	out, err := New().Generate(u, "java", Options{})
	if err != nil {
		t.Fatalf("java carries the was rows and refused: %v", err)
	}
	var all strings.Builder
	for _, b := range out {
		all.Write(b)
	}
	for _, old := range []string{"Silver", "ward", "ping", "multiplier"} {
		want := fmt.Sprintf("0x%016x", ir.TableWireId(old))
		if !strings.Contains(all.String(), want) {
			t.Errorf("java output lacks the id of %q, %s", old, want)
		}
	}
}
