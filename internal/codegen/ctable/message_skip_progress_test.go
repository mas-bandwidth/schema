package ctable

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

const skipProgressSchema = `package probe

fixed table Root {
	a int32
}
`

type skipProgressCase struct {
	name     string
	wire     []byte
	wantLoad bool
	unknown  int
}

func skipProgressPut(bits *[]uint8, value uint64, width int) {
	for b := range width {
		*bits = append(*bits, uint8((value>>b)&1))
	}
}

func skipProgressMessage(t *testing.T, bodies ...[]uint8) []byte {
	t.Helper()
	if len(bodies) == 0 || len(bodies) > 256 {
		t.Fatalf("body count %d outside 1..256", len(bodies))
	}
	var bits []uint8
	for _, body := range bodies {
		bits = append(bits, body...)
	}
	out := make([]byte, 2+(len(bits)+7)/8)
	out[0] = ir.TableWireMessageForm
	out[1] = uint8(len(bodies) - 1)
	for i, bit := range bits {
		if bit != 0 {
			out[2+(i>>3)] |= uint8(1 << (i & 7))
		}
	}
	return out
}

func skipProgressFixtures(t *testing.T) (*ir.Unit, []byte, int) {
	t.Helper()
	u := unitFrom(t, skipProgressSchema)
	zeroBit := ir.TableMessageShape{Min: 0, Max: 0, Elem: ir.TableKindBool, Inner: &ir.TableMessageShape{}}
	boolElement := ir.TableMessageShape{}
	entries := []ir.TableVocabularyEntry{
		{
			Id:   0x9991,
			Kind: ir.TableKindArray,
			Shape: ir.TableMessageShape{
				Min: 0, Max: ir.TableMessageListMax,
				Elem: ir.TableKindArray, Inner: &zeroBit,
			},
		},
		{
			Id:   0x9992,
			Kind: ir.TableKindArray,
			Shape: ir.TableMessageShape{
				Min: 4, Max: 4,
				Elem: ir.TableKindArray, Inner: &zeroBit,
			},
		},
		{
			Id:   0x9993,
			Kind: ir.TableKindArray,
			Shape: ir.TableMessageShape{
				Min: 0, Max: 255,
				Elem: ir.TableKindBool, Inner: &boolElement,
			},
		},
		{Id: 0x9994},
		{
			Id:   0x9995,
			Kind: ir.TableKindKeyed,
			Shape: ir.TableMessageShape{
				Max:  1,
				Elem: ir.TableKindArray, Inner: &zeroBit,
			},
		},
	}
	ann := depthBoundaryEncodeAnnouncement(0x0102030405060708, ir.TableVocabularyBytes(entries))
	return u, ann, ir.TableMessageRefBits(len(entries))
}

func skipProgressCases(t *testing.T, refBits int) []skipProgressCase {
	t.Helper()
	ref := func(bits *[]uint8, slot uint64) { skipProgressPut(bits, slot, refBits) }
	var cases []skipProgressCase
	for _, count := range []uint64{1, 3, 1000} {
		var body []uint8
		ref(&body, 1)
		skipProgressPut(&body, count, 32)
		ref(&body, 0)
		cases = append(cases, skipProgressCase{
			name: fmt.Sprintf("unbounded_count_%d", count),
			wire: skipProgressMessage(t, body),
		})
	}
	var fixedZero []uint8
	ref(&fixedZero, 2)
	ref(&fixedZero, 0)
	cases = append(cases, skipProgressCase{
		name: "fixed_count_zero_bit_element",
		wire: skipProgressMessage(t, fixedZero),
	})
	var fixed []uint8
	for range 3 {
		ref(&fixed, 2)
	}
	ref(&fixed, 0)
	cases = append(cases, skipProgressCase{
		name: "fixed_count_referenced_three_times",
		wire: skipProgressMessage(t, fixed),
	})
	var keyed []uint8
	ref(&keyed, 5)
	skipProgressPut(&keyed, 1, 1)
	ref(&keyed, 4)
	ref(&keyed, 0)
	cases = append(cases, skipProgressCase{
		name: "keyed_zero_bit_element",
		wire: skipProgressMessage(t, keyed),
	})
	var empty []uint8
	ref(&empty, 0)
	cases = append(cases, skipProgressCase{
		name:     "no_unnamed_entry",
		wire:     skipProgressMessage(t, empty),
		wantLoad: true,
		unknown:  0,
	})
	var boolZero []uint8
	ref(&boolZero, 3)
	skipProgressPut(&boolZero, 0, 8)
	ref(&boolZero, 0)
	cases = append(cases, skipProgressCase{
		name:     "bool_count_zero",
		wire:     skipProgressMessage(t, boolZero),
		wantLoad: true,
		unknown:  1,
	})
	return cases
}

