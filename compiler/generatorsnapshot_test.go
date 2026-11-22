// A REGISTRATION IS WHAT THE GENERATOR SAID AT REGISTER (review finding 12).
//
// Register validates a generator's names and its construct declaration, so
// the driver must go on using exactly what it validated: its own copy, taken
// at that call. These tests hold that a registered generator's dispatch
// names, its entry in Targets and the constructs it is handed do not move
// when the slices it returned are edited afterwards, nor when its methods
// answer differently on a later call. Public API only, as an external
// generator's module sees it.
package compiler_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/compiler"
	"github.com/mas-bandwidth/schema/v3/ir"
)

// held is a generator whose answers live in slices its owner keeps, and can
// edit, and whose methods count their calls. It declares nothing.
type held struct {
	names     []string
	later     []string // when set, what Names answers after its first call
	nameCalls *int
	calls     *int
}

func (g held) Names() []string {
	*g.nameCalls++
	if *g.nameCalls > 1 && g.later != nil {
		return g.later
	}
	return g.names
}

func (g held) Generate(*ir.Unit, compiler.Options) (map[string][]byte, error) {
	*g.calls++
	return map[string][]byte{}, nil
}

// heldCarrier is held with a construct declaration, in a slice its owner
// keeps too.
type heldCarrier struct {
	held
	carried    []string
	laterCarry []string // when set, what Carries answers after its first call
	carryCalls *int
	onCarries  func() // runs inside Carries, when set
}

func (g heldCarrier) Carries() []string {
	*g.carryCalls++
	if g.onCarries != nil {
		g.onCarries()
	}
	if *g.carryCalls > 1 && g.laterCarry != nil {
		return g.laterCarry
	}
	return g.carried
}

// twoGenerators registers limited (two spellings, an empty declaration) and
// timed (declares timestamp) on a fresh driver.
func twoGenerators(t *testing.T, names held) (c *compiler.Compiler, timedCalls *int) {
	t.Helper()
	c = compiler.New()
	limited := heldCarrier{held: names, carried: []string{}, carryCalls: new(int)}
	if err := c.Register(limited); err != nil {
		t.Fatal(err)
	}
	timedCalls = new(int)
	timed := heldCarrier{
		held:       held{names: []string{"timed"}, nameCalls: new(int), calls: timedCalls},
		carried:    []string{"timestamp"},
		carryCalls: new(int),
	}
	if err := c.Register(timed); err != nil {
		t.Fatal(err)
	}
	return c, timedCalls
}

// limitedStaysLimited holds the whole property for the two-generator driver:
// both limited spellings refuse a timestamp unit by the name limited, without
// invoking either generator; timed is handed it; Targets lists both.
func limitedStaysLimited(t *testing.T, c *compiler.Compiler, u *ir.Unit, limitedCalls, timedCalls *int) {
	t.Helper()
	for _, spelling := range []string{"limited", "limited-alias"} {
		_, err := c.Generate(u, spelling, nil)
		if err == nil {
			t.Fatalf("--lang %s was handed a timestamp unit it never declared", spelling)
		}
		if !strings.Contains(err.Error(), "limited target") {
			t.Fatalf("--lang %s was refused under another generator's name: %v", spelling, err)
		}
	}
	if *limitedCalls != 0 || *timedCalls != 0 {
		t.Fatalf("a refusal invoked a generator: limited %d, timed %d", *limitedCalls, *timedCalls)
	}
	if _, err := c.Generate(u, "timed", nil); err != nil {
		t.Fatalf("timed declared timestamp and was refused: %v", err)
	}
	if *limitedCalls != 0 || *timedCalls != 1 {
		t.Fatalf("--lang timed dispatched wrongly: limited %d, timed %d", *limitedCalls, *timedCalls)
	}
	targets := c.Targets()
	if !slices.Contains(targets, "limited") || !slices.Contains(targets, "timed") || slices.Contains(targets, "limited-alias") {
		t.Fatalf("Targets moved: %v", targets)
	}
}

// The names slice Names returned stays the caller's: editing it after Register
// changes nothing the driver does.
func TestRegisteredNamesDoNotFollowTheCallersSlice(t *testing.T) {
	u := loadSource(t, carriesTimestampSrc)
	names := []string{"limited", "limited-alias"}
	limitedCalls := new(int)
	c, timedCalls := twoGenerators(t, held{names: names, nameCalls: new(int), calls: limitedCalls})

	limitedStaysLimited(t, c, u, limitedCalls, timedCalls)
	names[0] = "timed"
	*timedCalls = 0
	limitedStaysLimited(t, c, u, limitedCalls, timedCalls)
	names[1] = "timed"
	*timedCalls = 0
	limitedStaysLimited(t, c, u, limitedCalls, timedCalls)
}

