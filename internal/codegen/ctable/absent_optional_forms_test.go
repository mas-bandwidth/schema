package ctable

// THE ABSENT OPTIONAL READS THE SAME IN EVERY LEG AND IN BOTH FORMS
// (docs/SPEC-TABLES.md §2.3, §3.4, §4; docs/rulings.json
// fixed-absent-payload-refuses-non-zero).
//
// One value with two absent optionals, each over a type whose field has a
// non-zero declared default, is written by the engine (internal/tablewire) as
// the fixed form and, from the same schema with the root declared a plain
// table, as the variable form. Four readers read both: the C leg, the C++
// leg, generated Go and the engine's own decode. Every one must land the
// optional absent and its payload at the DECLARED DEFAULT, never at the wire's
// zeros, so the two forms of one value cannot drift apart again.
//
// The fixed file is then stained: a non-zero byte under an absent optional's
// 0 flag, in a value, in a text's slack inside the payload, and in a nested
// optional's payload. Every reader must refuse each one. A non-zero byte in
// ordinary slack (a string's bytes past its length) is the control: it is
// still not read and the file still loads.

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	cgen "github.com/mas-bandwidth/schema/v2/internal/codegen/c"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpp"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpptable"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/gotable"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const absentFormsSchema = `package absentforms

type Leaf
{
    value int32 = 30
    tag   string(8)
}

fixed table Holder
{
    inner ?Leaf
    n     int32 = 5
}