func runCSkipProgressProbe(t *testing.T, u *ir.Unit, ann []byte, cases []skipProgressCase) (string, error) {
	t.Helper()
	cc := os.Getenv("CC")
	if cc == "" {
		cc = "cc"
	}
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
	var cfiles []string
	for name, data := range files {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, data, 0o600); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(name, ".c") {
			cfiles = append(cfiles, p)
		}
	}
	var src strings.Builder
	src.WriteString(`#include "ProbeTable.h"
#include <stdio.h>
#include <stdint.h>
#include <string.h>

static int failed = 0;

static void check_case(const char * name, const TableVocabulary * vocab, const uint8_t * wire, int64_t len, int want_ok, int want_unknown)
{
    Root value;
    root_reset(&value);
    value.a = -7;
    int64_t count = 1;
    TableReport rep;
    memset(&rep, 0, sizeof(rep));
    int ok = root_load_messages(&value, &count, vocab, wire, len, &rep);
    if (want_ok) {
        if (ok && count == 1 && !rep.refused && rep.refuse_reason == SCHEMA_TABLE_REFUSE_OK && rep.unknown == want_unknown) {
            printf("PASS %s\n", name);
        } else {
            printf("FAIL %s: ok=%d count=%lld refused=%d refuse_reason=%d unknown=%d\n",
                name, ok, (long long)count, rep.refused, (int)rep.refuse_reason, (int)rep.unknown);
            failed = 1;
        }
    } else {
        if (!ok && count == 0 && rep.refused) {
            printf("PASS %s\n", name);
        } else {
            printf("FAIL %s: ok=%d count=%lld refused=%d refuse_reason=%d unknown=%d\n",
                name, ok, (long long)count, rep.refused, (int)rep.refuse_reason, (int)rep.unknown);
            failed = 1;
        }
    }
}

int main(void)
{
`)
	fmt.Fprintf(&src, "    static const uint8_t ann[] = { %s };\n", depthBoundaryHexList(ann, ""))
	src.WriteString(`    TableMessageEntry entries[64];
    TableVocabulary vocab = table_vocabulary(entries, 64);
    TableReport arep;
    memset(&arep, 0, sizeof(arep));
    if (!announce_read(&vocab, ann, (int64_t)sizeof(ann), &arep) || !vocab.announced) {
        printf("announce_read failed\n");
        return 2;
    }
`)
	for i, tc := range cases {
		want := 0
		if tc.wantLoad {
			want = 1
		}
		fmt.Fprintf(&src, "    static const uint8_t w%d[] = { %s };\n", i, depthBoundaryHexList(tc.wire, ""))
		fmt.Fprintf(&src, "    check_case(%q, &vocab, w%d, (int64_t)sizeof(w%d), %d, %d);\n", tc.name, i, i, want, tc.unknown)
	}
	src.WriteString(`    return failed;
}
`)
	mainPath := filepath.Join(dir, "main.c")
	if err := os.WriteFile(mainPath, []byte(src.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "skipprogress")
	args := []string{"-std=c99", "-Wall", "-Wextra", "-Werror", "-O2", "-fsanitize=undefined", "-fno-sanitize-recover=all", "-fno-omit-frame-pointer", "-I", dir, mainPath}
	args = append(args, cfiles...)
	args = append(args, "-lm", "-o", bin)
	if out, err := exec.Command(cc, args...).CombinedOutput(); err != nil {
		t.Fatalf("compile C failed: %v\n%s", err, out)
	}
	out, err := exec.Command(bin).CombinedOutput()
	return string(out), err
}

func TestCMessageSkipElementConsumesBits(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the c compiler (it compiles and runs the generated message skip probe)")
	u, ann, refBits := skipProgressFixtures(t)
	cases := skipProgressCases(t, refBits)
	out, err := runCSkipProgressProbe(t, u, ann, cases)
	if err != nil {
		t.Fatalf("the message skip probe failed: %v\n%s", err, out)
	}
	for _, tc := range cases {
		if !strings.Contains(out, "PASS "+tc.name) {
			t.Fatalf("missing PASS for %s in:\n%s", tc.name, out)
		}
	}
}
