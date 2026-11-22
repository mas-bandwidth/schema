// REGISTER'S CHECK AND ITS INSERT ARE ONE STEP (reviewer finding F-188).
//
// A generator's Names and Carries are the caller's code, and they run inside
// Register. Whatever they do to the driver — register another generator
// under the same name, under an alias, or under a name of their own — the
// registry stays what a sequence of plain Register calls could have built:
// one generator per spelling, and no name twice in Targets. A Register
// entered from inside another's callback completes first; the outer one is
// then checked against the registry as that left it. Public API only.
package compiler_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/compiler"
	"github.com/mas-bandwidth/schema/v3/ir"
)

// nested is a generator whose Names and Carries can each run a callback
// before answering. tag says which generator a Generate reached.
type nested struct {
	names     []string
	tag       string
	reached   *[]string
	onNames   func()
	onCarries func()
}

func (g nested) Names() []string {
	if g.onNames != nil {
		g.onNames()
	}
	return g.names
}

func (g nested) Carries() []string {
	if g.onCarries != nil {
		g.onCarries()
	}
	return []string{"timestamp"}
}

func (g nested) Generate(*ir.Unit, compiler.Options) (map[string][]byte, error) {
	*g.reached = append(*g.reached, g.tag)
	return map[string][]byte{}, nil
}

// noNameTwice is the invariant: Targets lists every name once.
func noNameTwice(t *testing.T, c *compiler.Compiler) {
	t.Helper()
	targets := c.Targets()
	if compact := slices.Compact(slices.Clone(targets)); len(compact) != len(targets) {
		t.Fatalf("Targets lists a name twice: %v", targets)
	}
}

func count(targets []string, name string) int {
	n := 0
	for _, target := range targets {
		if target == name {
			n++
		}
	}
	return n
}

// The control: two generators with one name, registered one after the other.
// The second is refused, the first is kept and listed once.
func TestRegisterSequentialDuplicateIsRefused(t *testing.T) {
	u := loadSource(t, carriesTimestampSrc)
	var reached []string
	c := compiler.New()
	if err := c.Register(nested{names: []string{"notes"}, tag: "first", reached: &reached}); err != nil {
		t.Fatal(err)
	}
	err := c.Register(nested{names: []string{"notes"}, tag: "second", reached: &reached})
	if err == nil || !strings.Contains(err.Error(), "already registered") {
		t.Fatalf("the second generator named notes registered: %v", err)
	}
	if n := count(c.Targets(), "notes"); n != 1 {
		t.Fatalf("Targets lists notes %d times, want 1", n)
	}
	if _, err := c.Generate(u, "notes", nil); err != nil || !slices.Equal(reached, []string{"first"}) {
		t.Fatalf("--lang notes reached %v (%v), want the first", reached, err)
	}
}

// The same two generators, the second registered from inside a callback of
// the first: exactly one of them holds the name — the one whose Register
// completed first — and the other is refused as the duplicate it is.
func TestRegisterFromInsideACallbackKeepsOneGeneratorPerName(t *testing.T) {
	for _, where := range []string{"Carries", "Names"} {
		t.Run(where, func(t *testing.T) {
			u := loadSource(t, carriesTimestampSrc)
			var reached []string
			c := compiler.New()
			var innerErr error
			register := func() {
				innerErr = c.Register(nested{names: []string{"notes"}, tag: "inner", reached: &reached})
			}
			outer := nested{names: []string{"notes"}, tag: "outer", reached: &reached}
			if where == "Carries" {
				outer.onCarries = register
			} else {
				outer.onNames = register
			}
			outerErr := c.Register(outer)

			if innerErr != nil {
				t.Fatalf("the registration that completed first was refused: %v", innerErr)
			}
			if outerErr == nil || !strings.Contains(outerErr.Error(), "already registered") {
				t.Fatalf("the second generator named notes registered: %v", outerErr)
			}
			if n := count(c.Targets(), "notes"); n != 1 {
				t.Fatalf("Targets lists notes %d times, want 1", n)
			}
			if _, err := c.Generate(u, "notes", nil); err != nil || !slices.Equal(reached, []string{"inner"}) {
				t.Fatalf("--lang notes reached %v (%v), want the generator that registered", reached, err)
			}
		})
	}
}

// No order of callbacks puts a name in Targets twice, and a refused
// generator leaves none of its spellings behind.
func TestTargetsNeverListsANameTwice(t *testing.T) {
	u := loadSource(t, carriesTimestampSrc)
	type step struct {
		names []string
		in    string // the callback the next step registers from: "Names", "Carries", or "" for the last
	}
	cases := []struct {
		name  string
		steps []step            // outermost first; each registers the next from inside its callback
		want  map[string]string // spelling -> tag reached; "" means not a target
	}{
		{"an alias taken from inside Carries", []step{{[]string{"notes", "memo"}, "Carries"}, {[]string{"memo"}, ""}},
			map[string]string{"notes": "", "memo": "1"}},
		{"the canonical name taken as an alias from inside Names", []step{{[]string{"notes"}, "Names"}, {[]string{"memo", "notes"}, ""}},
			map[string]string{"notes": "1", "memo": "1"}},
		{"a different name from inside Carries", []step{{[]string{"notes"}, "Carries"}, {[]string{"memo"}, ""}},
			map[string]string{"notes": "0", "memo": "1"}},
		{"three deep, one name", []step{{[]string{"notes"}, "Carries"}, {[]string{"notes"}, "Names"}, {[]string{"notes"}, ""}},
			map[string]string{"notes": "2"}},
		{"three deep, the middle one distinct", []step{{[]string{"notes"}, "Names"}, {[]string{"memo"}, "Carries"}, {[]string{"notes"}, ""}},
			map[string]string{"notes": "2", "memo": "1"}},
		{"a built-in name from inside Carries", []step{{[]string{"notes"}, "Carries"}, {[]string{"go"}, ""}},
			map[string]string{"notes": "0"}},
		{"itself, again, from inside Carries", []step{{[]string{"notes"}, "Carries"}, {[]string{"notes"}, "Carries"}, {[]string{"notes"}, ""}},
			map[string]string{"notes": "2"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var reached []string
			c := compiler.New()
			builtins := len(c.Targets())
			var build func(i int) nested
			build = func(i int) nested {
				g := nested{names: tc.steps[i].names, tag: string(rune('0' + i)), reached: &reached}
				next := func() { _ = c.Register(build(i + 1)) }
				switch tc.steps[i].in {
				case "Names":
					g.onNames = next
				case "Carries":
					g.onCarries = next
				}
				return g
			}
			_ = c.Register(build(0))

			noNameTwice(t, c)
			canonical := map[string]bool{}
			for spelling, tag := range tc.want {
				reached = nil
				_, err := c.Generate(u, spelling, nil)
				if tag == "" {
					if err == nil || len(reached) != 0 {
						t.Fatalf("--lang %s is a target (reached %v), want none", spelling, reached)
					}
					continue
				}
				if err != nil || !slices.Equal(reached, []string{tag}) {
					t.Fatalf("--lang %s reached %v (%v), want generator %s", spelling, reached, err, tag)
				}
				canonical[tag] = true
			}
			if got := len(c.Targets()) - builtins; got != len(canonical) {
				t.Fatalf("Targets grew by %d, want %d: %v", got, len(canonical), c.Targets())
			}
		})
	}
}