fixed table Root
{
    name string(8)
    link ?Leaf
    hold Holder
}
`

// The fixed record body, from §3.4's table: name is string(8) (a 4-byte
// length and 8 bytes), link is ?Leaf (the flag, then Leaf: value 4, tag 4 + 8),
// hold is the nested table Holder (inner ?Leaf the same 17 bytes, then n 4).
const (
	absentFormsLinkFlag   = 12
	absentFormsLinkValue  = 13
	absentFormsLinkTag    = 21 // the tag's bytes; the length is 0, so all eight are slack inside the payload
	absentFormsInnerFlag  = 29
	absentFormsInnerValue = 30
	absentFormsNameSlack  = 5 // name holds "x": its bytes past the first are ordinary slack
	absentFormsBodyBytes  = 50
)

// absentFormsCase is one file and the verdict every reader must give it.
type absentFormsCase struct {
	name    string
	wire    []byte
	refused bool
}

func absentFormsUnit(t *testing.T, src string) *ir.Unit {
	t.Helper()
	ast, perrs := parser.Parse("AbsentForms.schema", []byte(src))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs)
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "AbsentForms.schema", Name: "AbsentForms.schema", Base: "AbsentForms", Bytes: []byte(src), AST: ast,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs)
	}
	return u
}

// absentFormsWrite is the engine's bytes for the value, both optionals
// absent, in the form the unit's root declares.
func absentFormsWrite(t *testing.T, u *ir.Unit) []byte {
	t.Helper()
	m := tabletext.NewModel(u)
	inst := m.New(m.Lookup("Root"))
	var r tabletext.Report
	if !m.Read(inst, []byte(`{"name": "x"}`), &r) || !r.Silent() {
		t.Fatalf("place the value: %+v", r)
	}
	b, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// absentFormsEngineRead is the engine's own decode of one file, in the line
// every reader prints.
func absentFormsEngineRead(t *testing.T, u *ir.Unit, wire []byte) string {
	t.Helper()
	m := tabletext.NewModel(u)
	inst := m.New(m.Lookup("Root"))
	var r tabletext.Report
	ok, err := tablewire.Decode(m, inst, wire, &r)
	if err != nil || !ok || r.Refused {
		return "refused"
	}
	link := inst.Fields[1]
	hold := inst.Fields[2].Cell.Tab
	inner := hold.Fields[0]
	value := func(f tabletext.Field) int64 {
		if f.Cell.Tab == nil {
			return -1
		}
		return f.Cell.Tab.Fields[0].Cell.I
	}
	return absentFormsLine(link.Present, value(link), inner.Present, value(inner))
}

func absentFormsLine(linkPresent bool, linkValue int64, innerPresent bool, innerValue int64) string {
	return fmt.Sprintf("ok link_present=%v link.value=%d inner_present=%v inner.value=%d", linkPresent, linkValue, innerPresent, innerValue)
}

// absentFormsCases are the fixed file, its stains and its control, then the
// variable file.
func absentFormsCases(t *testing.T, fixed, variable []byte) []absentFormsCase {
	t.Helper()
	layout := int(binary.LittleEndian.Uint32(fixed[16:20]))
	body := 16 + 4 + layout + 8
	if len(fixed) != body+absentFormsBodyBytes {
		t.Fatalf("the fixed file is %d bytes; the body offsets above assume %d", len(fixed), body+absentFormsBodyBytes)
	}
	for _, at := range []int{absentFormsLinkFlag, absentFormsLinkValue, absentFormsLinkTag + 3, absentFormsInnerFlag, absentFormsInnerValue, absentFormsNameSlack} {
		if fixed[body+at] != 0 {
			t.Fatalf("body byte %d is %d before staining; the offsets above are wrong", at, fixed[body+at])
		}
	}
	stain := func(at int) []byte {
		b := bytes.Clone(fixed)
		b[body+at] = 0x5A
		return b
	}
	return []absentFormsCase{
		{name: "fixed_clean", wire: fixed},
		{name: "fixed_value_under_absent_flag", wire: stain(absentFormsLinkValue), refused: true},
		{name: "fixed_text_slack_under_absent_flag", wire: stain(absentFormsLinkTag + 3), refused: true},
		{name: "fixed_nested_value_under_absent_flag", wire: stain(absentFormsInnerValue), refused: true},
		{name: "fixed_ordinary_slack_is_not_read", wire: stain(absentFormsNameSlack)},
		{name: "variable_clean", wire: variable},
	}
}

func absentFormsHex(b []byte) string {
	parts := make([]string, len(b))
	for i, c := range b {
		parts[i] = fmt.Sprintf("0x%02x", c)
	}
	return strings.Join(parts, ", ")
}

// absentFormsWant is every reader's expected line for a case.
func absentFormsWant(c absentFormsCase) string {
	if c.refused {
		return "refused"
	}
	return absentFormsLine(false, 30, false, 30)
}

// absentFormsParse reads a reader's "case <name> <line>" output.
func absentFormsParse(out string) map[string]string {
	got := map[string]string{}
	for line := range strings.SplitSeq(out, "\n") {
		rest, ok := strings.CutPrefix(strings.TrimSpace(line), "case ")
		if !ok {
			continue
		}
		name, verdict, _ := strings.Cut(rest, " ")
		got[name] = verdict
	}
	return got
}

func absentFormsWriteFiles(t *testing.T, files map[string][]byte) (string, []string) {
	t.Helper()
	dir := t.TempDir()
	var sources []string
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, data, 0o600); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(name, ".c") || strings.HasSuffix(name, ".cpp") {
			sources = append(sources, p)
		}
	}
	sort.Strings(sources)
	return dir, sources
}

// absentFormsC builds and runs the C reader over every case of one form.
func absentFormsC(t *testing.T, u *ir.Unit, fixedForm bool, cases []absentFormsCase) string {
	t.Helper()
	files, err := cgen.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	dir, sources := absentFormsWriteFiles(t, files)
	var src strings.Builder
	src.WriteString(`#include "AbsentFormsTable.h"
#include <stdio.h>
#include <string.h>
#include <stdint.h>

static uint8_t region[1 << 16];

static void line( const char * name, const Root * v )
{
    printf( "case %s ok link_present=%s link.value=%d inner_present=%s inner.value=%d\n", name,
            v->link_present ? "true" : "false", (int) v->link.value,
            v->hold.inner_present ? "true" : "false", (int) v->hold.inner.value );
}

