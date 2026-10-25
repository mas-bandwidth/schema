package cpptable

// TestFixedRoadmapCppT14CompressedFloats is cpp/R30: a compressed float rides
// as the float (docs/SPEC.md Floating-point discipline, docs/FIXED-FORM-ALGORITHM.md
// §5.1). The dequantize fixture is index 2 under min=-1, max=2, resolution=0.1
// (count=30, delta=3): unfused bits 0xbf4ccccc, fused bits 0xbf4ccccd.
//
// The cpp probe does not include serialize.h: this unit has no 128-bit storage,
// and the serialize sibling is not beside this checkout.

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpp"
	"github.com/mas-bandwidth/schema/v2/internal/lockfile"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestFixedRoadmapCppT14CompressedFloats(t *testing.T) {
	t.Parallel()
	t.Run("cpp/R30", func(t *testing.T) {
		t.Parallel()
		slowtest.Gate(t, "the C++ compiler (it compiles and runs the generated unit)")
		cxx := os.Getenv("CXX")
		if cxx == "" {
			cxx = "c++"
		}
		if _, err := exec.LookPath(cxx); err != nil {
			t.Skipf("c++: %v", err)
		}
		t14Cite(t)
		src := t14CppEmit(t, t14Schema(0.1), nil)
		if !strings.Contains(src, "TableFixedPutF32(") {
			t.Fatal("the fixed write does not store the float32 with TableFixedPutF32")
		}
		if !strings.Contains(src, "TABLE_FLOAT_FORCE_ROUND") {
			t.Fatal("the emitted quantize does not force the product to round before the add")
		}
		dir := t.TempDir()
		file := filepath.Join(dir, "old.bin")
		old := t14Schema(0.1)
		t14CppRun(t, cxx, old, nil, t14CppWrite(file))
		t14CppRun(t, cxx, old, nil, t14CppRead(file))
		t14CppRun(t, cxx, t14Schema(0.01), []string{old}, t14CppRead(file))
		t14CppRun(t, cxx, t14DroppedSchema(), []string{old}, t14CppRead(file))
		t14CppRun(t, cxx, old, nil, t14CppContract())
	})
}

func t14Schema(res float64) string {
	return fmt.Sprintf(`package cfold

fixed table Reading
{
    lead uint32 = 1
    aim float32 | min = -1, max = 2, resolution = %g
    trail uint32 = 2
}
`, res)
}

func t14DroppedSchema() string {
	return `package cfold

fixed table Reading
{
    lead uint32 = 1
    aim float32
    trail uint32 = 2
}
`
}

func t14Cite(t *testing.T) {
	t.Helper()
	b, err := os.ReadFile("../../lockfile/cfloatrange_test.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "func TestLockCFloatResolutionCoarsenedRefuses") || !strings.Contains(string(b), "resolution coarsened") {
		t.Fatal("the lockfile citation is gone")
	}
}

func t14CppLock(t *testing.T, older []string) *lockfile.Unit {
	t.Helper()
	if len(older) == 0 {
		return nil
	}
	var entries []lockfile.LineageEntry
	for _, src := range older {
		u := t02CppUnit(t, src)
		st := ir.TableFixedRoots(u)[0]
		layout := ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(st))
		entries = append(entries, lockfile.LineageEntry{
			Wire:   ir.TableFixedLayoutHash(layout, st),
			Layout: layout,
			Record: ir.TableFixedTypeBytes(st),
		})
	}
	return &lockfile.Unit{
		Version: 1,
		Package: "cfold",
		Tables: []lockfile.Table{{
			Decl:    lockfile.DeclFixedTable,
			Name:    "Reading",
			Lineage: entries,
		}},
	}
}

