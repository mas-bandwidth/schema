package ctable

// TestCFixedNormalizationIsDamage holds c/R17, c/C12 and c/C13 under the
// strict read: a bool or present byte other than 0 or 1 refuses
// wire_damaged (docs/FIXED-FORM-ALGORITHM.md §4.5 fix 2), never
// out_of_range, never normalised to 1 and never stored verbatim. One read
// is the whole value or a refusal: no destination byte and no counter
// moves beside the refusal. The negative control plants its fault in the
// real generated header.

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	cgen "github.com/mas-bandwidth/schema/v2/internal/codegen/c"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

const cNormDamageProbe = `#include <stdio.h>
#include <string.h>
#include "VnewHostileBoolTable.h"
#define CHECK(x) do { if (!(x)) { printf("FAIL: line %d: %s\n", __LINE__, #x); return 1; } } while (0)
static Hbb fresh;
static Hbb back[8];
static TableReport r;
static TableFixedEntry plan[4096];
static uint8_t data[1 << 20];
static uint8_t orig[1 << 20];
static int64_t written;
static int64_t n;
#define RECORD0() (data + kTableFixedHeaderBytes + 4 + hbb_fixed_layout_bytes + 8)
#define EXPECT_DAMAGE(WHO) do { \
    if (n != -1) { printf("%s: n=%lld, not -1\n", WHO, (long long)n); return 1; } \
    if (!r.refused || r.reason != 0 || r.refuse_reason != SCHEMA_TABLE_REFUSE_WIRE_DAMAGED) \
        { printf("%s: refused=%d reason=%d refuse_reason=%d, not wire_damaged\n", WHO, r.refused, r.reason, r.refuse_reason); return 1; } \
    if (r.unknown != 0 || r.widened != 0 || r.layout_hash != 0) \
        { printf("%s: counters moved on wire_damaged refusal\n", WHO); return 1; } \
    if (memcmp(&back[0], &fresh, sizeof(fresh)) != 0) \
        { printf("%s: a refusal landed a value\n", WHO); return 1; } } while (0)
#define LOAD_FORGED(WHO) do { \
    memcpy(data, orig, (size_t)written); \
    memset(&fresh, 0, sizeof(fresh)); hbb_reset(&fresh); \
    memset(back, 0, sizeof(back)); hbb_reset(&back[0]); \
    memset(&r, 0, sizeof(r)); } while (0)
int main(void)
{
    Hbb v;
    int i;
    memset(&v, 0, sizeof(v));
    v.flag = 1;
    v.link_present = 1;
    v.link.n = 5;
    v.lead = 0xAAAAAAAAu;
    for (i = 0; i < 8; ++i) { v.flags[i] = (uint8_t)(i & 1); }
    v.trail = 0xBBBBBBBBu;
    v.extra = 0xCCCCCCCCu;
    written = hbb_fixed_save(&v, 1, data, (int64_t)sizeof(data));
    if (written < 0) { printf("save failed\n"); return 1; }
    memcpy(orig, data, (size_t)written);
    /* Clean 0/1 bytes still read whole. */
    LOAD_FORGED("clean");
    n = hbb_fixed_load(back, 8, data, written, plan, 4096, NULL, &r);
    CHECK(n == 1 && !r.refused && r.reason == 0 && r.refuse_reason == 0);
    CHECK(back[0].flag == 1 && back[0].link_present == 1 && back[0].link.n == 5);
    CHECK(back[0].lead == 0xAAAAAAAAu && back[0].trail == 0xBBBBBBBBu && back[0].extra == 0xCCCCCCCCu);
    for (i = 0; i < 8; ++i) { CHECK(back[0].flags[i] == (uint8_t)(i & 1)); }
    /* c/C12: a scalar bool byte of 0x02 is damage. */
    LOAD_FORGED("c/C12");
    RECORD0()[0] = 0x02;
    n = hbb_fixed_load(back, 8, data, written, plan, 4096, NULL, &r);
    EXPECT_DAMAGE("c/C12 bool 0x02");
    /* A bool byte of 0xFF is damage too (the rule is > 1, not == 2). */
    LOAD_FORGED("c/C12 ff");
    RECORD0()[0] = 0xFF;
    n = hbb_fixed_load(back, 8, data, written, plan, 4096, NULL, &r);
    EXPECT_DAMAGE("c/C12 bool 0xFF");
    /* c/C13: a present byte of 0x02 is damage. */
    LOAD_FORGED("c/C13");
    RECORD0()[1] = 0x02;
    n = hbb_fixed_load(back, 8, data, written, plan, 4096, NULL, &r);
    EXPECT_DAMAGE("c/C13 present 0x02");
    /* c/R17: bool, present and every bool-array byte forged together. */
    LOAD_FORGED("c/R17");
    RECORD0()[0] = 0x02;
    RECORD0()[1] = 0x02;
    for (i = 0; i < 8; ++i) { RECORD0()[10 + i] = 0x02; }
    n = hbb_fixed_load(back, 8, data, written, plan, 4096, NULL, &r);
    EXPECT_DAMAGE("c/R17 bool+present+array 0x02");
    /* One forged bool-array byte alone is damage. */
    LOAD_FORGED("c/R17 array");
    RECORD0()[10 + 3] = 0x02;
    n = hbb_fixed_load(back, 8, data, written, plan, 4096, NULL, &r);
    EXPECT_DAMAGE("c/R17 one array bool 0x02");
    printf("c normalization is damage\n");
    return 0;
}
`