int main( void )
{
    (void) region;
`)
	for i, c := range cases {
		fmt.Fprintf(&src, "    {\n        static const uint8_t w[] = { %s };\n", absentFormsHex(c.wire))
		if fixedForm {
			src.WriteString("        static TableFixedEntry plan[256];\n        Root v;\n        TableReport r;\n")
			src.WriteString("        memset( &v, 0xA5, sizeof( v ) );\n")
			src.WriteString("        const int64_t n = root_fixed_load( &v, 1, w, (int64_t) sizeof( w ), plan, 256, NULL, &r );\n")
			fmt.Fprintf(&src, "        if ( n != 1 ) { printf( \"case %s refused\\n\" ); } else { line( %q, &v ); }\n", c.name, c.name)
		} else {
			src.WriteString("        TableReport r;\n        memset( region, 0xA5, sizeof( region ) );\n")
			src.WriteString("        const Root * v = root_load( region, (int64_t) sizeof( region ), w, (int64_t) sizeof( w ), &r );\n")
			fmt.Fprintf(&src, "        if ( v == NULL ) { printf( \"case %s refused\\n\" ); } else { line( %q, v ); }\n", c.name, c.name)
		}
		src.WriteString("    }\n")
		_ = i
	}
	src.WriteString("    return 0;\n}\n")
	mainPath := filepath.Join(dir, "main.c")
	if err := os.WriteFile(mainPath, []byte(src.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	cc := os.Getenv("CC")
	if cc == "" {
		cc = "cc"
	}
	bin := filepath.Join(dir, "probe")
	args := append([]string{"-std=c99", "-Wall", "-Wextra", "-Werror", "-O1", "-I", dir, mainPath}, sources...)
	args = append(args, "-lm", "-o", bin)
	if out, err := exec.Command(cc, args...).CombinedOutput(); err != nil {
		t.Fatalf("compile C: %v\n%s", err, out)
	}
	return absentFormsRun(t, exec.Command(bin))
}

// absentFormsCpp builds and runs the C++ reader over every case of one form.
func absentFormsCpp(t *testing.T, u *ir.Unit, fixedForm bool, cases []absentFormsCase) string {
	t.Helper()
	files, err := cpp.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := cpptable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	dir, sources := absentFormsWriteFiles(t, files)
	var src strings.Builder
	src.WriteString(`#include "AbsentFormsTable.h"
#include <cstdio>
#include <cstring>
#include <cstdint>
using namespace absentforms;

static uint8_t region[1 << 16];

static void line( const char * name, const Root & v )
{
    std::printf( "case %s ok link_present=%s link.value=%d inner_present=%s inner.value=%d\n", name,
                 v.link_present ? "true" : "false", (int) v.link.value,
                 v.hold.inner_present ? "true" : "false", (int) v.hold.inner.value );
}

int main( void )
{
    (void) region;
`)
	for _, c := range cases {
		fmt.Fprintf(&src, "    {\n        static const uint8_t w[] = { %s };\n", absentFormsHex(c.wire))
		if fixedForm {
			src.WriteString("        static TableFixedEntry plan[256];\n        Root v;\n        TableReport r;\n")
			src.WriteString("        std::memset( (void *) &v, 0xA5, sizeof( v ) );\n")
			src.WriteString("        const int64_t n = RootFixedLoad( &v, 1, w, (int64_t) sizeof( w ), plan, 256, NULL, &r );\n")
			fmt.Fprintf(&src, "        if ( n != 1 ) { std::printf( \"case %s refused\\n\" ); } else { line( %q, v ); }\n", c.name, c.name)
		} else {
			src.WriteString("        TableReport r;\n        std::memset( region, 0xA5, sizeof( region ) );\n")
			src.WriteString("        const Root * v = RootLoad( region, (int64_t) sizeof( region ), w, (int64_t) sizeof( w ), &r );\n")
			fmt.Fprintf(&src, "        if ( v == NULL ) { std::printf( \"case %s refused\\n\" ); } else { line( %q, *v ); }\n", c.name, c.name)
		}
		src.WriteString("    }\n")
	}
	src.WriteString("    return 0;\n}\n")
	mainPath := filepath.Join(dir, "main.cpp")
	if err := os.WriteFile(mainPath, []byte(src.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	cxx := os.Getenv("CXX")
	if cxx == "" {
		cxx = "c++"
	}
	bin := filepath.Join(dir, "probe")
	args := append([]string{"-std=c++17", "-Wall", "-Wextra", "-Werror", "-O1", "-I", dir, mainPath}, sources...)
	args = append(args, "-o", bin)
	if out, err := exec.Command(cxx, args...).CombinedOutput(); err != nil {
		t.Fatalf("compile C++: %v\n%s", err, out)
	}
	return absentFormsRun(t, exec.Command(bin))
}

// absentFormsGo builds and runs the generated Go reader over every case of one form.
func absentFormsGo(t *testing.T, u *ir.Unit, fixedForm bool, cases []absentFormsCase) string {
	t.Helper()
	files, err := golang.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := gotable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	runtime := slowtest.SerializeGo(t, "../../../")
	files["go.mod"] = []byte(fmt.Sprintf("module absentforms\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go/v2 v2.0.0\nreplace github.com/mas-bandwidth/serialize.go/v2 => %q\n", runtime))
	var src strings.Builder
	src.WriteString(`package absentforms

import (
	"fmt"
	"testing"
)

func absentFormsLine(name string, v *Root) {
	fmt.Printf("case %s ok link_present=%v link.value=%d inner_present=%v inner.value=%d\n", name, v.LinkPresent, v.Link.Value, v.Hold.InnerPresent, v.Hold.Inner.Value)
}

func TestAbsentForms(t *testing.T) {
`)
	for _, c := range cases {
		fmt.Fprintf(&src, "\t{\n\t\tw := []byte{%s}\n", absentFormsHex(c.wire))
		if fixedForm {
			src.WriteString("\t\tplan := make([]TableFixedEntry, 256)\n\t\tvals := make([]Root, 1)\n")
			src.WriteString("\t\tvals[0].Link.Value, vals[0].Hold.Inner.Value = -1, -1\n")
			src.WriteString("\t\tvar r TableReport\n")
			fmt.Fprintf(&src, "\t\tif RootFixedLoad(vals, w, plan, &r) != 1 {\n\t\t\tfmt.Println(\"case %s refused\")\n\t\t} else {\n\t\t\tabsentFormsLine(%q, &vals[0])\n\t\t}\n", c.name, c.name)
		} else {
			src.WriteString("\t\tregion := make([]byte, 1<<16)\n\t\tvar r TableReport\n")
			fmt.Fprintf(&src, "\t\tif v := RootLoad(region, w, &r); v == nil {\n\t\t\tfmt.Println(\"case %s refused\")\n\t\t} else {\n\t\t\tabsentFormsLine(%q, v)\n\t\t}\n", c.name, c.name)
		}
		src.WriteString("\t}\n")
	}
	src.WriteString("}\n")
	files["probe_test.go"] = []byte(src.String())
	dir := t.TempDir()
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-v", "-count=1", ".")
	cmd.Dir = dir
	return absentFormsRun(t, cmd)
}

func absentFormsRun(t *testing.T, cmd *exec.Cmd) string {
	t.Helper()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s: %v\n%s", cmd.Path, err, out)
	}
	return string(out)
}

func TestAbsentOptionalReadsTheSameInEveryLegAndForm(t *testing.T) {
	slowtest.Gate(t, "the C, C++ and Go toolchains (it compiles and runs one reader in each)")
	t.Parallel()
	fixedUnit := absentFormsUnit(t, absentFormsSchema)
	variableUnit := absentFormsUnit(t, strings.Replace(absentFormsSchema, "fixed table Root", "table Root", 1))
	fixed := absentFormsWrite(t, fixedUnit)
	variable := absentFormsWrite(t, variableUnit)
	if fixed[0] != 3 || variable[0] != 1 {
		t.Fatalf("the engine wrote forms %d and %d; want 3 (fixed) and 1 (variable)", fixed[0], variable[0])
	}
	cases := absentFormsCases(t, fixed, variable)
	var fixedCases, variableCases []absentFormsCase
	for _, c := range cases {
		if strings.HasPrefix(c.name, "fixed_") {
			fixedCases = append(fixedCases, c)
		} else {
			variableCases = append(variableCases, c)
		}
	}
	got := map[string]map[string]string{"engine": {}}
	for _, c := range fixedCases {
		got["engine"][c.name] = absentFormsEngineRead(t, fixedUnit, c.wire)
	}
	for _, c := range variableCases {
		got["engine"][c.name] = absentFormsEngineRead(t, variableUnit, c.wire)
	}
	legs := []struct {
		name string
		run  func(*testing.T, *ir.Unit, bool, []absentFormsCase) string
	}{
		{"c", absentFormsC},
		{"cpp", absentFormsCpp},
		{"go", absentFormsGo},
	}
	for _, leg := range legs {
		m := absentFormsParse(leg.run(t, fixedUnit, true, fixedCases))
		maps.Copy(m, absentFormsParse(leg.run(t, variableUnit, false, variableCases)))
		got[leg.name] = m
	}
	var bad []string
	for _, leg := range []string{"c", "cpp", "go", "engine"} {
		for _, c := range cases {
			if want, have := absentFormsWant(c), got[leg][c.name]; have != want {
				bad = append(bad, fmt.Sprintf("%s %s: got %q, want %q", leg, c.name, have, want))
			}
		}
	}
	if len(bad) > 0 {
		t.Fatalf("an absent optional does not read the same in every leg and form:\n%s", strings.Join(bad, "\n"))
	}
}
