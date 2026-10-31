package ctable

import (
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
	"github.com/mas-bandwidth/schema/v2/internal/codegen/cstable"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/dart"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/darttable"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/elixirtable"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/gotable"
	javacodegen "github.com/mas-bandwidth/schema/v2/internal/codegen/java"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/javatable"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/js"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/jstable"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/rusttable"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const depthBoundarySchema = `package depthprobe

fixed table Root {
	a int32
}
`

type depthBoundaryCase struct {
	name     string
	family   string
	depth    int
	wire     []byte
	expectOK bool
}

func depthBoundaryUnit(t *testing.T) *ir.Unit {
	t.Helper()
	ast, perrs := parser.Parse("DepthProbe.schema", []byte(depthBoundarySchema))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs)
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path:  "DepthProbe.schema",
		Name:  "DepthProbe.schema",
		Base:  "DepthProbe",
		Bytes: []byte(depthBoundarySchema),
		AST:   ast,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs)
	}
	return u
}

func depthBoundaryAppendLeb(out []byte, v uint64) []byte {
	for v >= 0x80 {
		out = append(out, uint8(v)|0x80)
		v >>= 7
	}
	return append(out, uint8(v))
}

func depthBoundaryEncodeAnnouncement(buildVersion uint64, vocabBytes []byte) []byte {
	var out []byte
	out = append(out, ir.TableWireForm)
	out = append(out, 1, uint8(ir.TableKindU64))
	out = binary.LittleEndian.AppendUint64(out, buildVersion)
	out = append(out, 2, uint8(ir.TableKindArray))
	var sub []byte
	sub = append(sub, uint8(ir.TableKindU8))
	sub = depthBoundaryAppendLeb(sub, uint64(len(vocabBytes)))
	sub = append(sub, vocabBytes...)
	out = depthBoundaryAppendLeb(out, uint64(len(sub)))
	out = append(out, sub...)
	out = append(out, 0)
	out = binary.LittleEndian.AppendUint64(out, ir.TableBuildVersionWireId)
	out = binary.LittleEndian.AppendUint64(out, ir.TableMessageVocabularyWireId)
	out = binary.LittleEndian.AppendUint64(out, 2)
	return out
}

func depthBoundaryEncodeBatch(t *testing.T, family string, depth, refBits int) []byte {
	t.Helper()
	var bits []uint8
	put := func(val uint64, width int) {
		for b := range width {
			bits = append(bits, uint8((val>>b)&1))
		}
	}
	switch family {
	case "body":
		for range depth {
			put(2, refBits)
		}
		for range depth {
			put(0, refBits)
		}
	case "union":
		for i := 0; i < depth+1; i++ {
			put(3, refBits)
		}
		put(0, refBits)
	case "array":
		for i := 0; i < depth-2; i++ {
			put(2, refBits)
		}
		put(4, refBits)
		for i := 0; i < depth-1; i++ {
			put(0, refBits)
		}
	default:
		t.Fatalf("unknown family %q", family)
	}
	put(1, refBits)
	put(42, 32)
	put(0, refBits)
	put(1, refBits)
	put(99, 32)
	put(0, refBits)

	out := make([]byte, 2+(len(bits)+7)/8)
	out[0] = ir.TableWireMessageForm
	out[1] = 1
	for i, bit := range bits {
		if bit != 0 {
			out[2+(i>>3)] |= uint8(1 << (i & 7))
		}
	}
	return out
}

func depthBoundaryFixtures(t *testing.T) (*ir.Unit, []byte, []depthBoundaryCase) {
	t.Helper()
	u := depthBoundaryUnit(t)
	st := u.Tables["Root"]
	if st == nil || len(st.Fields) != 1 {
		t.Fatalf("expected Root table with 1 field, got %+v", st)
	}
	entries := []ir.TableVocabularyEntry{
		ir.TableFieldEntry(st.Fields[0]),
		{Id: 0x9991, Kind: ir.TableKindTable},
		{Id: 0x9992, Kind: ir.TableKindUnion},
		{
			Id:   0x9993,
			Kind: ir.TableKindArray,
			Shape: ir.TableMessageShape{
				Min:   1,
				Max:   1,
				Elem:  ir.TableKindTable,
				Inner: &ir.TableMessageShape{},
			},
		},
	}
	refBits := ir.TableMessageRefBits(len(entries))
	unitRefBits := ir.TableMessageRefBits(len(ir.TableVocabulary(u)))
	if refBits != 3 || unitRefBits != 3 {
		t.Fatalf("refBits=%d unitRefBits=%d, want 3", refBits, unitRefBits)
	}
	vocabBytes := ir.TableVocabularyBytes(entries)
	ann := depthBoundaryEncodeAnnouncement(0x0102030405060708, vocabBytes)

	families := []string{"body", "union", "array"}
	depths := []int{127, 128, 129}
	var cases []depthBoundaryCase
	byName := map[string][]byte{}
	for _, family := range families {
		for _, d := range depths {
			wire := depthBoundaryEncodeBatch(t, family, d, refBits)
			name := fmt.Sprintf("%s_%d", family, d)
			byName[name] = wire
			cases = append(cases, depthBoundaryCase{
				name:     name,
				family:   family,
				depth:    d,
				wire:     wire,
				expectOK: d <= 128,
			})
			if d == 128 {
				trunc := append([]byte(nil), wire[:len(wire)-12]...)
				cases = append(cases, depthBoundaryCase{
					name:     name + "_truncated",
					family:   family,
					depth:    d,
					wire:     trunc,
					expectOK: false,
				})
			}
		}
	}
	if len(byName["union_129"]) != len(byName["union_128"]) || len(byName["union_129"]) >= len(byName["body_128"]) {
		t.Fatalf("expected len(union_129)=%d == len(union_128)=%d < len(body_128)=%d",
			len(byName["union_129"]), len(byName["union_128"]), len(byName["body_128"]))
	}
	return u, ann, cases
}