func cNormDamageRun(t *testing.T, sabotage [2]string) ([]byte, error) {
	t.Helper()
	cc := os.Getenv("CC")
	if cc == "" {
		cc = "cc"
	}
	if _, err := exec.LookPath(cc); err != nil {
		t.Skipf("cc: %v", err)
	}
	u := cUnitOf(t, cReadSchema(t, "VNEW_hostile_bool"))
	files, err := cgen.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tfiles, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tfiles)
	dir := t.TempDir()
	mutApplied := false
	header := ""
	var srcs []string
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if strings.HasSuffix(name, "Table.h") {
			header = name
		}
		if sabotage[0] != "" {
			if text := string(data); strings.Contains(text, sabotage[0]) {
				data = []byte(strings.ReplaceAll(text, sabotage[0], sabotage[1]))
				mutApplied = true
			}
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(name, ".c") {
			srcs = append(srcs, filepath.Join(dir, name))
		}
	}
	if sabotage[0] != "" && !mutApplied {
		t.Fatalf("sabotage target %q not found in generated files", sabotage[0])
	}
	if header == "" {
		t.Fatal("no Table.h was generated")
	}
	probe := strings.Replace(cNormDamageProbe, "VnewHostileBoolTable.h", header, 1)
	if err := os.WriteFile(filepath.Join(dir, "probe.c"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	args := []string{"-std=c99", "-Wall", "-Wextra", "-Werror", "-O1", "-ffp-contract=off"}
	args = append(args, gccFortifySilence(cc)...)
	args = append(args, "-I"+dir, filepath.Join(dir, "probe.c"))
	args = append(args, srcs...)
	args = append(args, "-o", filepath.Join(dir, "probe"), "-lm")
	if out, err := exec.Command(cc, args...).CombinedOutput(); err != nil {
		return out, fmt.Errorf("compile probe: %w", err)
	}
	return exec.Command(filepath.Join(dir, "probe")).CombinedOutput()
}

func TestCFixedNormalizationIsDamage(t *testing.T) {
	slowtest.Gate(t, "functional test: invokes the C compiler (cc) and runs a compiled C fixed-form probe")
	t.Parallel()
	out, err := cNormDamageRun(t, [2]string{})
	if err != nil {
		t.Fatalf("c/R17+C12+C13 wire_damaged probe failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "c normalization is damage") {
		t.Fatalf("missing completion banner:\n%s", out)
	}
	// c/C13 on its own optional schema, through the self-built helper.
	out2, err := cRunSelfBuilt(t, "VNEW_optional_add", nil, 0, `
    v.link_present = 0;
    v.lead = 0xAAAAAAAAu;
    v.trail = 0xBBBBBBBBu;`, `
    FORGE_U8( 4, 0x02 );`, `
    if ( n != -1 ) { printf( "C13: n=%lld, not a refusal\n", (long long) n ); return 1; }
    if ( !r.refused || r.reason != 0 || r.refuse_reason != SCHEMA_TABLE_REFUSE_WIRE_DAMAGED )
        { printf( "C13: a present byte other than 0 or 1 is not refused wire_damaged\n" ); return 1; }
    if ( r.unknown != 0 || r.widened != 0 ) { printf( "C13: counters moved\n" ); return 1; }
    if ( memcmp( &back[0], &fresh, sizeof( back[0] ) ) != 0 )
        { printf( "C13: a refusal landed a value\n" ); return 1; }`)
	if err != nil {
		t.Fatalf("c/C13 optional_add wire_damaged probe failed: %v\n%s", err, out2)
	}
}

func TestCFixedNormalizationIsDamageNegativeControl(t *testing.T) {
	slowtest.Gate(t, "functional negative control: invokes the C compiler (cc) on a mutated C fixed-form artifact")
	t.Parallel()
	// Plant the pre-fix answer in the real generated header: bool/present
	// damage counted as out_of_range instead of wire_damaged.
	out, err := cNormDamageRun(t, [2]string{
		"if ( damaged != 0 ) { report->refused = 1; report->refuse_reason = SCHEMA_TABLE_REFUSE_WIRE_DAMAGED; }",
		"if ( damaged != 0 ) { report->refused = 1; report->refuse_reason = SCHEMA_TABLE_REFUSE_OUT_OF_RANGE; }",
	})
	if err == nil {
		t.Fatalf("negative control failed: probe exited 0 when wire_damaged was planted back to out_of_range:\n%s", out)
	}
	if !strings.Contains(string(out), "not wire_damaged") {
		t.Fatalf("negative control failed for the wrong reason (want a wire_damaged refusal check):\n%s", out)
	}
}
