package cpptable

// TestCppFixedNormalizationIsDamage holds cpp/R17, cpp/C12 and cpp/C13 under
// the strict read: a bool or present byte other than 0 or 1 refuses
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

	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpp"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

const cppNormDamageProbe = `#include <cstdio>
#include <cstring>
#include <cstdint>
#include "VOLD_hostile_boolTable.h"
#include "VNEW_hostile_boolTable.h"

#define CHECK(x) do { if (!(x)) { std::printf("FAIL: line %d: %s\n", __LINE__, #x); return 1; } } while (0)

static vnew_hostile_bool::Hbb fresh;
static vnew_hostile_bool::Hbb back[8];
static vnew_hostile_bool::TableReport r;
static vnew_hostile_bool::TableFixedEntry plan[4096];
static uint8_t data[1 << 20];
static uint8_t orig[1 << 20];
static uint8_t old_data[1 << 20];
static uint8_t old_orig[1 << 20];
static int64_t written;
static int64_t old_written;
static int64_t n;

#define RECORD0() (data + vnew_hostile_bool::kTableFixedHeaderBytes + 4 + vnew_hostile_bool::HbbFixedLayoutBytes + 8)
#define OLD_RECORD0() (old_data + vold_hostile_bool::kTableFixedHeaderBytes + 4 + vold_hostile_bool::HbbFixedLayoutBytes + 8)

#define EXPECT_DAMAGE(WHO) do { \
    if (n != -1) { std::printf("%s: n=%lld, not -1\n", WHO, (long long)n); return 1; } \
    if (!r.refused || r.damaged || r.reason != vnew_hostile_bool::wire_damaged || r.refuse_reason != vnew_hostile_bool::wire_damaged) \
        { std::printf("%s: refused=%d damaged=%d reason=%d refuse_reason=%d, not wire_damaged\n", WHO, (int)r.refused, (int)r.damaged, (int)r.reason.value, (int)r.refuse_reason); return 1; } \
    if (r.unknown != 0 || r.widened != 0 || r.layout_hash != 0) \
        { std::printf("%s: counters moved on wire_damaged refusal\n", WHO); return 1; } \
    if (std::memcmp(&back[0], &fresh, sizeof(fresh)) != 0) \
        { std::printf("%s: a refusal landed a value\n", WHO); return 1; } } while (0)

#define LOAD_FORGED() do { \
    std::memcpy(data, orig, (size_t)written); \
    std::memcpy(old_data, old_orig, (size_t)old_written); \
    std::memset(&fresh, 0, sizeof(fresh)); vnew_hostile_bool::HbbReset(fresh); \
    std::memset(back, 0, sizeof(back)); vnew_hostile_bool::HbbReset(back[0]); \
    r = vnew_hostile_bool::TableReport(); } while (0)

int main(void)
{
    vnew_hostile_bool::Hbb v;
    vnew_hostile_bool::HbbReset(v);
    v.flag = true;
    v.link_present = true;
    v.link.n = 5;
    v.lead = 0xAAAAAAAAu;
    for (int i = 0; i < 8; ++i) { v.flags[i] = (i & 1) != 0; }
    v.trail = 0xBBBBBBBBu;
    v.extra = 0xCCCCCCCCu;
    written = vnew_hostile_bool::HbbFixedSave(&v, 1, data, (int64_t)sizeof(data));
    if (written < 0) { std::printf("save failed\n"); return 1; }
    std::memcpy(orig, data, (size_t)written);

    vold_hostile_bool::Hbb ov;
    vold_hostile_bool::HbbReset(ov);
    ov.flag = true;
    ov.link_present = true;
    ov.link.n = 5;
    ov.lead = 0xAAAAAAAAu;
    for (int i = 0; i < 8; ++i) { ov.flags[i] = (i & 1) != 0; }
    ov.trail = 0xBBBBBBBBu;
    old_written = vold_hostile_bool::HbbFixedSave(&ov, 1, old_data, (int64_t)sizeof(old_data));
    if (old_written < 0) { std::printf("old save failed\n"); return 1; }
    std::memcpy(old_orig, old_data, (size_t)old_written);

    /* Clean 0/1 bytes still read whole on both plans. */
    LOAD_FORGED();
    n = vnew_hostile_bool::HbbFixedLoad(back, 8, data, written, plan, 4096, NULL, &r);
    CHECK(n == 1 && !r.refused && !r.damaged && r.refuse_reason == vnew_hostile_bool::ok);
    CHECK(back[0].flag && back[0].link_present && back[0].link.n == 5);
    CHECK(back[0].lead == 0xAAAAAAAAu && back[0].trail == 0xBBBBBBBBu && back[0].extra == 0xCCCCCCCCu);
    for (int i = 0; i < 8; ++i) { CHECK(back[0].flags[i] == ((i & 1) != 0)); }

    LOAD_FORGED();
    n = vnew_hostile_bool::HbbFixedLoad(back, 8, old_data, old_written, plan, 4096, NULL, &r);
    CHECK(n == 1 && !r.refused && !r.damaged && r.refuse_reason == vnew_hostile_bool::ok);
    CHECK(back[0].flag && back[0].link_present && back[0].link.n == 5);
    CHECK(back[0].lead == 0xAAAAAAAAu && back[0].trail == 0xBBBBBBBBu && back[0].extra == 9u);

    /* cpp/C12: a scalar bool byte of 0x02 or 0xFF is damage. */
    LOAD_FORGED();
    RECORD0()[0] = 0x02;
    n = vnew_hostile_bool::HbbFixedLoad(back, 8, data, written, plan, 4096, NULL, &r);
    EXPECT_DAMAGE("cpp/C12 bool 0x02");

    LOAD_FORGED();
    RECORD0()[0] = 0xFF;
    n = vnew_hostile_bool::HbbFixedLoad(back, 8, data, written, plan, 4096, NULL, &r);
    EXPECT_DAMAGE("cpp/C12 bool 0xFF");

    /* cpp/C13: a present byte of 0x02 or 0xFF is damage. */
    LOAD_FORGED();
    RECORD0()[1] = 0x02;
    n = vnew_hostile_bool::HbbFixedLoad(back, 8, data, written, plan, 4096, NULL, &r);
    EXPECT_DAMAGE("cpp/C13 present 0x02");

    LOAD_FORGED();
    RECORD0()[1] = 0xFF;
    n = vnew_hostile_bool::HbbFixedLoad(back, 8, data, written, plan, 4096, NULL, &r);
    EXPECT_DAMAGE("cpp/C13 present 0xFF");

    /* cpp/R17: bool, present and every bool-array byte forged together, or one array element alone, on both identity and compiled plans. */
    LOAD_FORGED();
    RECORD0()[0] = 0x02;
    RECORD0()[1] = 0x02;
    for (int i = 0; i < 8; ++i) { RECORD0()[10 + i] = 0x02; }
    n = vnew_hostile_bool::HbbFixedLoad(back, 8, data, written, plan, 4096, NULL, &r);
    EXPECT_DAMAGE("cpp/R17 bool+present+array 0x02");

    LOAD_FORGED();
    RECORD0()[10 + 3] = 0x02;
    n = vnew_hostile_bool::HbbFixedLoad(back, 8, data, written, plan, 4096, NULL, &r);
    EXPECT_DAMAGE("cpp/R17 one array bool 0x02");

    LOAD_FORGED();
    OLD_RECORD0()[0] = 0x02;
    n = vnew_hostile_bool::HbbFixedLoad(back, 8, old_data, old_written, plan, 4096, NULL, &r);
    EXPECT_DAMAGE("cpp/R17 compiled plan bool 0x02");

    LOAD_FORGED();
    OLD_RECORD0()[1] = 0x02;
    n = vnew_hostile_bool::HbbFixedLoad(back, 8, old_data, old_written, plan, 4096, NULL, &r);
    EXPECT_DAMAGE("cpp/R17 compiled plan present 0x02");

    LOAD_FORGED();
    OLD_RECORD0()[10 + 5] = 0x02;
    n = vnew_hostile_bool::HbbFixedLoad(back, 8, old_data, old_written, plan, 4096, NULL, &r);
    EXPECT_DAMAGE("cpp/R17 compiled plan array bool 0x02");

    std::printf("cpp normalization is damage\n");
    return 0;
}
`