func depthBoundaryHexList(b []byte, suffix string) string {
	parts := make([]string, len(b))
	for i, v := range b {
		parts[i] = fmt.Sprintf("0x%02x%s", v, suffix)
	}
	return strings.Join(parts, ", ")
}

func assertDepthBoundaryOutput(t *testing.T, runtimeName string, cases []depthBoundaryCase, out string, err error, shiftedOut string, shiftedErr error) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s message depth boundary failed:\n%s\n%v", runtimeName, out, err)
	}
	for _, tc := range cases {
		if !strings.Contains(out, "PASS "+tc.name) {
			t.Fatalf("%s missing PASS for %s in:\n%s", runtimeName, tc.name, out)
		}
	}
	if shiftedErr == nil {
		t.Fatalf("%s guard shift control (128 -> 256) unexpectedly passed:\n%s", runtimeName, shiftedOut)
	}
	for _, name := range []string{"body_129", "union_129", "array_129"} {
		if !strings.Contains(shiftedOut, "FAIL "+name) {
			t.Fatalf("%s guard shift control (128 -> 256) did not fail %s:\n%s", runtimeName, name, shiftedOut)
		}
	}
}

func runCDepthBoundaryProbe(t *testing.T, u *ir.Unit, ann []byte, cases []depthBoundaryCase, edit func(map[string][]byte)) (string, error) {
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
	if edit != nil {
		edit(files)
	}
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
	src.WriteString(`#include "DepthProbeTable.h"
#include <stdio.h>
#include <stdint.h>
#include <string.h>

static int failed = 0;

static void check_case(const char * name, const TableVocabulary * vocab, const uint8_t * wire, int64_t len, int expect_ok)
{
    Root values[2];
    root_reset(&values[0]);
    root_reset(&values[1]);
    values[0].a = -7;
    values[1].a = -7;
    int64_t count = 2;
    TableReport rep;
    memset(&rep, 0, sizeof(rep));
    int ok = root_load_messages_tolerant(values, &count, vocab, wire, len, &rep);
    if (expect_ok) {
        if (ok && count == 2 && !rep.malformed && !rep.refused && rep.unknown == 1 && values[0].a == 42 && values[1].a == 99) {
            printf("PASS %s\n", name);
        } else {
            printf("FAIL %s: ok=%d count=%lld malformed=%d refused=%d unknown=%d a0=%d a1=%d\n",
                name, ok, (long long)count, rep.malformed, rep.refused, (int)rep.unknown, (int)values[0].a, (int)values[1].a);
            failed = 1;
        }
    } else {
        if (!ok && count == 0 && rep.malformed && !rep.refused) {
            printf("PASS %s\n", name);
        } else {
            printf("FAIL %s: ok=%d count=%lld malformed=%d refused=%d a0=%d a1=%d\n",
                name, ok, (long long)count, rep.malformed, rep.refused, (int)values[0].a, (int)values[1].a);
            failed = 1;
        }
    }
}

int main(void)
{
`)
	fmt.Fprintf(&src, "    static const uint8_t ann[] = { %s };\n", depthBoundaryHexList(ann, ""))
	src.WriteString(`    TableMessageEntry entries[kTableMessageEntriesHere];
    TableVocabulary vocab = table_vocabulary(entries, kTableMessageEntriesHere);
    TableReport arep;
    memset(&arep, 0, sizeof(arep));
    if (!announce_read(&vocab, ann, (int64_t)sizeof(ann), &arep) || !vocab.announced) {
        printf("announce_read failed\n");
        return 2;
    }
`)
	for i, tc := range cases {
		okInt := 0
		if tc.expectOK {
			okInt = 1
		}
		fmt.Fprintf(&src, "    static const uint8_t w%d[] = { %s };\n", i, depthBoundaryHexList(tc.wire, ""))
		fmt.Fprintf(&src, "    check_case(%q, &vocab, w%d, (int64_t)sizeof(w%d), %d);\n", tc.name, i, i, okInt)
	}
	src.WriteString(`    return failed;
}
`)
	mainPath := filepath.Join(dir, "main.c")
	if err := os.WriteFile(mainPath, []byte(src.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "depthprobe")
	args := []string{"-std=c99", "-Wall", "-Wextra", "-Werror", "-O2", "-fsanitize=address,undefined", "-fno-omit-frame-pointer", "-I", dir, mainPath}
	args = append(args, cfiles...)
	args = append(args, "-lm", "-o", bin)
	if out, err := exec.Command(cc, args...).CombinedOutput(); err != nil {
		t.Fatalf("compile C failed: %v\n%s", err, out)
	}
	out, err := exec.Command(bin).CombinedOutput()
	return string(out), err
}

func runAllRuntimesCpp(t *testing.T, u *ir.Unit, ann []byte, cases []depthBoundaryCase, edit func(map[string][]byte)) (string, error) {
	t.Helper()
	cxx := os.Getenv("CXX")
	if cxx == "" {
		cxx = "c++"
	}
	files, err := cpp.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := cpptable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	if edit != nil {
		edit(files)
	}
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
		if strings.HasSuffix(name, ".cpp") {
			sources = append(sources, p)
		}
	}
	var src strings.Builder
	src.WriteString(`#include "DepthProbeTable.h"
#include <cstdio>
#include <cstdint>
#include <cstring>
using namespace depthprobe;

static bool failed = false;

static void check_case(const char * name, const TableVocabulary & vocab, const uint8_t * wire, int64_t len, bool expect_ok)
{
    Root values[2];
    RootReset(values[0]);
    RootReset(values[1]);
    values[0].a = -7;
    values[1].a = -7;
    int64_t count = 2;
    TableReport rep;
    memset(&rep, 0, sizeof(rep));
    bool ok = RootLoadMessages(values, &count, vocab, wire, len, &rep);
    if (expect_ok) {
        if (ok && count == 2 && !rep.malformed && !rep.refused && rep.unknown == 1 && values[0].a == 42 && values[1].a == 99) {
            printf("PASS %s\n", name);
        } else {
            printf("FAIL %s: ok=%d count=%lld malformed=%d refused=%d unknown=%d a0=%d a1=%d\n",
                name, (int)ok, (long long)count, (int)rep.malformed, (int)rep.refused, (int)rep.unknown, (int)values[0].a, (int)values[1].a);
            failed = true;
        }
    } else {
        if (!ok && count == 0 && rep.malformed && !rep.refused) {
            printf("PASS %s\n", name);
        } else {
            printf("FAIL %s: ok=%d count=%lld malformed=%d refused=%d a0=%d a1=%d\n",
                name, (int)ok, (long long)count, (int)rep.malformed, (int)rep.refused, (int)values[0].a, (int)values[1].a);
            failed = true;
        }
    }
}

int main(void)
{
`)
	fmt.Fprintf(&src, "    static const uint8_t ann[] = { %s };\n", depthBoundaryHexList(ann, ""))
	src.WriteString(`    TableMessageEntry entries[kTableMessageEntriesHere];
    TableVocabulary vocab(entries, kTableMessageEntriesHere);
    TableReport arep;
    memset(&arep, 0, sizeof(arep));
    if (!AnnounceRead(vocab, ann, (int64_t)sizeof(ann), &arep) || !vocab.announced) {
        printf("AnnounceRead failed\n");
        return 2;
    }
`)
	for i, tc := range cases {
		fmt.Fprintf(&src, "    static const uint8_t w%d[] = { %s };\n", i, depthBoundaryHexList(tc.wire, ""))
		fmt.Fprintf(&src, "    check_case(%q, vocab, w%d, (int64_t)sizeof(w%d), %v);\n", tc.name, i, i, tc.expectOK)
	}
	src.WriteString(`    return failed ? 1 : 0;
}
`)
	mainPath := filepath.Join(dir, "main.cpp")
	if err := os.WriteFile(mainPath, []byte(src.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "depthprobe")
	args := []string{"-std=c++17", "-O2", "-fsanitize=address,undefined", "-fno-omit-frame-pointer", "-I", dir, mainPath}
	args = append(args, sources...)
	args = append(args, "-o", bin)
	if out, err := exec.Command(cxx, args...).CombinedOutput(); err != nil {
		t.Fatalf("compile C++ failed: %v\n%s", err, out)
	}
	out, err := exec.Command(bin).CombinedOutput()
	return string(out), err
}

func runAllRuntimesCSharp(t *testing.T, u *ir.Unit, ann []byte, cases []depthBoundaryCase, edit func(map[string][]byte)) (string, error) {
	t.Helper()
	dotnetName := os.Getenv("DOTNET")
	if dotnetName == "" {
		dotnetName = "dotnet"
	}
	dotnetPath, err := exec.LookPath(dotnetName)
	if err != nil {
		t.Fatalf("dotnet: %v", err)
	}
	resolved, err := filepath.EvalSymlinks(dotnetPath)
	if err != nil {
		resolved = dotnetPath
	}
	root := filepath.Dir(resolved)
	cscs, _ := filepath.Glob(filepath.Join(root, "sdk", "*", "Roslyn", "bincore", "csc.dll"))
	if len(cscs) == 0 {
		t.Fatalf("no csc.dll under %s", root)
	}
	sort.Strings(cscs)
	csc := cscs[len(cscs)-1]
	refDirs, _ := filepath.Glob(filepath.Join(root, "packs", "Microsoft.NETCore.App.Ref", "*", "ref", "net*"))
	if len(refDirs) == 0 {
		t.Fatalf("no ref assemblies under %s", root)
	}
	sort.Strings(refDirs)
	refs, _ := filepath.Glob(filepath.Join(refDirs[len(refDirs)-1], "*.dll"))
	runtimes, _ := filepath.Glob(filepath.Join(root, "shared", "Microsoft.NETCore.App", "*"))
	if len(runtimes) == 0 {
		t.Fatalf("no shared runtime under %s", root)
	}
	sort.Strings(runtimes)
	runtime := filepath.Base(runtimes[len(runtimes)-1])

	files, err := cstable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	if edit != nil {
		edit(files)
	}
	dir := t.TempDir()
	var sources []string
	for name, data := range files {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, data, 0o600); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, p)
	}
	var prog strings.Builder
	prog.WriteString(`using System;
using Depthprobe;

static class Program
{
    static bool failed = false;
    static void CheckCase(string name, TableVocabulary vocab, byte[] wire, bool expectOK)
    {
        var values = new Root[] { new Root { A = -7 }, new Root { A = -7 } };
        var rep = new TableReport();
        var verdict = Schema.RootLoadMessages(values, wire, vocab, rep, out int count);
        if (expectOK)
        {
            if (verdict == Schema.TableWire.Verdict.Ok && count == 2 && !rep.Malformed && !rep.Refused && rep.Unknown == 1 && values[0].A == 42 && values[1].A == 99)
                Console.WriteLine("PASS " + name);
            else { Console.WriteLine("FAIL " + name); failed = true; }
        }
        else
        {
            if (verdict == Schema.TableWire.Verdict.Damaged && count == 0 && rep.Malformed && !rep.Refused)
                Console.WriteLine("PASS " + name);
            else { Console.WriteLine("FAIL " + name); failed = true; }
        }
    }
    static int Main()
    {
`)
	fmt.Fprintf(&prog, "        byte[] ann = new byte[]{ %s };\n", depthBoundaryHexList(ann, ""))
	prog.WriteString(`        var vocab = new TableVocabulary();
        var arep = new TableReport();
        if (Schema.AnnounceRead(vocab, ann, arep) != Schema.TableWire.Verdict.Ok || !vocab.Announced) return 2;
`)
	for _, tc := range cases {
		fmt.Fprintf(&prog, "        CheckCase(%q, vocab, new byte[]{ %s }, %v);\n", tc.name, depthBoundaryHexList(tc.wire, ""), tc.expectOK)
	}
	prog.WriteString("        return failed ? 1 : 0;\n    }\n}\n")
	probePath := filepath.Join(dir, "Program.cs")
	if err := os.WriteFile(probePath, []byte(prog.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	app := filepath.Join(dir, "depthprobe.dll")
	args := []string{"exec", csc, "-nologo", "-noconfig", "-target:exe", "-optimize+", "-unsafe",
		"-nullable:annotations", "-langversion:latest", "-nostdlib+", "-out:" + app}
	for _, ref := range refs {
		args = append(args, "-r:"+ref)
	}
	args = append(args, sources...)
	args = append(args, probePath)
	env := append(os.Environ(), "DOTNET_NOLOGO=1", "DOTNET_SKIP_FIRST_TIME_EXPERIENCE=1", "HOME="+dir, "DOTNET_CLI_HOME="+dir, "TMPDIR="+dir)
	compile := exec.Command(dotnetPath, args...)
	compile.Env, compile.Dir = env, dir
	if out, err := compile.CombinedOutput(); err != nil {
		t.Fatalf("csc compile failed: %v\n%s", err, out)
	}
	cfg := fmt.Sprintf("{\"runtimeOptions\":{\"tfm\":\"net10.0\",\"framework\":{\"name\":\"Microsoft.NETCore.App\",\"version\":%q}}}\n", runtime)
	if err := os.WriteFile(filepath.Join(dir, "depthprobe.runtimeconfig.json"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	run := exec.Command(dotnetPath, "exec", app)
	run.Env, run.Dir = env, dir
	out, err := run.CombinedOutput()
	return string(out), err
}

func runAllRuntimesDart(t *testing.T, u *ir.Unit, ann []byte, cases []depthBoundaryCase, edit func(map[string][]byte)) (string, error) {
	t.Helper()
	dartBin := os.Getenv("DART")
	if dartBin == "" {
		for _, cand := range []string{"dart", "/opt/flutter/bin/dart", "/usr/local/bin/dart", "/opt/homebrew/bin/dart"} {
			if p, err := exec.LookPath(cand); err == nil {
				dartBin = p
				break
			}
		}
	}
	if dartBin == "" {
		t.Fatal("dart binary not found")
	}
	files, err := dart.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := darttable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	if edit != nil {
		edit(files)
	}
	dir := t.TempDir()
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var probe strings.Builder
	probe.WriteString(`import 'dart:io';
import 'dart:typed_data';
import 'DepthProbeFixed.dart' as h;
import 'DepthProbeFixed.dart' as r;

void main() {
`)
	fmt.Fprintf(&probe, "  final ann = Uint8List.fromList(<int>[%s]);\n", depthBoundaryHexList(ann, ""))
	probe.WriteString(`  final vocab = h.TableVocabulary(entries: List<h.TableMessageEntry>.generate(64, (i) => h.TableMessageEntry(0, 0)));
  final arep = h.TableReport();
  if (!h.announceRead(vocab, ann, arep) || !vocab.announced) { exit(2); }
  var failed = false;
  void checkCase(String name, List<int> raw, bool expectOK) {
    final v0 = h.Root()..a = -7;
    final v1 = h.Root()..a = -7;
    final values = <h.Root>[v0, v1];
    final rep = h.TableReport();
    final count = r.rootLoadMessages(values, Uint8List.fromList(raw), vocab, rep);
    if (expectOK) {
      if (count == 2 && !rep.malformed && !rep.refused && rep.unknown == 1 && values[0].a == 42 && values[1].a == 99) {
        print('PASS $name');
      } else {
        print('FAIL $name');
        failed = true;
      }
    } else {
      if (count == 0 && rep.malformed && !rep.refused) {
        print('PASS $name');
      } else {
        print('FAIL $name');
        failed = true;
      }
    }
  }
`)
	for _, tc := range cases {
		fmt.Fprintf(&probe, "  checkCase(%q, <int>[%s], %v);\n", tc.name, depthBoundaryHexList(tc.wire, ""), tc.expectOK)
	}
	probe.WriteString("  if (failed) exit(1);\n}\n")
	if err := os.WriteFile(filepath.Join(dir, "probe.dart"), []byte(probe.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(dartBin, "--enable-asserts", "probe.dart")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func runAllRuntimesElixir(t *testing.T, u *ir.Unit, ann []byte, cases []depthBoundaryCase, edit func(map[string][]byte)) (string, error) {
	t.Helper()
	elixirBin, err := exec.LookPath("elixir")
	if err != nil {
		t.Fatalf("elixir: %v", err)
	}
	tables, err := elixirtable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	if edit != nil {
		edit(tables)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "TableMessage.ex"), tables["TableMessage.ex"], 0o600); err != nil {
		t.Fatal(err)
	}
	var probe strings.Builder
	probe.WriteString(`defmodule DepthProbe do
  alias Depthprobe.TableMessage, as: M
  def check_case(name, vocab, wire, true) do
    case M.load_messages(vocab, wire, 2) do
      {:ok, [b0, b1]} ->
        if length(b0) == 2 and Enum.at(b0, 1) == {1, 42} and b1 == [{1, 99}] do
          IO.puts("PASS " <> name)
        else
          IO.puts("FAIL " <> name)
          Process.put(:failed, true)
        end
      other ->
        IO.puts("FAIL " <> name <> ": " <> inspect(other))
        Process.put(:failed, true)
    end
  end
  def check_case(name, vocab, wire, false) do
    case M.load_messages(vocab, wire, 2) do
      {:malformed, 0, []} ->
        IO.puts("PASS " <> name)
      other ->
        IO.puts("FAIL " <> name <> ": " <> inspect(other))
        Process.put(:failed, true)
    end
  end
  def run do
`)
	fmt.Fprintf(&probe, "    {:ok, vocab} = M.announce_read(<<%s>>)\n", depthBoundaryHexList(ann, ""))
	for _, tc := range cases {
		fmt.Fprintf(&probe, "    check_case(%q, vocab, <<%s>>, %v)\n", tc.name, depthBoundaryHexList(tc.wire, ""), tc.expectOK)
	}
	probe.WriteString("    if Process.get(:failed), do: System.halt(1)\n  end\nend\nDepthProbe.run()\n")
	if err := os.WriteFile(filepath.Join(dir, "probe.exs"), []byte(probe.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(elixirBin, "-r", "TableMessage.ex", "probe.exs")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func runAllRuntimesGo(t *testing.T, u *ir.Unit, ann []byte, cases []depthBoundaryCase, edit func(map[string][]byte)) (string, error) {
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
	if edit != nil {
		edit(files)
	}
	dir := t.TempDir()
	runtime := slowtest.SerializeGo(t, "../../../")
	files["go.mod"] = []byte(fmt.Sprintf("module depthprobe\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go/v2 v2.0.0\nreplace github.com/mas-bandwidth/serialize.go/v2 => %q\n", runtime))
	var testSrc strings.Builder
	testSrc.WriteString(`package depthprobe

import (
	"fmt"
	"testing"
)

func TestProbe(t *testing.T) {
`)
	fmt.Fprintf(&testSrc, "\tann := []byte{%s}\n", depthBoundaryHexList(ann, ""))
	testSrc.WriteString(`	var storage [TableMessageEntriesHere]TableMessageEntry
	var vocab TableVocabulary
	vocab.Init(storage[:])
	var arep TableReport
	if !AnnounceRead(&vocab, ann, &arep) || !vocab.Announced {
		t.Fatalf("AnnounceRead failed: %+v", arep)
	}
	failed := false
	checkCase := func(name string, wire []byte, expectOK bool) {
		values := [2]Root{{A: -7}, {A: -7}}
		var rep TableReport
		n, ok := RootLoadMessages(values[:], &vocab, wire, &rep)
		if expectOK {
			if ok && n == 2 && !rep.Malformed && rep.Reason == "" && rep.Unknown == 1 && values[0].A == 42 && values[1].A == 99 {
				fmt.Println("PASS " + name)
			} else {
				fmt.Println("FAIL " + name)
				failed = true
			}
		} else {
			if !ok && n == 0 && rep.Malformed && rep.Reason == "" {
				fmt.Println("PASS " + name)
			} else {
				fmt.Println("FAIL " + name)
				failed = true
			}
		}
	}
`)
	for _, tc := range cases {
		fmt.Fprintf(&testSrc, "\tcheckCase(%q, []byte{%s}, %v)\n", tc.name, depthBoundaryHexList(tc.wire, ""), tc.expectOK)
	}
	testSrc.WriteString("\tif failed { t.Fatal(\"failed\") }\n}\n")
	files["probe_test.go"] = []byte(testSrc.String())
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-v", "-count=1", ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func runAllRuntimesJava(t *testing.T, u *ir.Unit, ann []byte, cases []depthBoundaryCase, edit func(map[string][]byte)) (string, error) {
	t.Helper()
	javac, err := exec.LookPath("javac")
	if err != nil {
		t.Fatalf("javac: %v", err)
	}
	javaBin, err := exec.LookPath("java")
	if err != nil {
		t.Fatalf("java: %v", err)
	}
	files, err := javatable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	pkt, err := javacodegen.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	if edit != nil {
		edit(files)
	}
	dir := t.TempDir()
	srcDir := filepath.Join(dir, "src", u.Package)
	classes := filepath.Join(dir, "classes")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	args := []string{"--release", "17", "-nowarn", "-d", classes}
	var names []string
	for name := range files {
		keep := name == "TableFixed.java" ||
			name == "TableVocabulary.java" ||
			name == "TableMessage.java" ||
			strings.HasSuffix(name, "Fixed.java") ||
			strings.HasSuffix(name, "Message.java")
		if keep {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		p := filepath.Join(srcDir, name)
		if err := os.WriteFile(p, files[name], 0o644); err != nil {
			t.Fatal(err)
		}
		args = append(args, p)
	}
	for _, name := range []string{"Int128.java", "UInt128.java"} {
		if data, ok := pkt[name]; ok {
			p := filepath.Join(srcDir, name)
			if err := os.WriteFile(p, data, 0o644); err != nil {
				t.Fatal(err)
			}
			args = append(args, p)
		}
	}
	var driver strings.Builder
	fmt.Fprintf(&driver, "package %s;\npublic final class Driver {\n", u.Package)
	driver.WriteString(`    static boolean failed = false;
    static void checkCase(String name, TableVocabulary vocab, byte[] wire, boolean expectOK) {
        RootFixed.Value v0 = new RootFixed.Value(); v0.a = -7;
        RootFixed.Value v1 = new RootFixed.Value(); v1.a = -7;
        RootFixed.Value[] values = new RootFixed.Value[]{v0, v1};
        TableVocabulary.Report rep = new TableVocabulary.Report();
        int count = RootMessage.loadMessages(values, values.length, vocab, wire, rep);
        if (expectOK) {
            if (!rep.malformed && !rep.refused && count == 2 && rep.unknown == 1 && values[0].a == 42 && values[1].a == 99)
                System.out.println("PASS " + name);
            else { System.out.println("FAIL " + name); failed = true; }
        } else {
            if (count == 0 && rep.malformed && !rep.refused)
                System.out.println("PASS " + name);
            else { System.out.println("FAIL " + name); failed = true; }
        }
    }
    public static void main(String[] args) {
`)
	var javaAnn []string
	for _, b := range ann {
		javaAnn = append(javaAnn, fmt.Sprintf("(byte)0x%02x", b))
	}
	fmt.Fprintf(&driver, "        byte[] ann = new byte[]{%s};\n", strings.Join(javaAnn, ", "))
	driver.WriteString(`        TableVocabulary vocab = new TableVocabulary();
        vocab.reset(64);
        TableVocabulary.Report arep = new TableVocabulary.Report();
        if (!TableVocabulary.announceRead(vocab, ann, arep) || !vocab.announced) System.exit(2);
`)
	for _, tc := range cases {
		var jb []string
		for _, b := range tc.wire {
			jb = append(jb, fmt.Sprintf("(byte)0x%02x", b))
		}
		fmt.Fprintf(&driver, "        checkCase(%q, vocab, new byte[]{%s}, %v);\n", tc.name, strings.Join(jb, ", "), tc.expectOK)
	}
	driver.WriteString("        if (failed) System.exit(1);\n    }\n}\n")
	driverPath := filepath.Join(srcDir, "Driver.java")
	if err := os.WriteFile(driverPath, []byte(driver.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	args = append(args, driverPath)
	if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
	out, err := exec.Command(javaBin, "-ea", "-cp", classes, u.Package+".Driver").CombinedOutput()
	return string(out), err
}

func runAllRuntimesJS(t *testing.T, u *ir.Unit, ann []byte, cases []depthBoundaryCase, edit func(map[string][]byte)) (string, error) {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatalf("node: %v", err)
	}
	files, err := js.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := jstable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	if edit != nil {
		edit(files)
	}
	dir := t.TempDir()
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var probe strings.Builder
	probe.WriteString(`import * as m from "./DepthProbeTable.js";
const ann = new Uint8Array([` + depthBoundaryHexList(ann, "") + `]);
const vocab = new m.TableVocabulary();
vocab.Init(new Array(m.TableMessageEntriesHere));
const arep = {};
if (!m.AnnounceRead(vocab, ann, arep) || !vocab.Announced) process.exit(2);
let failed = false;
function checkCase(name, wire, expectOK) {
  const v0 = new m.Root(); v0.a = -7;
  const v1 = new m.Root(); v1.a = -7;
  const values = [v0, v1];
  const rep = {};
  const count = m.RootLoadMessages(values, vocab, wire, rep);
  const ok = rep.Verdict === 0 && !rep.Malformed && !rep.Refused;
  if (expectOK) {
    if (ok && count === 2 && rep.Unknown === 1 && values[0].a === 42 && values[1].a === 99)
      console.log("PASS " + name);
    else { console.log("FAIL " + name); failed = true; }
  } else {
    if (!ok && count === 0 && rep.Malformed && !rep.Refused)
      console.log("PASS " + name);
    else { console.log("FAIL " + name); failed = true; }
  }
}
`)
	for _, tc := range cases {
		fmt.Fprintf(&probe, "checkCase(%q, new Uint8Array([%s]), %v);\n", tc.name, depthBoundaryHexList(tc.wire, ""), tc.expectOK)
	}
	probe.WriteString("if (failed) process.exit(1);\n")
	if err := os.WriteFile(filepath.Join(dir, "probe.mjs"), []byte(probe.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, filepath.Join(dir, "probe.mjs"))
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func runAllRuntimesRust(t *testing.T, u *ir.Unit, ann []byte, cases []depthBoundaryCase, edit func(map[string][]byte)) (string, error) {
	t.Helper()
	if _, err := exec.LookPath("rustc"); err != nil {
		t.Fatalf("rustc: %v", err)
	}
	files, err := rusttable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	if edit != nil {
		edit(files)
	}
	dir := t.TempDir()
	var mods []string
	for name, data := range files {
		keep := name == rusttable.BuildVersionModule+".rs" || name == rusttable.FixedRuntimeModule+".rs" || name == rusttable.MessageRuntimeModule+".rs" ||
			name == rusttable.VariableRuntimeModule+".rs" ||
			strings.HasSuffix(name, "_records.rs") || strings.HasSuffix(name, "_fixed.rs") || strings.HasSuffix(name, "_message.rs")
		if !keep {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
		mods = append(mods, strings.TrimSuffix(name, ".rs"))
	}
	sort.Strings(mods)
	var main strings.Builder
	main.WriteString("#![allow(dead_code, unused_imports, unused_variables)]\n")
	for _, m := range mods {
		fmt.Fprintf(&main, "mod %s;\npub use %s::*;\n", m, m)
	}
	main.WriteString(`
fn check_case(name: &str, vocab: &TableVocabulary, wire: &[u8], expect_ok: bool, failed: &mut bool) {
    let mut values = [RootRow { a: -7 }, RootRow { a: -7 }];
    let mut rep = TableReport::default();
    let (count, ok) = root_load_messages(&mut values, vocab, wire, &mut rep);
    if expect_ok {
        if ok && count == 2 && !rep.malformed && !rep.refused && rep.unknown == 1 && values[0].a == 42 && values[1].a == 99 {
            println!("PASS {name}");
        } else {
            println!("FAIL {name}");
            *failed = true;
        }
    } else {
        if !ok && count == 0 && rep.malformed && !rep.refused {
            println!("PASS {name}");
        } else {
            println!("FAIL {name}");
            *failed = true;
        }
    }
}
fn main() {
`)
	fmt.Fprintf(&main, "    let ann: &[u8] = &[%s];\n", depthBoundaryHexList(ann, "u8"))
	main.WriteString(`    let mut storage = vec![TableMessageEntry::default(); 64];
    let mut vocab = TableVocabulary::new(&mut storage);
    let mut arep = TableReport::default();
    assert!(announce_read(&mut vocab, ann, &mut arep) && vocab.announced);
    let mut failed = false;
`)
	for _, tc := range cases {
		fmt.Fprintf(&main, "    check_case(%q, &vocab, &[%s], %v, &mut failed);\n", tc.name, depthBoundaryHexList(tc.wire, "u8"), tc.expectOK)
	}
	main.WriteString("    if failed { std::process::exit(1); }\n}\n")
	if err := os.WriteFile(filepath.Join(dir, "main.rs"), []byte(main.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "depthprobe")
	cmd := exec.Command("rustc", "--edition", "2024", "-o", bin, "main.rs")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("rustc: %v\n%s", err, out)
	}
	run := exec.Command(bin)
	run.Dir = dir
	out, err := run.CombinedOutput()
	return string(out), err
}

func shiftCGuard(t *testing.T, files map[string][]byte) {
	t.Helper()
	replaced := false
	for name, data := range files {
		s := string(data)
		if strings.Contains(s, "skip_depth>128") || strings.Contains(s, "depth>128") {
			s = strings.ReplaceAll(s, "skip_depth>128", "skip_depth>256")
			s = strings.ReplaceAll(s, "depth>128", "depth>256")
			files[name] = []byte(s)
			replaced = true
		}
	}
	if !replaced {
		t.Fatalf("expected generated C files to contain skip_depth>128")
	}
}

func TestGeneratedMessageDepthBoundary(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the c compiler (it compiles and runs the generated depth probe)")
	u, ann, cases := depthBoundaryFixtures(t)
	out, err := runCDepthBoundaryProbe(t, u, ann, cases, nil)
	shiftedOut, shiftedErr := runCDepthBoundaryProbe(t, u, ann, cases, func(files map[string][]byte) {
		shiftCGuard(t, files)
	})
	assertDepthBoundaryOutput(t, "c", cases, out, err, shiftedOut, shiftedErr)
}

func TestGeneratedMessageDepthBoundaryAllRuntimes(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "every runtime's toolchain (it compiles and runs the depth probe in all nine)")
	u, ann, cases := depthBoundaryFixtures(t)

	runtimes := []struct {
		name  string
		run   func(*testing.T, *ir.Unit, []byte, []depthBoundaryCase, func(map[string][]byte)) (string, error)
		shift func(*testing.T, map[string][]byte)
	}{
		{
			name:  "c",
			run:   runCDepthBoundaryProbe,
			shift: shiftCGuard,
		},
		{
			name: "cpp",
			run:  runAllRuntimesCpp,
			shift: func(t *testing.T, files map[string][]byte) {
				for name, data := range files {
					files[name] = []byte(strings.ReplaceAll(string(data), "if ( depth > 128 )", "if ( depth > 256 )"))
				}
			},
		},
		{
			name: "csharp",
			run:  runAllRuntimesCSharp,
			shift: func(t *testing.T, files map[string][]byte) {
				for name, data := range files {
					files[name] = []byte(strings.ReplaceAll(string(data), "if (depth > 128)", "if (depth > 256)"))
				}
			},
		},
		{
			name: "dart",
			run:  runAllRuntimesDart,
			shift: func(t *testing.T, files map[string][]byte) {
				for name, data := range files {
					files[name] = []byte(strings.ReplaceAll(string(data), "if (depth > 128)", "if (depth > 256)"))
				}
			},
		},
		{
			name: "elixir",
			run:  runAllRuntimesElixir,
			shift: func(t *testing.T, files map[string][]byte) {
				for name, data := range files {
					files[name] = []byte(strings.ReplaceAll(string(data), "when depth > 128", "when depth > 256"))
				}
			},
		},
		{
			name: "go",
			run:  runAllRuntimesGo,
			shift: func(t *testing.T, files map[string][]byte) {
				for name, data := range files {
					files[name] = []byte(strings.ReplaceAll(string(data), "if depth > 128", "if depth > 256"))
				}
			},
		},
		{
			name: "java",
			run:  runAllRuntimesJava,
			shift: func(t *testing.T, files map[string][]byte) {
				for name, data := range files {
					files[name] = []byte(strings.ReplaceAll(string(data), "if (depth > 128", "if (depth > 256"))
				}
			},
		},
		{
			name: "js",
			run:  runAllRuntimesJS,
			shift: func(t *testing.T, files map[string][]byte) {
				for name, data := range files {
					files[name] = []byte(strings.ReplaceAll(string(data), "if (depth > 128)", "if (depth > 256)"))
				}
			},
		},
		{
			name: "rust",
			run:  runAllRuntimesRust,
			shift: func(t *testing.T, files map[string][]byte) {
				for name, data := range files {
					files[name] = []byte(strings.ReplaceAll(string(data), "if depth > 128", "if depth > 256"))
				}
			},
		},
	}

	for _, rt := range runtimes {
		t.Run(rt.name, func(t *testing.T) {
			t.Parallel()
			out, err := rt.run(t, u, ann, cases, nil)
			shiftedOut, shiftedErr := rt.run(t, u, ann, cases, func(files map[string][]byte) {
				rt.shift(t, files)
			})
			assertDepthBoundaryOutput(t, rt.name, cases, out, err, shiftedOut, shiftedErr)
		})
	}
}