func t14CppEmit(t *testing.T, schema string, older []string) string {
	t.Helper()
	u := t02CppUnit(t, schema)
	files, err := cpp.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := GenerateLineage(u, t14CppLock(t, older))
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

func t14CppRun(t *testing.T, cxx, schema string, older []string, probe string) {
	t.Helper()
	u := t02CppUnit(t, schema)
	files, err := cpp.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	lock := t14CppLock(t, older)
	if lock != nil {
		st := ir.TableFixedRoots(u)[0]
		layout := ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(st))
		lock.Tables[0].Lineage = append(lock.Tables[0].Lineage, lockfile.LineageEntry{
			Wire:   ir.TableFixedLayoutHash(layout, st),
			Layout: layout,
			Record: ir.TableFixedTypeBytes(st),
		})
	}
	tables, err := GenerateLineage(u, lock)
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
	source := fmt.Sprintf(`#include <stdio.h>
#include <string.h>
#include <stdint.h>
#include <vector>
#include "%s"
using namespace cfold;
%s
`, header, probe)
	if err := os.WriteFile(filepath.Join(dir, "probe.cpp"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	args := []string{"-std=c++17", "-O2", "-I" + dir, filepath.Join(dir, "probe.cpp"), "-o", filepath.Join(dir, "probe")}
	if out, err := exec.Command(cxx, args...).CombinedOutput(); err != nil {
		t.Fatalf("compile: %v\n%s", err, out)
	}
	if out, err := exec.Command(filepath.Join(dir, "probe")).CombinedOutput(); err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
}

func t14CppWrite(file string) string {
	return fmt.Sprintf(`
int main() {
    Reading v;
    union { uint32_t u; float f; } bits;
    uint8_t data[1 << 16];
    uint8_t raw[4] = { 0x9a, 0x99, 0x99, 0x3e };
    ReadingReset(v);
    v.lead = 1;
    bits.u = 0x3E99999Au;
    v.aim = bits.f;
    v.trail = 2;
    int64_t n = ReadingFixedSave(&v, 1, data, (int64_t)sizeof data);
    if (n < 0) { printf("save failed\n"); return 1; }
    int found = 0;
    for (int64_t i = 0; i + 4 <= n; i++) { if (memcmp(data + i, raw, 4) == 0) found = 1; }
    if (!found) { printf("the file does not carry the float32 bits\n"); return 1; }
    FILE * f = fopen(%q, "wb");
    if (f == NULL || fwrite(data, 1, (size_t)n, f) != (size_t)n) { printf("write failed\n"); return 1; }
    fclose(f);
    return 0;
}
`, file)
}

func t14CppRead(file string) string {
	return fmt.Sprintf(`
int main() {
    uint8_t data[1 << 16];
    Reading back;
    TableReport r;
    static TableFixedEntry plan[4096];
    union { uint32_t u; float f; } bits;
    FILE * f = fopen(%q, "rb");
    if (f == NULL) { printf("missing file\n"); return 1; }
    int64_t len = (int64_t)fread(data, 1, sizeof data, f);
    fclose(f);
    memset(&r, 0, sizeof r);
    ReadingReset(back);
    int64_t n = ReadingFixedLoad(&back, 1, data, len, plan, 4096, NULL, &r);
    if (n != 1) { printf("load refused n=%%lld\n", (long long)n); return 1; }
    bits.f = back.aim;
    if (bits.u != 0x3E99999Au || back.lead != 1 || back.trail != 2) {
        printf("value moved %%08x\n", bits.u);
        return 1;
    }
    if (r.clamped != 0) { printf("clamped\n"); return 1; }
    return 0;
}
`, file)
}

func t14CppContract() string {
	return `
int main() {
    /* min=-1 max=2 res=0.1 count=30 index=2: unfused 0xbf4ccccc, fused 0xbf4ccccd. */
    union { uint32_t u; float f; } bits;
    bits.f = TableMessageDequantize(2, -1.0f, 3.0f, 30);
    if (bits.u != 0xbf4cccccu) {
        printf("dequantize %#x, want unfused 0xbf4ccccc (fused would be 0xbf4ccccd)\n", bits.u);
        return 1;
    }
    return 0;
}
`
}
