package ctable

// FULL-WIDTH ENUM AND UNION ORDINALS ON THE C LEG (docs/roadmap.sexp node
// `fixed-tables`, leaf c/R31; docs/FIXED-FORM-ALGORITHM.md §4.1, §5.2, §5.8
// rows 5, 7 and 14). The same fixture as the Go leg's t16: a writer with 300
// enum variants and 300 union arms, a reader with a subset, an 8-byte
// ordinal pinning the 64-bit temporary, both guard lanes stamped above
// MaxUint32, and a remap past the uint16 length word refused by name.
//
// Assertions on the emitted header are unit tests. The compiled probe is a
// functional test: slowtest.Gate first, then the compiler by exec.LookPath,
// skipping by name when it is absent.

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
	"github.com/mas-bandwidth/schema/v2/ir"
)

func t16CNames(prefix string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("%s%d", prefix, i)
	}
	return out
}

func t16CFixture(many, pick, outer []string) string {
	var b strings.Builder
	b.WriteString("package probe\n\ntype Cell { n int32 }\n\nenum Many { ")
	b.WriteString(strings.Join(many, ", "))
	b.WriteString(" }\n\nunion Pick {\n")
	for _, a := range pick {
		fmt.Fprintf(&b, "    %s Cell\n", a)
	}
	b.WriteString("}\n\ntype Holder { inner Inner }\n\nunion Inner {\n    x Cell\n    y Cell\n}\n\nunion Outer {\n")
	for _, a := range outer {
		if a == "a299" {
			fmt.Fprintf(&b, "    %s Holder\n", a)
		} else {
			fmt.Fprintf(&b, "    %s Cell\n", a)
		}
	}
	b.WriteString("}\n\nfixed table Root {\n    many Many\n    pick Pick\n    top Outer\n    seq int32\n}\n")
	return b.String()
}

func t16CWriterSchema() string {
	return t16CFixture(t16CNames("v", 300), t16CNames("a", 300), t16CNames("a", 300))
}

func t16CReaderSchema() string {
	return t16CFixture([]string{"v0", "v1", "v299"}, []string{"a0", "a299"}, []string{"a0", "a299"})
}

func TestFixedRoadmapCT16OrdinalLanes(t *testing.T) {
	t.Parallel()
	t.Run("c/R31", func(t *testing.T) {
		t.Parallel()
		t16CR31(t)
	})
}

func t16CR31(t *testing.T) {
	// THE HEADER CARRIES THE FULL-WIDTH LANES (§4.1) AND ARM 300 WHOLE.
	header := cT08Header(t, t16CWriterSchema())
	for _, want := range []string{"uint64_t arg;", "uint64_t arg2;"} {
		if !strings.Contains(header, want) {
			t.Errorf("R31: the emitted plan does not carry %q (FIXED-FORM-ALGORITHM.md §4.1)", want)
		}
	}
	rows, _ := cT08PlanRows(t, header, "root")
	found300 := false
	for _, r := range rows {
		if len(r) > 6 && r[6] == "300" {
			found300 = true
		}
	}
	if !found300 {
		t.Error("R31: the published identity plan carries no entry with arg 300; arm 300's ordinal truncated")
	}

	slowtest.Gate(t, "the C toolchain (it compiles and runs the generated unit)")
	cc := os.Getenv("CC")
	if cc == "" {
		cc = "cc"
	}
	if _, err := exec.LookPath(cc); err != nil {
		t.Skipf("the C compiler %q is absent: %v", cc, err)
	}
	writer := unitFrom(t, t16CWriterSchema())
	wst := writer.Tables["Root"]
	wlayout := ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(wst))
	var arr strings.Builder
	for i, by := range wlayout {
		if i > 0 {
			arr.WriteString(",")
		}
		fmt.Fprintf(&arr, "%d", by)
	}
	t16CProbe(t, cc, arr.String(), len(wlayout))
}