// Names is asked once, at Register: a generator that answers differently
// afterwards is still the one that registered.
func TestRegisteredNamesAreAskedOnce(t *testing.T) {
	u := loadSource(t, carriesTimestampSrc)
	limitedCalls, nameCalls := new(int), new(int)
	limited := held{
		names:     []string{"limited", "limited-alias"},
		later:     []string{"timed"},
		nameCalls: nameCalls,
		calls:     limitedCalls,
	}
	c, timedCalls := twoGenerators(t, limited)

	limitedStaysLimited(t, c, u, limitedCalls, timedCalls)
	if *nameCalls != 1 {
		t.Fatalf("Names was called %d times, want once, at Register", *nameCalls)
	}
}

// The construct slice Carries returned stays the caller's: editing it after
// Register neither adds a construct nor takes one away.
func TestDeclaredConstructsDoNotFollowTheCallersSlice(t *testing.T) {
	stamped := loadSource(t, carriesTimestampSrc)
	required := loadSource(t, "package probe\n\ntable Ship\n{\n    name string(32) | required\n}\n")
	carried := []string{"required"}
	calls := new(int)
	g := heldCarrier{
		held:       held{names: []string{"catalog"}, nameCalls: new(int), calls: calls},
		carried:    carried,
		carryCalls: new(int),
	}
	c := compiler.New()
	if err := c.Register(g); err != nil {
		t.Fatal(err)
	}
	carried[0] = "timestamp"
	if _, err := c.Generate(stamped, "catalog", nil); err == nil || *calls != 0 {
		t.Fatalf("a construct edited in after Register was handed over (calls %d): %v", *calls, err)
	}
	if _, err := c.Generate(required, "catalog", nil); err != nil || *calls != 1 {
		t.Fatalf("a construct declared at Register was taken away (calls %d): %v", *calls, err)
	}
}

// Carries is asked once, at Register: a generator that answers differently
// afterwards carries what it declared then.
func TestDeclaredConstructsAreAskedOnce(t *testing.T) {
	stamped := loadSource(t, carriesTimestampSrc)
	calls, carryCalls := new(int), new(int)
	g := heldCarrier{
		held:       held{names: []string{"catalog"}, nameCalls: new(int), calls: calls},
		carried:    []string{"required"},
		laterCarry: []string{"timestamp"},
		carryCalls: carryCalls,
	}
	c := compiler.New()
	if err := c.Register(g); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Generate(stamped, "catalog", nil); err == nil || *calls != 0 {
		t.Fatalf("a construct answered after Register was handed over (calls %d): %v", *calls, err)
	}
	if *carryCalls != 1 {
		t.Fatalf("Carries was called %d times, want once, at Register", *carryCalls)
	}
}

// The names Register checked are the names it registers: a generator's own
// methods run during Register, and nothing they do to the returned slice
// moves the registration onto a name another generator holds.
func TestRegisteredNamesAreTheNamesThatWereChecked(t *testing.T) {
	u := loadSource(t, carriesTimestampSrc)
	names := []string{"catalog"}
	calls := new(int)
	g := heldCarrier{
		held:       held{names: names, nameCalls: new(int), calls: calls},
		carried:    []string{"timestamp"},
		carryCalls: new(int),
		onCarries:  func() { names[0] = "go" },
	}
	c := compiler.New()
	before := c.Targets()
	if err := c.Register(g); err != nil {
		t.Fatal(err)
	}
	if want := append(slices.Clone(before), "catalog"); !slices.Equal(c.Targets(), sorted(want)) {
		t.Fatalf("Targets is %v, want the built-ins and catalog", c.Targets())
	}
	if _, err := c.Generate(u, "go", nil); err != nil {
		t.Fatalf("the built-in go target: %v", err)
	}
	if *calls != 0 {
		t.Fatal("--lang go dispatched to the registered generator")
	}
	if _, err := c.Generate(u, "catalog", nil); err != nil || *calls != 1 {
		t.Fatalf("--lang catalog did not reach its generator (calls %d): %v", *calls, err)
	}
}

func sorted(s []string) []string {
	slices.Sort(s)
	return s
}
