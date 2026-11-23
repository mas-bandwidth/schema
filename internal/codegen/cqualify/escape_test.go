package cqualify

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestQualifyEscapedLeavesThePlatformAlone is the C escape on a hand-written
// stand-in for an emitter's output in a unit declaring types named uint8_t,
// memcpy and NULL (escape.go): the user's types take the package's spelling,
// and the emitter's own uint8_t, memcpy call and NULL stay the platform's —
// the very tokens Qualify once respelled with the user's.
func TestQualifyEscapedLeavesThePlatformAlone(t *testing.T) {
	u := &ir.Unit{Package: "probe", Structs: map[string]*ir.Struct{}}
	for _, n := range []string{"uint8_t", "memcpy", "NULL"} {
		u.Structs[n] = &ir.Struct{Name: n}
	}
	esc, table := Escape(u, func(string) bool { return true })
	marked := map[string]string{}
	for k, st := range esc.Structs {
		marked[st.DeclaredName()] = k
	}
	fill := strings.NewReplacer("{U}", marked["uint8_t"], "{M}", marked["memcpy"], "{N}", marked["NULL"])
	src := fill.Replace(`typedef struct {U} { uint8_t raw; } {U};
typedef struct {M} { void * p; } {M};
typedef struct {N} { int x; } {N};
static int copy_it( {U} * d, const {U} * s, {M} * m, {N} * n )
{
    memcpy( d, s, sizeof( {U} ) );
    m->p = NULL;
    (void) n;
    return 1; /* copies a {U} */
}
static const char * const names[3] = { "{U}", "{M}", "{N}" };
`)
	out, err := QualifyEscaped(map[string][]byte{"Probe.h": []byte(src)}, "probe", table)
	if err != nil {
		t.Fatal(err)
	}
	got := string(out["Probe.h"])
	for _, keep := range []string{
		"{ uint8_t raw; }",                // the emitter's uint8_t
		"memcpy( d, s, sizeof(",           // the emitter's memcpy call
		"m->p = NULL;",                    // the platform's NULL
		`{ "uint8_t", "memcpy", "NULL" }`, // literals carry the schema's spellings
		"/* copies a uint8_t */",          // and so do comments
	} {
		if !strings.Contains(got, keep) {
			t.Errorf("QualifyEscaped lost %q:\n%s", keep, got)
		}
	}
	for _, gone := range []string{"typedef struct uint8_t", "typedef struct memcpy", "typedef struct NULL", "probe_memcpy("} {
		if strings.Contains(got, gone) {
			t.Errorf("QualifyEscaped wrote %q:\n%s", gone, got)
		}
	}
	if table.Marked(got) {
		t.Errorf("a marked spelling survived:\n%s", got)
	}
}
