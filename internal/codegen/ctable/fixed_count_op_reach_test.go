package ctable

// C twin of cpptable TestFixedCountOpReachCoversItsHeader. A count entry
// reads four bytes at src. Reach checks that span, and kind 14 refuses a
// counted local field when the peer array is shorter than the count word.
// Hostile compile is layout_record_too_large, the epilogue's name for every
// hostile plan (R14), not layout_malformed.

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

const countOpSpan = "const uint64_t span = ( e.op == kTableFixedCount ) ? 4ull : (uint64_t) e.size;"

const countOpRefuseC = "if ( d->counted && te.size < 4u ) { c->hostile = 1; break; }"

const countOpLocalSchema = `package probe
fixed table Root {
 lead uint64
 tail [..1]uint8
}
`

const countOpPeerSchema = `package probe
fixed table Root {
 lead uint64
 tail [1]uint8
}
`

func countOpBetween(src, start, end string) string {
	i := strings.Index(src, start)
	if i < 0 {
		return ""
	}
	rest := src[i:]
	j := strings.Index(rest[len(start):], end)
	if j < 0 {
		return rest
	}
	return rest[:len(start)+j]
}

func countOpCompile14(src string) string {
	op := strings.Index(src, "e.op = kTableFixedCount")
	if op < 0 {
		return ""
	}
	head := strings.LastIndex(src[:op], "case 14:")
	if head < 0 {
		return ""
	}
	tail := strings.Index(src[op:], "case 16:")
	if tail < 0 {
		return ""
	}
	return src[head : op+tail]
}

func countOpLayouts(t *testing.T, unit func(*testing.T, string) *ir.Unit) (local, peer []byte) {
	t.Helper()
	le := ir.TableFixedWalkRoot(unit(t, countOpLocalSchema).Tables["Root"])
	he := ir.TableFixedWalkRoot(unit(t, countOpPeerSchema).Tables["Root"])
	if len(le) != 4 || len(he) != 4 {
		t.Fatalf("entries local %d peer %d, want 4", len(le), len(he))
	}
	if le[0].Size != 13 || he[0].Size != 9 {
		t.Fatalf("root size local %d peer %d, want 13 and 9", le[0].Size, he[0].Size)
	}
	if le[1].Kind != ir.TableKindU64 || le[1].Size != 8 || he[1].Size != 8 || le[1].ID != he[1].ID {
		t.Fatalf("lead local kind %d size %d id %d; peer size %d id %d", le[1].Kind, le[1].Size, le[1].ID, he[1].Size, he[1].ID)
	}
	if le[2].Kind != ir.TableKindArray || he[2].Kind != ir.TableKindArray || le[2].ID != he[2].ID {
		t.Fatalf("tail array local kind %d id %d peer kind %d id %d", le[2].Kind, le[2].ID, he[2].Kind, he[2].ID)
	}
	if le[2].Size != 5 || le[2].Dst.Counted != 1 {
		t.Fatalf("local tail size %d counted %d, want 5 and 1", le[2].Size, le[2].Dst.Counted)
	}
	if he[2].Size != 1 || he[2].Dst.Counted != 0 {
		t.Fatalf("peer tail size %d counted %d, want 1 and 0", he[2].Size, he[2].Dst.Counted)
	}
	if le[3].Kind != ir.TableKindU8 || le[3].Size != 1 || he[3].Kind != ir.TableKindU8 || he[3].Size != 1 {
		t.Fatalf("element local kind %d size %d peer kind %d size %d", le[3].Kind, le[3].Size, he[3].Kind, he[3].Size)
	}
	return ir.TableFixedLayoutBytes(le), ir.TableFixedLayoutBytes(he)
}

func countOpCBytes(b []byte) string {
	var sb strings.Builder
	for i, v := range b {
		if i > 0 {
			sb.WriteByte(',')
		}
		fmt.Fprintf(&sb, "%d", v)
	}
	return sb.String()
}

func TestFixedCountOpReachCoversItsHeader(t *testing.T) {
	t.Parallel()
	push := countOpBetween(tableFixedRuntime, "void table_fixed_push(", "uint32_t table_fixed_lay_table(")
	if !strings.Contains(push, countOpSpan) {
		t.Fatalf("table_fixed_push reach-checks kTableFixedCount at src+size; the count op reads 4 bytes; missing %q", countOpSpan)
	}
	if !strings.Contains(countOpCompile14(tableFixedRuntime), countOpRefuseC) {
		t.Fatalf("kind-14 compile does not refuse a counted local field against a peer te.size < 4: missing %q", countOpRefuseC)
	}
	local, peer := countOpLayouts(t, unitFrom)
	t.Run("asan", func(t *testing.T) {
		t.Parallel()
		slowtest.Gate(t, "the C compiler (cc) with AddressSanitizer")
		countOpCProbe(t, local, peer)
	})
}

