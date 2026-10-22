package ctable

// TestFixedRoadmapCT14CompressedFloats is c/R30: a compressed float rides as
// the float (docs/SPEC.md Floating-point discipline, docs/FIXED-FORM-ALGORITHM.md
// §5.1). The dequantize fixture is index 2 under min=-1, max=2, resolution=0.1
// (count=30, delta=3): unfused bits 0xbf4ccccc, fused bits 0xbf4ccccd.

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

const t14Old = `package cfold

fixed table Reading
{
    lead uint32 = 1
    aim float32 | min = -1, max = 2, resolution = 0.1
    trail uint32 = 2
}
`

const t14Fine = `package cfold

fixed table Reading
{
    lead uint32 = 1
    aim float32 | min = -1, max = 2, resolution = 0.01
    trail uint32 = 2
}
`

const t14Dropped = `package cfold

fixed table Reading
{
    lead uint32 = 1
    aim float32
    trail uint32 = 2
}
`

func TestFixedRoadmapCT14CompressedFloats(t *testing.T) {
	t.Parallel()
	t.Run("c/R30", func(t *testing.T) {
		t.Parallel()
		slowtest.Gate(t, "the C compiler (it compiles and runs the generated unit)")
		cc := os.Getenv("CC")
		if cc == "" {
			cc = "cc"
		}
		if _, err := exec.LookPath(cc); err != nil {
			t.Skipf("cc: %v", err)
		}
		t14CiteCoarser(t)
		src := t14Emit(t, t14Old, nil)
		if !strings.Contains(src, "table_fixed_putf32(") {
			t.Fatal("the fixed write does not store the float32 with table_fixed_putf32")
		}
		if !strings.Contains(src, "SCHEMA_TABLE_FLOAT_FORCE_ROUND") {
			t.Fatal("the emitted quantize does not force the product to round before the add")
		}
		dir := t.TempDir()
		file := filepath.Join(dir, "old.bin")
		t14Run(t, cc, t14Old, nil, t14WriteProbe(file), "")
		t14Run(t, cc, t14Old, nil, t14ReadProbe(file), "")
		t14Run(t, cc, t14Fine, []string{t14Old}, t14ReadProbe(file), "")
		t14Run(t, cc, t14Dropped, []string{t14Old}, t14ReadProbe(file), "")
		t14Run(t, cc, t14Old, nil, t14ContractProbe(), "")
	})
}

func t14CiteCoarser(t *testing.T) {
	t.Helper()
	b, err := os.ReadFile("../../lockfile/cfloatrange_test.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, needle := range []string{
		"func TestLockCFloatResolutionCoarsenedRefuses",
		"resolution coarsened",
	} {
		if !strings.Contains(s, needle) {
			t.Fatalf("the lockfile citation is gone: %s", needle)
		}
	}
}

func t14Emit(t *testing.T, schema string, older []string) string {
	t.Helper()
	u := unitFrom(t, schema)
	lineage := map[string][]FixedLineageEntry{}
	for _, src := range older {
		o := unitFrom(t, src)
		for _, st := range ir.TableFixedRoots(o) {
			e, ok := FixedLineageOf(o, st.Name)
			if !ok {
				t.Fatalf("no lineage entry for %s", st.Name)
			}
			lineage[st.Name] = append(lineage[st.Name], e)
		}
	}
	files, err := cgen.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := GenerateLineage(u, lineage)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	var b strings.Builder
	for _, data := range files {
		b.Write(data)
	}
	return b.String()
}

func t14Run(t *testing.T, cc, schema string, older []string, probe, extra string) {
	t.Helper()
	u := unitFrom(t, schema)
	lineage := map[string][]FixedLineageEntry{}
	for _, src := range older {
		o := unitFrom(t, src)
		for _, st := range ir.TableFixedRoots(o) {
			e, ok := FixedLineageOf(o, st.Name)
			if !ok {
				t.Fatalf("no lineage entry for %s", st.Name)
			}
			lineage[st.Name] = append(lineage[st.Name], e)
		}
	}
	files, err := cgen.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := GenerateLineage(u, lineage)
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
		t.Fatal("no Table.h")
	}
	source := fmt.Sprintf("#include <stdio.h>\n#include <string.h>\n#include <stdint.h>\n#include \"%s\"\n%s\n", header, probe)
	if err := os.WriteFile(filepath.Join(dir, "probe.c"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	args := append([]string{"-std=c99", "-O2", "-I" + dir}, gccFortifySilence(cc)...)
	if extra != "" {
		args = append(args, extra)
	}
	args = append(args, filepath.Join(dir, "probe.c"), "-o", filepath.Join(dir, "probe"), "-lm")
	if out, err := exec.Command(cc, args...).CombinedOutput(); err != nil {
		t.Fatalf("compile: %v\n%s", err, out)
	}
	if out, err := exec.Command(filepath.Join(dir, "probe")).CombinedOutput(); err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
}

func t14WriteProbe(file string) string {
	return fmt.Sprintf(`
int main(void) {
    Reading v;
    union { uint32_t u; float f; } bits;
    uint8_t data[1 << 16];
    uint8_t raw[4] = { 0x9a, 0x99, 0x99, 0x3e };
    int64_t n;
    int i;
    FILE * f;
    reading_reset(&v);
    v.lead = 1;
    bits.u = 0x3E99999Au;
    v.aim = bits.f;
    v.trail = 2;
    n = reading_fixed_save(&v, 1, data, (int64_t)sizeof data);
    if (n < 0) { printf("save failed\n"); return 1; }
    for (i = 0; i + 4 <= n; i++) { if (memcmp(data + i, raw, 4) == 0) break; }
    if (i + 4 > n) { printf("the file does not carry the float32 bits\n"); return 1; }
    f = fopen(%q, "wb");
    if (f == NULL || fwrite(data, 1, (size_t)n, f) != (size_t)n) { printf("write failed\n"); return 1; }
    fclose(f);
    return 0;
}
`, file)
}

func t14ReadProbe(file string) string {
	return fmt.Sprintf(`
int main(void) {
    uint8_t data[1 << 16];
    Reading back[1];
    TableReport r;
    static TableFixedEntry plan[4096];
    union { uint32_t u; float f; } bits;
    FILE * f = fopen(%q, "rb");
    int64_t n, len;
    if (f == NULL) { printf("missing file\n"); return 1; }
    len = (int64_t)fread(data, 1, sizeof data, f);
    fclose(f);
    memset(&r, 0, sizeof r);
    reading_reset(&back[0]);
    n = reading_fixed_load(back, 1, data, len, plan, 4096, NULL, &r);
    if (n != 1) { printf("load refused n=%%lld\n", (long long)n); return 1; }
    bits.f = back[0].aim;
    if (bits.u != 0x3E99999Au || back[0].lead != 1 || back[0].trail != 2) {
        printf("value moved %%08x lead %%u trail %%u\n", bits.u, back[0].lead, back[0].trail);
        return 1;
    }
    if (r.clamped != 0) { printf("clamped %%d\n", r.clamped); return 1; }
    return 0;
}
`, file)
}

func t14ContractProbe() string {
	return `
int main(void) {
    /* min=-1 max=2 res=0.1 count=30 index=2: unfused 0xbf4ccccc, fused 0xbf4ccccd. */
    union { uint32_t u; float f; } bits;
    bits.f = table_message_dequantize(2, -1.0f, 3.0f, 30);
    if (bits.u != 0xbf4cccccu) {
        printf("dequantize %#x, want unfused 0xbf4ccccc (fused would be 0xbf4ccccd)\n", bits.u);
        return 1;
    }
    return 0;
}
`
}