func cppNormDamageRun(t *testing.T, sabotage [2]string) ([]byte, error) {
	t.Helper()
	cxx := os.Getenv("CXX")
	if cxx == "" {
		cxx = "c++"
	}
	if _, err := exec.LookPath(cxx); err != nil {
		t.Skipf("c++: %v", err)
	}
	schemaDir := t.TempDir()
	for _, side := range []string{"VOLD_hostile_bool.schema", "VNEW_hostile_bool.schema"} {
		raw, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "tables", side))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(schemaDir, side), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	uOld := loadUnit(t, filepath.Join(schemaDir, "VOLD_hostile_bool.schema"))
	uNew := loadUnit(t, filepath.Join(schemaDir, "VNEW_hostile_bool.schema"))

	files, err := cpp.Generate(uOld)
	if err != nil {
		t.Fatal(err)
	}
	tOld, err := GenerateLineage(uOld, nil)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tOld)

	fNew, err := cpp.Generate(uNew)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, fNew)
	tNew, err := GenerateLineage(uNew, nil)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tNew)

	dir := t.TempDir()
	mutApplied := false
	var srcs []string
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if sabotage[0] != "" && name == "VNEW_hostile_boolTable.h" {
			if text := string(data); strings.Contains(text, sabotage[0]) {
				data = []byte(strings.ReplaceAll(text, sabotage[0], sabotage[1]))
				mutApplied = true
			}
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(name, ".cpp") {
			srcs = append(srcs, filepath.Join(dir, name))
		}
	}
	if sabotage[0] != "" && !mutApplied {
		t.Fatalf("sabotage target %q not found in VNEW_hostile_boolTable.h", sabotage[0])
	}
	if err := os.WriteFile(filepath.Join(dir, "probe.cpp"), []byte(cppNormDamageProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	args := []string{"-std=c++17", "-O1", "-I" + dir, filepath.Join(dir, "probe.cpp")}
	args = append(args, srcs...)
	args = append(args, "-o", filepath.Join(dir, "probe"))
	if out, err := exec.Command(cxx, args...).CombinedOutput(); err != nil {
		return out, fmt.Errorf("compile probe: %w", err)
	}
	return exec.Command(filepath.Join(dir, "probe")).CombinedOutput()
}

func TestCppFixedNormalizationIsDamage(t *testing.T) {
	slowtest.Gate(t, "functional test: invokes the C++ compiler (c++) and runs a compiled C++ fixed-form probe")
	t.Parallel()
	out, err := cppNormDamageRun(t, [2]string{})
	if err != nil {
		t.Fatalf("cpp/R17+C12+C13 wire_damaged probe failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "cpp normalization is damage") {
		t.Fatalf("missing completion banner:\n%s", out)
	}
}

func TestCppFixedNormalizationIsDamageNegativeControl(t *testing.T) {
	slowtest.Gate(t, "functional negative control: invokes the C++ compiler (c++) on a mutated C++ fixed-form artifact")
	t.Parallel()
	out, err := cppNormDamageRun(t, [2]string{
		"report->reason = wire_damaged;\n                    report->refuse_reason = wire_damaged;",
		"report->reason = out_of_range;\n                    report->refuse_reason = out_of_range;",
	})
	if err == nil {
		t.Fatalf("negative control failed: probe exited 0 when wire_damaged was planted to out_of_range:\n%s", out)
	}
	if !strings.Contains(string(out), "not wire_damaged") {
		t.Fatalf("negative control failed for the wrong reason (want a wire_damaged refusal check):\n%s", out)
	}
}
