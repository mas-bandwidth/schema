package cqualify

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// cppLibc stands in for check.LibcIdentifier: the names this test declares.
func cppLibc(n string) bool {
	switch n {
	case "uint8_t", "memcpy", "exp", "size_t":
		return true
	}
	return false
}

// TestQualifyCppKeepsUserAndPlatformApart is the C++ pass on a hand-written
// stand-in for an emitter's output, in a unit that declares types named
// uint8_t, memcpy, NULL and exp (cpp.go): the user's marked spellings come
// back as the schema's (NULL past its macro as NULL_), the emitter's own
// platform uses are qualified where they name the global — a type always, a
// function at a call, inside a #define's body too — and an emitter local named
// like a libc function (`int64_t exp`) is left alone, as are literals,
// comments, the macro NULL and every name the unit does not declare.
func TestQualifyCppKeepsUserAndPlatformApart(t *testing.T) {
	u := &ir.Unit{Package: "probe", Structs: map[string]*ir.Struct{}}
	for _, n := range []string{"uint8_t", "memcpy", "NULL", "exp", "Holder"} {
		u.Structs[n] = &ir.Struct{Name: n}
	}
	esc, table := Escape(u, func(n string) bool { return CppCollides(n, cppLibc) })
	if table == nil {
		t.Fatal("no type was escaped")
	}
	marked := map[string]string{}
	for k, st := range esc.Structs {
		marked[st.DeclaredName()] = k
	}
	if marked["Holder"] != "Holder" {
		t.Errorf("Holder collides with nothing and must keep its spelling, got %q", marked["Holder"])
	}
	fill := strings.NewReplacer("{U}", marked["uint8_t"], "{M}", marked["memcpy"], "{N}", marked["NULL"], "{E}", marked["exp"])
	src := fill.Replace(`namespace probe {
struct {U} { uint8_t raw; };
struct {M} { std::uint8_t n; };
struct {N} { int x; };
struct {E} { int y; };
struct Holder { {U} first; {N} none; };
inline void copy({U} * d, const {U} * s, size_t n)
{
    memcpy(d, s, n);
    (void)::memcpy(d, s, n);
    int64_t exp = 0;
    exp += 1;
    const char * p = NULL;
    {M} m{};
    (void)m; (void)p; (void)sizeof(uint8_t);
}
// a comment naming {U} and {N}
static const char * names[] = { "{U}", "{N}" };
#define PROBE_COPY(d, s) memcpy(d, s, sizeof(uint8_t))
}
`)
	want := `namespace probe {
struct uint8_t { ::uint8_t raw; };
struct memcpy { std::uint8_t n; };
struct NULL_ { int x; };
struct exp { int y; };
struct Holder { uint8_t first; NULL_ none; };
inline void copy(uint8_t * d, const uint8_t * s, size_t n)
{
    ::memcpy(d, s, n);
    (void)::memcpy(d, s, n);
    int64_t exp = 0;
    exp += 1;
    const char * p = NULL;
    memcpy m{};
    (void)m; (void)p; (void)sizeof(::uint8_t);
}
// a comment naming uint8_t and NULL
static const char * names[] = { "uint8_t", "NULL" };
#define PROBE_COPY(d, s) ::memcpy(d, s, sizeof(::uint8_t))
}
`
	out, err := QualifyCpp(map[string][]byte{"Probe.h": []byte(src)}, table, cppLibc)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(out["Probe.h"]); got != want {
		t.Errorf("QualifyCpp:\n%s\nwant:\n%s", got, want)
	}
}