func countOpCProbe(t *testing.T, local, peer []byte) {
	t.Helper()
	cc := os.Getenv("CC")
	if cc == "" {
		cc = "cc"
	}
	if _, err := exec.LookPath(cc); err != nil {
		t.Fatalf("the C compiler %q is absent: %v", cc, err)
	}
	u := unitFrom(t, countOpLocalSchema)
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
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	probe := strings.NewReplacer(
		"PEER_BYTES", countOpCBytes(peer),
		"LOCAL_BYTES", countOpCBytes(local),
	).Replace(countOpCProbeSrc)
	if err := os.WriteFile(filepath.Join(dir, "main.c"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	args := []string{"-std=c99", "-Wall", "-Wextra", "-Werror", "-Wshadow", "-O1"}
	args = append(args, gccFortifySilence(cc)...)
	args = append(args,
		"-fsanitize=address", "-fno-sanitize-recover=all", "-fno-omit-frame-pointer", "-g",
		"-I", dir, filepath.Join(dir, "main.c"),
	)
	for name := range files {
		if strings.HasSuffix(name, ".c") && !strings.Contains(name, "/") {
			args = append(args, filepath.Join(dir, name))
		}
	}
	bin := filepath.Join(dir, "probe")
	args = append(args, "-lm", "-o", bin)
	if out, err := exec.Command(cc, args...).CombinedOutput(); err != nil {
		t.Fatalf("compile: %v\n%s", err, out)
	}
	ctx, cancel := slowtest.ProbeContext(t)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin)
	cmd.Env = append(os.Environ(), "ASAN_OPTIONS=halt_on_error=1:detect_leaks=0:abort_on_error=1")
	out, err := cmd.CombinedOutput()
	text := string(out)
	if strings.Contains(text, "AddressSanitizer") || strings.Contains(text, "runtime error") {
		t.Fatalf("AddressSanitizer reported on the count op:\n%s", text)
	}
	if err != nil {
		t.Fatalf("probe: %v\n%s", err, text)
	}
	if !strings.Contains(text, "refused layout_record_too_large") || !strings.Contains(text, "well-formed ok") {
		t.Fatalf("probe:\n%s", text)
	}
}

const countOpCProbeSrc = `#include <stdio.h>
#include <string.h>
#include <stdlib.h>
#include "ProbeTable.h"

static const uint8_t hostile_layout[] = { PEER_BYTES };
static const uint8_t well_layout[] = { LOCAL_BYTES };

static int run_layout(const uint8_t *bytes, int n, int hostile) {
    TableFixedLayoutView parsed;
    TableReport r;
    TableFixedEntry plan[256];
    int why = SCHEMA_TABLE_LAYOUT_MALFORMED;
    int32_t guarded = 0;
    int32_t fill_count = 0;
    int32_t made;
    uint32_t fill_at = 0;
    uint32_t rec;
    memset(&r, 0, sizeof r);
    if (!table_fixed_parse_layout(bytes, n, &parsed, &why)) {
        printf("parse refused reason %d\n", why);
        return 1;
    }
    made = table_fixed_compile(&parsed, root_fixed_layout, (int32_t) root_fixed_layout_bytes,
        root_fixed_dst, root_fixed_cover, root_fixed_cover_count,
        plan, 256, &guarded, &fill_at, &fill_count, &r);
    rec = table_fixed_entry_at(&parsed, 0).size;
    if (hostile) {
        if (rec != 9u) { printf("hostile record %u\n", rec); return 1; }
        if (made >= 0) {
            uint8_t *buf = (uint8_t *)malloc(rec);
            Root back;
            if (buf == NULL) { printf("malloc\n"); return 1; }
            memset(buf, 0, rec);
            root_reset(&back);
            memset(&r, 0, sizeof r);
            table_fixed_run(plan, made, guarded, buf, (uint8_t *)(void *)&back, &r, NULL);
            printf("hostile compiled made=%d count=%d tail=%u\n", made, (int)back.tail_count, (unsigned)back.tail[0]);
            free(buf);
            return 1;
        }
        if (made != -2 || r.reason != SCHEMA_TABLE_LAYOUT_RECORD_TOO_LARGE) {
            printf("hostile made=%d reason=%d\n", made, r.reason);
            return 1;
        }
        printf("refused layout_record_too_large\n");
        return 0;
    }
    if (rec != 13u) { printf("well record %u\n", rec); return 1; }
    if (made < 0) { printf("well-formed made=%d reason=%d\n", made, r.reason); return 1; }
    {
        uint8_t *buf = (uint8_t *)malloc(rec);
        Root back;
        if (buf == NULL) { printf("malloc\n"); return 1; }
        memset(buf, 0, rec);
        buf[8] = 1;
        buf[12] = 0x5a;
        root_reset(&back);
        memset(&r, 0, sizeof r);
        table_fixed_run(plan, made, guarded, buf, (uint8_t *)(void *)&back, &r, NULL);
        free(buf);
        if (back.lead != 0 || back.tail_count != 1 || back.tail[0] != 0x5a) {
            printf("well-formed landed lead=%llu count=%d tail=%u\n",
                (unsigned long long)back.lead, (int)back.tail_count, (unsigned)back.tail[0]);
            return 1;
        }
    }
    printf("well-formed ok\n");
    return 0;
}

int main(void) {
    if (run_layout(hostile_layout, (int)sizeof hostile_layout, 1)) return 1;
    if (run_layout(well_layout, (int)sizeof well_layout, 0)) return 1;
    return 0;
}
`