// t16CProbe generates the reader's C, compiles a probe that compiles the
// WRITER's layout against it and runs the plan, and executes the probe.
func t16CProbe(t *testing.T, cc, writerArray string, writerLen int) {
	t.Helper()
	u := unitFrom(t, t16CReaderSchema())
	files, err := cgen.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	var header string
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(name, "Table.h") {
			header = name
		}
	}
	if header == "" {
		t.Fatal("no Table.h was generated")
	}
	probe := fmt.Sprintf(`#include <stdio.h>
#include <string.h>
#include "%s"
#define CHECK(x) do { if (!(x)) { fprintf(stderr, "line %%d: %%s\n", __LINE__, #x); return 1; } } while (0)
static const uint8_t writer_layout[] = {%s};
int main(void)
{
    static TableFixedEntry plan[8192];
    static TableFixedEntry bigplan[8192];
    TableFixedLayoutView parsed;
    TableReport r;
    int why = SCHEMA_TABLE_LAYOUT_MALFORMED;
    int32_t made, guarded = 0, fill_count = 0, i, ordinals = 0;
    uint32_t fill_at = 0;
    uint8_t body[19];
    CHECK(table_fixed_parse_layout(writer_layout, %d, &parsed, &why));
    memset(&r, 0, sizeof(r));
    made = table_fixed_compile(&parsed, root_fixed_layout, (int32_t) root_fixed_layout_bytes, root_fixed_dst,
                               root_fixed_cover, root_fixed_cover_count, plan, 8192, &guarded, &fill_at, &fill_count, &r);
    CHECK(made > 0);
    /* THE REMAP TABLE'S LENGTH WORD IS THE WRITER'S VARIANT COUNT (§5.2). */
    for (i = 0; i < made; ++i)
    {
        if (plan[i].op == kTableFixedOrdinal)
        {
            const uint16_t * table = (const uint16_t *) (const void *) ((const uint8_t *) plan + plan[i].aux);
            ++ordinals;
            if (table[0] != 300) { fprintf(stderr, "remap length word %%u, want writer 300\n", (unsigned) table[0]); return 1; }
            if (table[300] != 3) { fprintf(stderr, "remap[300] %%u, want reader ordinal 3\n", (unsigned) table[300]); return 1; }
        }
    }
    CHECK(ordinals == 1);
    /* THE COMPILED RUN: variant/arm 300 remap to the reader's last entries. */
    memset(body, 0, sizeof(body));
    body[0] = 0x2C; body[1] = 0x01;          /* many ordinal 300 */
    body[2] = 0x2C; body[3] = 0x01;          /* pick tag 300 */
    body[4] = 0x09; body[5] = 0x03;          /* pick payload 777 */
    body[8] = 0x2C; body[9] = 0x01;          /* top outer tag 300 */
    body[10] = 2;                            /* inner arm 2 */
    body[11] = 0x78; body[12] = 0x03;        /* inner payload 888 */
    body[15] = 9;                            /* seq */
    {
        Root back;
        root_reset(&back);
        memset(&r, 0, sizeof(r));
        table_fixed_run(plan, made, guarded, body, (uint8_t *) (void *) &back, &r, NULL);
        CHECK(back.many == MANY_V299);
        CHECK(back.pick.type == PICK_TYPE_A299);
        CHECK(back.pick.as.a299.n == 777);
        CHECK(back.top.type == OUTER_TYPE_A299);
        CHECK(back.top.as.a299.inner.type == INNER_TYPE_Y);
        CHECK(back.top.as.a299.inner.as.y.n == 888);
        CHECK(back.seq == 9);
        /* FORGED ORDINAL PAST THE WRITER'S SET LANDS None. */
        body[0] = 0x2D; body[1] = 0x01;          /* 301 */
        root_reset(&back);
        table_fixed_run(plan, made, guarded, body, (uint8_t *) (void *) &back, &r, NULL);
        CHECK(back.many == MANY_NONE);
    }
    /* BOTH GUARD LANES HOLD AN ORDINAL ABOVE MaxUint32 (§4.1). */
    {
        uint8_t src[24];
        uint8_t out[4];
        TableFixedEntry e = table_fixed_entry_zero();
        uint64_t tag = 4294967297ull;
        memset(src, 0, sizeof(src));
        memcpy(src, &tag, 8);
        memcpy(src + 8, &tag, 8);
        src[16] = 7; src[17] = 7; src[18] = 7; src[19] = 7;
        e.src = 16; e.dst = 0; e.size = 4; e.guard = 0; e.arg = tag; e.argw = 8;
        e.guard2 = 8; e.arg2 = tag; e.argw2 = 8; e.op = kTableFixedCopy;
        memset(out, 0, sizeof(out));
        memset(&r, 0, sizeof(r));
        table_fixed_run(&e, 1, 0, src, out, &r, NULL);
        CHECK(out[0] == 7 && out[3] == 7);
        e.arg = 1;
        memset(out, 0, sizeof(out));
        table_fixed_run(&e, 1, 0, src, out, &r, NULL);
        CHECK(out[0] == 0);
    }
    /* THE 64-BIT ORDINAL TEMPORARY (§4.5) AND THE LENGTH-WORD REFUSAL. */
    {
        TableFixedCompiler c;
        uint16_t vals[3] = {1, 2, 3};
        uint32_t aux;
        TableFixedEntry e = table_fixed_entry_zero();
        uint8_t src[8];
        uint8_t out[8];
        uint64_t raw = 4294967297ull;
        table_fixed_compiler_init(&c);
        c.plan = bigplan; c.capacity = 8192;
        aux = table_fixed_lay_table(&c, vals, 3);
        CHECK(!c.overflow);
        e.src = 0; e.dst = 0; e.size = 8; e.dstsize = 8; e.aux = aux; e.op = kTableFixedOrdinal;
        bigplan[0] = e;
        memcpy(src, &raw, 8);
        memset(out, 0, sizeof(out));
        memset(&r, 0, sizeof(r));
        table_fixed_run(bigplan, 1, 1, src, out, &r, NULL);
        CHECK(out[0] == 0 && out[7] == 0);
        raw = 70000;
        memcpy(src, &raw, 8);
        memset(out, 0, sizeof(out));
        table_fixed_run(bigplan, 1, 1, src, out, &r, NULL);
        CHECK(out[0] == 0 && out[7] == 0);
        raw = 2;
        memcpy(src, &raw, 8);
        memset(out, 0, sizeof(out));
        table_fixed_run(bigplan, 1, 1, src, out, &r, NULL);
        CHECK(out[0] == 2);
        /* PAST THE uint16 LENGTH WORD THE PLAN REFUSES (§5.8 row 14):
           lay_table records the overflow compile surfaces as plan_too_large. */
        table_fixed_compiler_init(&c);
        c.plan = bigplan; c.capacity = 8192;
        (void) table_fixed_lay_table(&c, NULL, 65536);
        CHECK(c.overflow == 1);
    }
    return 0;
}
`, header, writerArray, writerLen)
	if err := os.WriteFile(filepath.Join(dir, "probe.c"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	args := append([]string{"-std=c99", "-O1", "-I" + dir}, gccFortifySilence(cc)...)
	args = append(args, filepath.Join(dir, "probe.c"), "-o", filepath.Join(dir, "probe"), "-lm")
	if out, err := exec.Command(cc, args...).CombinedOutput(); err != nil {
		t.Fatalf("the probe did not compile: %v\n%s", err, out)
	}
	if out, err := exec.Command(filepath.Join(dir, "probe")).CombinedOutput(); err != nil {
		t.Fatalf("the probe failed: %v\n%s", err, out)
	}
}
