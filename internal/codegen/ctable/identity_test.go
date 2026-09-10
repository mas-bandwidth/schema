package ctable

import (
	"os"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// Identity tests from #838, held against THIS branch's identity shape:
// one copy of the body and a straight-line scatter of the identity plan
// (count and text stay plan ops, unrolled in the scatter), prefill only the
// holes the plan will not write (empty on identity), and one memcpy with no
// scatter where ir.TableFixedIdentityFlat. Packed layout is not forced.
//
// gocritic offBy1: Index can be -1 — every needle goes through mustIndex.
// gcc -Werror=array-bounds: copy_run's overlapping unroll inlines into fill_run
// against the compiled path's stack defaults (Held is 36 bytes, Flat is 8).
// Identity dest is a pointer; the probe is not the form. The flags on the
// two execute tests silence that false positive and do not skip the prefill.
// GNU is detected by __GNUC__ without __clang__: Ubuntu's cc --version never
// says gcc, and requiring that word dropped the flags on CI.

const identityFlatSchema = `package probe
table Flat {
    a uint32
    b uint32
}
`

const identityHeldSchema = `package probe
table Held {
    marks [..4]int32
    label string(8)
}
`

const identityPadSchema = `package probe
table Pad {
    a uint8
    b uint32
}
`

func tableH(t *testing.T, src string) string {
	t.Helper()
	files := generate(t, src)
	for name, data := range files {
		if strings.HasSuffix(name, "Table.h") {
			return string(data)
		}
	}
	t.Fatal("no Table.h")
	return ""
}

func identityCCFlags() []string {
	cc := os.Getenv("CC")
	if cc == "" {
		cc = "cc"
	}
	return gccFortifySilence(cc)
}

// textTermBytes is the terminator width a text flavour stores past the bound:
// one for utf8, two for wide, none for bytes. Same numbers as the coverage
// walk. QF1003 wants a tagged switch on Meta, not a chain of equals.
func textTermBytes(meta int) int64 {
	switch meta {
	case 1:
		return 1
	case 2:
		return 2
	}
	return 0
}

func mustIndex(t *testing.T, s, needle string) int {
	t.Helper()
	i := strings.Index(s, needle)
	if i < 0 {
		t.Fatalf("%q missing", needle)
	}
	return i
}

func fromNeedle(t *testing.T, s, needle string) string {
	t.Helper()
	return s[mustIndex(t, s, needle):]
}

func between(t *testing.T, s, start, end string) string {
	t.Helper()
	i := mustIndex(t, s, start)
	j := mustIndex(t, s, end)
	if j < i {
		t.Fatalf("%q is before %q", end, start)
	}
	return s[i:j]
}

func identityLoad(t *testing.T, h, name string) string {
	t.Helper()
	load := fromNeedle(t, h, name+"_fixed_load")
	body, _, found := strings.Cut(load, "\n}\n")
	if !found {
		t.Fatalf("%s_fixed_load not closed", name)
	}
	return body
}

func identityPath(t *testing.T, load string) string {
	t.Helper()
	// Rowan's identity read returns; a stranger's layout is the next paragraph,
	// not an else. "else" is not a bound — gocritic offBy1 on Index, and the
	// form-byte ternary is not an else.
	return between(t, load, "if ( identity )", "A STRANGER'S LAYOUT")
}

func TestIdentityPlanKeepsCountAndText(t *testing.T) {
	u := unitFrom(t, identityHeldSchema)
	st := u.Tables["Held"]
	plan, _ := ir.TableFixedBuildPlan(u, st)
	var sawCount, sawText bool
	for _, e := range plan {
		if e.Op == ir.TableFixedOpCount {
			sawCount = true
		}
		if e.Op == ir.TableFixedOpText {
			sawText = true
		}
	}
	if !sawCount || !sawText {
		t.Fatal("identity plan dropped count or text; the scatter unrolls those ops, it does not rewrite them to copies")
	}
	if ir.TableFixedIdentityFlat(u, st) {
		t.Fatal("Held has a count after its array and a terminator; it is not a flat memcpy")
	}
}

func TestIdentityCoverageIsThePlanAndHolesAreEmpty(t *testing.T) {
	u := unitFrom(t, identityHeldSchema)
	st := u.Tables["Held"]
	cover := ir.TableFixedIdentityCoverage(u, st)
	if len(cover) == 0 {
		t.Fatal("identity coverage of Held is empty")
	}
	plan, _ := ir.TableFixedBuildPlan(u, st)
	covered := make([]bool, len(cover))
	for _, e := range plan {
		var ranges []ir.TableFixedRange
		switch e.Op {
		case ir.TableFixedOpCopy:
			ranges = []ir.TableFixedRange{{Dst: e.Dst, Size: e.Size}}
		case ir.TableFixedOpCount:
			ranges = []ir.TableFixedRange{{Dst: e.Dst, Size: ir.TableFixedCountBytes}}
		case ir.TableFixedOpText:
			ranges = []ir.TableFixedRange{
				{Dst: e.Dst, Size: ir.TableFixedCountBytes},
				{Dst: e.Aux, Size: e.Size + textTermBytes(e.Meta)},
			}
		}
		for _, r := range ranges {
			found := false
			for i, c := range cover {
				if r.Dst >= c.Dst && r.Dst+r.Size <= c.Dst+c.Size {
					covered[i] = true
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("plan dest [%d,%d) is not in identity coverage", r.Dst, r.Dst+r.Size)
			}
		}
	}
	for i, ok := range covered {
		if !ok {
			t.Fatalf("coverage range [%d,%d) is not a plan destination — identity holes must be empty", cover[i].Dst, cover[i].Dst+cover[i].Size)
		}
	}
}

func TestIdentityLoadPrefillsNoDefaults(t *testing.T) {
	h := tableH(t, identityHeldSchema)
	load := identityLoad(t, h, "held")
	if !strings.Contains(load, "if ( identity )") {
		t.Fatal("identity path not branched")
	}
	id := identityPath(t, load)
	if strings.Contains(id, "held_reset") {
		t.Fatal("identity path still prefills defaults")
	}
	if strings.Contains(id, "table_fixed_fill_run") {
		t.Fatal("identity path still runs the hole prefill")
	}
	if strings.Contains(id, "table_fixed_run") {
		t.Fatal("identity path still spends the plan in the interpreter")
	}
	if !strings.Contains(fromNeedle(t, load, "A STRANGER'S LAYOUT"), "held_reset") {
		t.Fatal("compiled path dropped the default image")
	}
	if !strings.Contains(id, "schema_probe_held_fixed_scatter_") {
		t.Fatal("identity path dropped the straight-line scatter")
	}
	if !strings.Contains(id, "memcpy( image, at + 8") {
		t.Fatal("identity path dropped the one copy of the body")
	}
}

func TestIdentityFlatIsMemcpyEmptyScatter(t *testing.T) {
	u := unitFrom(t, identityFlatSchema)
	st := u.Tables["Flat"]
	if !ir.TableFixedIdentityFlat(u, st) {
		t.Fatal("two uint32s should be a storage image that is the wire image")
	}
	body := ir.TableFixedTypeBytes(st)
	if body != 8 {
		t.Fatalf("Flat body want 8, got %d", body)
	}
	h := tableH(t, identityFlatSchema)
	load := identityLoad(t, h, "flat")
	id := identityPath(t, load)
	if !strings.Contains(id, "memcpy( (void *) ( values + k ), at + 8, (size_t) 8 )") {
		t.Fatal("flat identity read is not one memcpy of the body")
	}
	if strings.Contains(id, "table_fixed_run") {
		t.Fatal("flat identity still scatters through the interpreter")
	}
	if strings.Contains(id, "fixed_scatter_") {
		t.Fatal("flat identity still calls a scatter")
	}
	if strings.Contains(id, "flat_reset") {
		t.Fatal("flat identity still prefills")
	}
	if strings.Contains(h, "schema_probe_flat_fixed_scatter_") {
		t.Fatal("flat identity still emitted a scatter")
	}
}

func TestIdentityGuardedTextKeepsGuardArg(t *testing.T) {
	const src = `package probe
type ArmA
{
    n int32
}
type ArmB
{
    label string(8)
}
union Pick
{
    a ArmA
    b ArmB
}
table Root
{
    pick Pick
}
`
	u := unitFrom(t, src)
	st := u.Tables["Root"]
	plan, _ := ir.TableFixedBuildPlan(u, st)
	var text ir.TableFixedLeaf
	found := false
	for _, e := range plan {
		if e.Op == ir.TableFixedOpText {
			text = e
			found = true
			break
		}
	}
	if !found {
		t.Fatal("compiler plan lost the arm's text")
	}
	if text.Arg != 2 {
		t.Fatalf("compiler text arg want 2 (second arm), got %d", text.Arg)
	}
	if text.Meta != 1 {
		t.Fatalf("compiler text meta want 1 (utf8), got %d", text.Meta)
	}
}

func TestIdentityPaddedDoesNotMemcpyTheStruct(t *testing.T) {
	u := unitFrom(t, identityPadSchema)
	if ir.TableFixedIdentityFlat(u, u.Tables["Pad"]) {
		t.Fatal("uint8 then uint32 is padded; memcpy of the struct would be a wire-byte move")
	}
	h := tableH(t, identityPadSchema)
	load := identityLoad(t, h, "pad")
	id := identityPath(t, load)
	if strings.Contains(id, "memcpy( (void *) ( values + k ), at + 8") {
		t.Fatal("padded identity memcpy'd the struct; that is a wire-byte move")
	}
	if !strings.Contains(id, "schema_probe_pad_fixed_scatter_") {
		t.Fatal("padded identity dropped the scatter")
	}
}

func TestIdentityHeldRoundTripAndClamp(t *testing.T) {
	runCGenerated(t, identityHeldSchema, `#include "ProbeTable.h"
#include <stdio.h>
#include <string.h>
#define CHECK(x) do { if (!(x)) { fprintf(stderr, "line %d: %s\n", __LINE__, #x); return 1; } } while (0)
int main(void)
{
    Held value, loaded;
    TableReport report;
    TableFixedEntry plan[64];
    uint8_t file[512];
    int64_t need, n;
    uint32_t layout_bytes;
    uint8_t * body;
    memset(&value, 0xAA, sizeof(value));
    value.marks[0] = 7;
    value.marks_count = 1;
    value.label[0] = 'h';
    value.label[1] = 'i';
    value.label_length = 2;
    need = held_fixed_measure(1);
    CHECK(need > 0 && need <= (int64_t)sizeof(file));
    CHECK(held_fixed_save(&value, 1, file, (int64_t)sizeof(file)) == need);
    memset(&loaded, 0xAA, sizeof(loaded));
    memset(&report, 0, sizeof(report));
    n = held_fixed_load(&loaded, 1, file, need, plan, 64, &report);
    CHECK(n == 1);
    CHECK(!report.malformed && !report.refused && report.clamped == 0);
    CHECK(loaded.marks_count == 1 && loaded.marks[0] == 7);
    CHECK(loaded.marks[1] == 0 && loaded.marks[2] == 0 && loaded.marks[3] == 0);
    CHECK(loaded.label_length == 2 && loaded.label[0] == 'h' && loaded.label[1] == 'i' && loaded.label[2] == 0);
    layout_bytes = table_fixed_get32(file + kTableFixedHeaderBytes);
    body = file + kTableFixedHeaderBytes + 4 + layout_bytes + 8;
    table_fixed_put32(body, 99);
    table_fixed_put32(body + 4 + 16, 99);
    memset(&loaded, 0, sizeof(loaded));
    memset(&report, 0, sizeof(report));
    n = held_fixed_load(&loaded, 1, file, need, plan, 64, &report);
    CHECK(n == 1);
    CHECK(report.clamped == 2);
    CHECK(loaded.marks_count == 4);
    CHECK(loaded.label_length == 8);
    CHECK(loaded.label[8] == 0);
    return 0;
}
`, identityCCFlags()...)
}

func TestIdentityFlatRoundTripFromDirty(t *testing.T) {
	runCGenerated(t, identityFlatSchema, `#include "ProbeTable.h"
#include <stdio.h>
#include <string.h>
#define CHECK(x) do { if (!(x)) { fprintf(stderr, "line %d: %s\n", __LINE__, #x); return 1; } } while (0)
/* gcc -O2 inlines table_fixed_copy_run into fill_run against stack defaults
   (8 bytes) and refuses the 16-byte unroll (-Werror=array-bounds). Identity
   is memcpy of the body's eight bytes; dest arrives here as a pointer gcc
   cannot bound. The compile flags on this probe match Held's. */
#if defined(__GNUC__)
__attribute__((noinline))
#endif
static int64_t load_flat(Flat *loaded, const uint8_t *file, int64_t need, TableFixedEntry *plan, TableReport *report)
{
    return flat_fixed_load(loaded, 1, file, need, plan, 8, report);
}
int main(void)
{
    Flat value, loaded;
    TableReport report;
    TableFixedEntry plan[8];
    uint8_t file[256];
    int64_t need, n;
    value.a = 1;
    value.b = 2;
    need = flat_fixed_measure(1);
    CHECK(flat_fixed_save(&value, 1, file, (int64_t)sizeof(file)) == need);
    memset(&loaded, 0xAA, sizeof(loaded));
    memset(&report, 0, sizeof(report));
    n = load_flat(&loaded, file, need, plan, &report);
    CHECK(n == 1);
    CHECK(!report.malformed && !report.refused && report.clamped == 0);
    CHECK(loaded.a == 1 && loaded.b == 2);
    CHECK((int64_t)sizeof(loaded) == 8);
    return 0;
}
`, identityCCFlags()...)
}
