package ctable

import (
	"context"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	cgen "github.com/mas-bandwidth/schema/v2/internal/codegen/c"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// C twin of gotable/matched_width_test.go. Regenerated goldens cannot hold
// has(N)+getN against a later emitter edit on their own.

const matchedWidthSchema = `package probe
table Root {
 a uint16
 b int32
}
`

func unitFrom(t *testing.T, src string) *ir.Unit {
	t.Helper()
	f, perrs := parser.Parse("Probe.schema", []byte(src))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "Probe.schema", Name: "Probe.schema", Base: "Probe",
		Bytes: []byte(src), AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	return u
}

func generate(t *testing.T, src string) map[string][]byte {
	t.Helper()
	out, err := Generate(unitFrom(t, src))
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	return out
}

func TestMatchedScalarWidthShape(t *testing.T) {
	files := generate(t, matchedWidthSchema)
	body := ""
	for name, data := range files {
		if strings.HasSuffix(name, "Table.h") {
			body += string(data)
		}
	}
	load := funcDef(body, "root_load_body")
	if load == "" {
		t.Fatal("root_load_body was not emitted")
	}
	for _, want := range []string{
		"table_reader_get16",
		"table_reader_get32",
		"table_reader_has( &(*r), 2 )",
		"table_reader_has( &(*r), 4 )",
		"table_kind_widens( kind, 7 )",
		"table_kind_widens( kind, 4 )",
		"(uint8_t) table_reader_get8( &(*r) )",
		"(int8_t) table_reader_get8( &(*r) )",
	} {
		if !strings.Contains(load, want) {
			t.Fatalf("matched-width load_body is missing %q", want)
		}
	}
	rest := stripWidenArms(load)
	if strings.Contains(rest, "(uint8_t) table_reader_get8( &(*r) )") {
		t.Fatal("unsigned source-width get8 escaped the widen arm")
	}
	if strings.Contains(rest, "(int8_t) table_reader_get8( &(*r) )") {
		t.Fatal("signed source-width get8 escaped the widen arm")
	}
	if strings.Contains(rest, "table_kind_bytes") {
		t.Fatal("matched arm still sizes the payload through table_kind_bytes")
	}
	if strings.Contains(rest, "switch ( kind )") {
		t.Fatal("matched arm still switches on the runtime kind")
	}
}

func TestMatchedWidthRoundTrip(t *testing.T) {
	runCGenerated(t, matchedWidthSchema, `#include "ProbeTable.h"
#include <stdio.h>
#define CHECK(x) do { if (!(x)) { fprintf(stderr, "line %d: %s\n", __LINE__, #x); return 1; } } while (0)
int main(void)
{
    Root value, loaded;
    TableReport report;
    int64_t n;
    uint8_t wire[256];
    root_reset(&value);
    value.a = 0x1234;
    value.b = -400;
    n = root_measure(&value);
    CHECK(n > 0 && n <= (int64_t)sizeof(wire));
    CHECK(root_save(&value, wire, n) == n);
    memset(&report, 0, sizeof(report));
    CHECK(root_load(&loaded, wire, n, &report));
    CHECK(!report.malformed && !report.refused && !report.unknown && !report.kind_mismatch && !report.widened && !report.clamped);
    CHECK(loaded.a == 0x1234 && loaded.b == -400);
    return 0;
}
`)
}

func TestWidenArmReadsWireWidth(t *testing.T) {
	u := unitFrom(t, matchedWidthSchema)
	a := ir.TableFieldWireId(u.Tables["Root"].Fields[0])
	b := ir.TableFieldWireId(u.Tables["Root"].Fields[1])
	runCGenerated(t, matchedWidthSchema, fmt.Sprintf(`#include "ProbeTable.h"
#include <stdio.h>
#define CHECK(x) do { if (!(x)) { fprintf(stderr, "line %%d: %%s\n", __LINE__, #x); return 1; } } while (0)
int main(void)
{
    TableWriteIds ids;
    uint8_t buf[256];
    TableWriter w = table_writer_make(buf, (int64_t)sizeof(buf), &ids);
    Root loaded;
    TableReport report;
    table_writer_put8(&w, 1);
    table_writer_id(&w, 0x%016xull);
    table_writer_put8(&w, 6);
    table_writer_put8(&w, 9);
    table_writer_id(&w, 0x%016xull);
    table_writer_put8(&w, 2);
    table_writer_put8(&w, 7);
    table_writer_put8(&w, 0);
    table_writer_finish(&w);
    CHECK(!w.overflow);
    memset(&report, 0, sizeof(report));
    CHECK(root_load(&loaded, buf, w.offset, &report));
    CHECK(report.widened == 2 && report.kind_mismatch == 0 && !report.malformed && !report.refused);
    CHECK(loaded.a == 9 && loaded.b == 7);
    return 0;
}
`, a, b))
}

func TestMatchedWidthMismatchSkips(t *testing.T) {
	u := unitFrom(t, matchedWidthSchema)
	b := ir.TableFieldWireId(u.Tables["Root"].Fields[1])
	runCGenerated(t, matchedWidthSchema, fmt.Sprintf(`#include "ProbeTable.h"
#include <stdio.h>
#define CHECK(x) do { if (!(x)) { fprintf(stderr, "line %%d: %%s\n", __LINE__, #x); return 1; } } while (0)
int main(void)
{
    TableWriteIds ids;
    uint8_t buf[256];
    TableWriter w = table_writer_make(buf, (int64_t)sizeof(buf), &ids);
    Root loaded;
    TableReport report;
    table_writer_put8(&w, 1);
    table_writer_id(&w, 0x%016xull);
    table_writer_put8(&w, 1);
    table_writer_put8(&w, 1);
    table_writer_put8(&w, 0);
    table_writer_finish(&w);
    CHECK(!w.overflow);
    memset(&report, 0, sizeof(report));
    CHECK(root_load(&loaded, buf, w.offset, &report));
    CHECK(report.kind_mismatch == 1 && report.widened == 0 && !report.malformed && !report.refused);
    CHECK(loaded.b == 0);
    return 0;
}
`, b))
}

// gccFortifyFlags silence gcc -O2 -Werror when copy_run's overlapping unroll
// inlines into fill_run against a stack object smaller than that unroll.
// Clang rejects -Wno-maybe-uninitialized under -Werror, so they are GNU-only.
var gccFortifyFlags = []string{
	"-Wno-array-bounds",
	"-Wno-maybe-uninitialized",
	"-Wno-stringop-overread",
}

// gccFortifySilence returns gccFortifyFlags for a GNU cc, and nil for clang.
// Ubuntu's `cc --version` is "cc (Ubuntu …)" and never contains the word gcc;
// 2c49a99a required that word and CI dropped the flags. GNU is __GNUC__
// without __clang__, with --version as a fallback that does not require "gcc".
func gccFortifySilence(cc string) []string {
	if gnuCC(cc) {
		return gccFortifyFlags
	}
	return nil
}

func gnuCC(cc string) bool {
	cmd := exec.Command(cc, "-dM", "-E", "-")
	cmd.Stdin = strings.NewReader("\n")
	out, err := cmd.CombinedOutput()
	if err == nil {
		defs := string(out)
		return strings.Contains(defs, "#define __GNUC__") && !strings.Contains(defs, "#define __clang__")
	}
	vout, verr := exec.Command(cc, "--version").CombinedOutput()
	if verr != nil {
		return false
	}
	s := strings.ToLower(string(vout))
	return !strings.Contains(s, "clang") && !strings.Contains(s, "apple llvm")
}

func TestGccFortifySilenceFollowsGNUNotTheWordGcc(t *testing.T) {
	cc := os.Getenv("CC")
	if cc == "" {
		cc = "cc"
	}
	flags := gccFortifySilence(cc)
	if gnuCC(cc) {
		if flags == nil {
			t.Fatal("GNU cc dropped fortify silence flags; Ubuntu's cc --version has no word gcc")
		}
		return
	}
	if flags != nil {
		t.Fatalf("non-GNU cc must not get fortify silence flags, got %v", flags)
	}
}

func runCGenerated(t *testing.T, schema, source string, extraCC ...string) {
	t.Helper()
	cc := os.Getenv("CC")
	if cc == "" {
		var err error
		cc, err = exec.LookPath("cc")
		if err != nil {
			t.Skip("generated C execution requires cc")
		}
	}
	u := unitFrom(t, schema)
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
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "main.c"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	args := []string{"-std=c99", "-Wall", "-Wextra", "-Werror", "-Wshadow", "-O2"}
	args = append(args, extraCC...)
	args = append(args, "-I", dir, filepath.Join(dir, "main.c"))
	for name := range files {
		if strings.HasSuffix(name, ".c") {
			args = append(args, filepath.Join(dir, name))
		}
	}
	args = append(args, "-lm", "-o", filepath.Join(dir, "probe"))
	if output, err := exec.Command(cc, args...).CombinedOutput(); err != nil {
		t.Fatalf("compile: %v\n%s", err, output)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if output, err := exec.CommandContext(ctx, filepath.Join(dir, "probe")).CombinedOutput(); err != nil {
		t.Fatalf("execute: %v\n%s", err, output)
	}
}

func funcDef(src, name string) string {
	sig := name + "( TableReader * r, Root * value )"
	from := 0
	for {
		i := strings.Index(src[from:], sig)
		if i < 0 {
			return ""
		}
		i += from
		j := i + len(sig)
		for j < len(src) && (src[j] == ' ' || src[j] == '\n') {
			j++
		}
		if j < len(src) && src[j] == '{' {
			depth := 0
			for k := j; k < len(src); k++ {
				switch src[k] {
				case '{':
					depth++
				case '}':
					depth--
					if depth == 0 {
						return src[i : k+1]
					}
				}
			}
			return src[i:]
		}
		from = i + len(sig)
	}
}

func stripWidenArms(src string) string {
	const needle = "table_kind_widens("
	var b strings.Builder
	for {
		i := strings.Index(src, needle)
		if i < 0 {
			b.WriteString(src)
			return b.String()
		}
		b.WriteString(src[:i])
		j := strings.Index(src[i:], "{")
		if j < 0 {
			return b.String()
		}
		j += i
		depth := 0
		closed := false
		for k := j; k < len(src); k++ {
			switch src[k] {
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					src = src[k+1:]
					closed = true
				}
			}
			if closed {
				break
			}
		}
		if !closed {
			return b.String()
		}
	}
}
