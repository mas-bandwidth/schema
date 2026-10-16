package gotable

import (
	"bytes"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/ir"
)

var surfaceSchema = `package probe
flags Caps { Jump, Fly }
enum Grade {
 Gold | was = "OldGold"
}
type Badge {
 title string(8) = "fresh" | was = "label"
}
union Choice {
 Empty
 Number int32
 Badge Badge
}
fixed table Root {
 title string(12) = "default"
 tag bytes(4) = "ab"
 caps Caps = { Jump, Fly }
 badge Badge
 values ?[..3]int32
 grades ?[2]Grade
}
fixed table Armed {
 choice Choice
}
`

var surfaceVariableSchema = surfaceSchema + `
table Node {
 value int32 = 3
}
table VarRoot {
 id int32 = 7
 label string(16) = "var"
 badge Badge
 node *Node
 choice Choice
 items [..4]int32
 scores map[string(8)]int32
}
`

// surfaceFileSuffix maps each surface that owns a whole file to the distinctive
// path suffix that file carries when the surface is on.
var surfaceFileSuffix = map[string]string{
	"json":    "TableJson.go",
	"cook":    "Cook.go",
	"block":   "Block.go",
	"view":    "View.go",
	"message": "TableMessage.go",
}

func genWithSurfaces(t *testing.T, u *ir.Unit, s Surfaces) map[string][]byte {
	t.Helper()
	out, err := GenerateLineageSurface(u, nil, s)
	if err != nil {
		t.Fatalf("GenerateLineageSurface(%v): %v", s, err)
	}
	return out
}

func moduleBySuffix(out map[string][]byte, suffix string) string {
	var b strings.Builder
	for name, data := range out {
		if strings.HasSuffix(name, suffix) {
			b.Write(data)
		}
	}
	return b.String()
}

func tableModule(out map[string][]byte) string {
	return moduleBySuffix(out, "Table.go")
}

func assertSameOutput(t *testing.T, label string, want, got map[string][]byte) {
	t.Helper()
	if len(want) != len(got) {
		t.Fatalf("%s: want %d files, got %d", label, len(want), len(got))
	}
	for name, data := range want {
		other, ok := got[name]
		if !ok {
			t.Errorf("%s: missing %s", label, name)
		} else if !bytes.Equal(data, other) {
			t.Errorf("%s: %s differs", label, name)
		}
	}
}

func resolveSerializeGo(t *testing.T) string {
	t.Helper()
	if runtime, err := filepath.Abs("../../../../serialize.go"); err == nil {
		if _, statErr := os.Stat(filepath.Join(runtime, "go.mod")); statErr == nil {
			return runtime
		}
	}
	gc, err := exec.Command("go", "env", "GOMODCACHE").Output()
	if err != nil {
		t.Fatal(err)
	}
	matches, _ := filepath.Glob(filepath.Join(strings.TrimSpace(string(gc)), "github.com/mas-bandwidth/serialize.go@*"))
	if len(matches) == 0 {
		t.Fatal("serialize.go is neither staged at ../../../../serialize.go nor in GOMODCACHE")
	}
	return matches[0]
}

func buildProbeTestSource(s Surfaces, variable bool) string {
	var b strings.Builder
	b.WriteString("package probe\n\nimport (\n\t\"bytes\"\n\t\"testing\"\n\t\"unsafe\"\n)\n\n")
	b.WriteString("var _ = bytes.Equal\nvar _ = unsafe.Pointer(nil)\n\n")
	b.WriteString(`func TestSurfaceRoundTrips(t *testing.T) {
	var v Root
	RootReset(&v)
	copy(v.Title[:], "hello")
	v.TitleLength = 5
	v.ValuesPresent = true
	v.ValuesCount = 2
	v.Values[0] = 11
	v.Values[1] = 22
	n := RootMeasure(&v)
	if n <= 0 {
		t.Fatalf("RootMeasure = %d", n)
	}
	wire := make([]byte, n)
	if wrote := RootSave(&v, wire); wrote != n {
		t.Fatalf("RootSave = %d, want %d", wrote, n)
	}
	var dst Root
	var rep TableReport
	if !RootLoad(&dst, wire, &rep) || rep.Malformed {
		t.Fatalf("RootLoad failed: %+v", rep)
	}
	if dst.TitleLength != 5 || string(dst.Title[:5]) != "hello" || !dst.ValuesPresent || dst.ValuesCount != 2 || dst.Values[0] != 11 || dst.Values[1] != 22 {
		t.Fatalf("Root wire round-trip mismatch: %+v", dst)
	}
`)
	if variable {
		b.WriteString(`
	var vb VarRootBuilder
	if !vb.Init() {
		t.Fatal("VarRootBuilder.Init failed")
	}
	defer vb.Shutdown()
	vr := vb.GetRoot()
	vr.Id = 77
	copy(vr.Label[:], "region")
	vr.LabelLength = 6
	node := NodeEmplace(&vb.Main, &vr.Node)
	node.Value = 99
	vn := VarRootMeasure(vr, &vb.Arena)
	if vn <= 0 {
		t.Fatalf("VarRootMeasure = %d", vn)
	}
	vwire := make([]byte, vn)
	if wrote := VarRootSave(vr, vwire, &vb.Arena); wrote != vn {
		t.Fatalf("VarRootSave = %d, want %d", wrote, vn)
	}
	rsize := VarRootLoadMeasure(vwire)
	if rsize <= 0 {
		t.Fatalf("VarRootLoadMeasure = %d", rsize)
	}
	raw := make([]byte, rsize+63)
	off := (-uintptr(unsafe.Pointer(&raw[0]))) & 63
	region := raw[off : off+uintptr(rsize)]
	var vrep TableReport
	vloaded := VarRootLoad(region, vwire, &vrep)
	if vloaded == nil || vrep.Malformed || vloaded.Id != 77 || vloaded.LabelLength != 6 || string(vloaded.Label[:6]) != "region" {
		t.Fatalf("VarRoot wire round-trip mismatch: %+v rep=%+v", vloaded, vrep)
	}
`)
	}
	if s.Fixed && !variable {
		b.WriteString(`
	fn := RootFixedMeasure(1)
	if fn <= 0 {
		t.Fatalf("RootFixedMeasure = %d", fn)
	}
	fbuf := make([]byte, fn)
	if wrote := RootFixedSave([]Root{v}, fbuf); wrote != fn {
		t.Fatalf("RootFixedSave = %d, want %d", wrote, fn)
	}
	var fdst [1]Root
	plan := make([]TableFixedEntry, RootFixedPlanMax)
	var frep TableReport
	if got := RootFixedLoad(fdst[:], fbuf, plan, &frep); got != 1 || frep.Malformed || fdst[0].Values[1] != 22 {
		t.Fatalf("RootFixedLoad = %d rep=%+v dst=%+v", got, frep, fdst[0])
	}
`)
	}
	if s.JSON {
		b.WriteString(`
	jn := RootToJsonMeasure(&v)
	if jn <= 0 {
		t.Fatalf("RootToJsonMeasure = %d", jn)
	}
	jbuf := make([]byte, jn)
	if wrote := RootToJson(&v, jbuf); wrote != jn {
		t.Fatalf("RootToJson = %d, want %d", wrote, jn)
	}
	var jdst Root
	var jrep TableReport
	if !RootFromJson(&jdst, jbuf, &jrep) || jrep.Malformed || jdst.Values[0] != 11 {
		t.Fatalf("RootFromJson failed: rep=%+v dst=%+v", jrep, jdst)
	}
`)
		if variable {
			b.WriteString(`
	if !vb.Lock() {
		t.Fatal("vb.Lock failed")
	}
	vconst := vb.AsConst()
	vjn := VarRootToJsonMeasure(vconst)
	if vjn <= 0 {
		t.Fatalf("VarRootToJsonMeasure = %d", vjn)
	}
	vjbuf := make([]byte, vjn)
	if wrote := VarRootToJson(vconst, vjbuf); wrote != vjn {
		t.Fatalf("VarRootToJson = %d, want %d", wrote, vjn)
	}
	var jb VarRootBuilder
	if !jb.Init() {
		t.Fatal("jb.Init failed")
	}
	defer jb.Shutdown()
	var vjrep TableReport
	if !VarRootFromJson(&jb, vjbuf, &vjrep) || vjrep.Malformed || jb.GetRoot().Id != 77 {
		t.Fatalf("VarRootFromJson failed: rep=%+v", vjrep)
	}
`)
		}
	}
	if s.Cook {
		b.WriteString(`
	cn := RootCookMeasure(&v)
	if cn <= 0 {
		t.Fatalf("RootCookMeasure = %d", cn)
	}
	craw := make([]byte, cn+63)
	coff := (-uintptr(unsafe.Pointer(&craw[0]))) & 63
	cbuf := craw[coff : coff+uintptr(cn)]
	if !RootCookFrom(&v, cbuf, TableByteOrderLittle) {
		t.Fatal("RootCookFrom failed")
	}
	var cook RootCook
	if !RootOpen(&cook, unsafe.Pointer(&cbuf[0]), cn) || cook.Root().ValuesCount != 2 {
		t.Fatalf("RootOpen failed: count=%d", cook.Root().ValuesCount)
	}
`)
	}
	if s.Block {
		b.WriteString(`
	var blk RootBlock
	if blk.Type().Name != "Root" {
		t.Fatalf("RootBlock metadata mismatch: name=%q", blk.Type().Name)
	}
`)
	}
	if s.View {
		b.WriteString(`
	uv := UnitView()
	if uv == nil || uv.Package != "probe" || uv.NumTables < 2 {
		t.Fatalf("UnitView mismatch: %+v", uv)
	}
`)
	}
	if s.Message {
		b.WriteString(`
	ann := make([]byte, AnnounceMeasure())
	if Announce(ann) != int64(len(ann)) {
		t.Fatal("Announce failed")
	}
	var vocab TableVocabulary
	entries := make([]TableMessageEntry, TableMessageEntriesHere+8)
	vocab.Init(entries)
	var arep TableReport
	if !AnnounceRead(&vocab, ann, &arep) || arep.Malformed {
		t.Fatalf("AnnounceRead failed: %+v", arep)
	}
	batch := []*Root{&v}
	var mrep TableReport
	mn := RootMeasureMessages(batch, &mrep)
	if mn <= 0 {
		t.Fatalf("RootMeasureMessages = %d", mn)
	}
	mbuf := make([]byte, mn)
	if wrote := RootSaveMessages(batch, mbuf, &mrep); wrote != mn {
		t.Fatalf("RootSaveMessages = %d, want %d", wrote, mn)
	}
	var mdst [1]Root
	var lrep TableReport
	if count, ok := RootLoadMessages(mdst[:], &vocab, mbuf, &lrep); !ok || count != 1 || mdst[0].Values[1] != 22 {
		t.Fatalf("RootLoadMessages count=%d ok=%v rep=%+v", count, ok, lrep)
	}
`)
	}
	if s.Retain {
		b.WriteString(`
	if RootLoadRetain == "" {
		t.Fatal("RootLoadRetain constant is empty")
	}
`)
		if variable {
			b.WriteString(`
	store := TableRetain{Bytes: make([]byte, 1024), Ids: make([]TableRetainId, 32)}
	var rrep TableReport
	rloaded := VarRootLoadRetain(region, vwire, &store, &rrep)
	if rloaded == nil || rrep.Malformed || rloaded.Id != 77 {
		t.Fatalf("VarRootLoadRetain failed: %+v", rrep)
	}
	rm := VarRootMeasureRetain(rloaded, &store)
	if rm != int64(len(vwire)) {
		t.Fatalf("VarRootMeasureRetain = %d, want %d", rm, len(vwire))
	}
	rout := make([]byte, rm)
	rrep = TableReport{}
	if wrote := VarRootSaveRetain(rloaded, &store, rout, &rrep); wrote != rm || !bytes.Equal(rout, vwire) {
		t.Fatalf("VarRootSaveRetain = %d rep=%+v", wrote, rrep)
	}
`)
		}
	}
	b.WriteString("}\n")
	return b.String()
}

func assertCompilesAndRoundTrips(t *testing.T, u *ir.Unit, s Surfaces, variable bool, runtime string) {
	t.Helper()
	files := genWithSurfaces(t, u, s)
	packet, err := golang.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, packet)
	dir := t.TempDir()
	files["go.mod"] = []byte(fmt.Sprintf("module probe\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go v0.0.0\nreplace github.com/mas-bandwidth/serialize.go => %q\n", runtime))
	files["probe_test.go"] = []byte(buildProbeTestSource(s, variable))
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-count=1", ".")
	cmd.Dir = dir
	if outBytes, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated surface %+v (variable=%v) does not compile/run: %v\n%s", s, variable, err, outBytes)
	}
}

// TestGoGenerateSurfaceEmitsOnlyWhatItNames pins the --surface flag
// (docs/SPEC-TABLES.md §8.8): each surface list emits only its files and
// functions, the default equals the full set byte for byte on both fixed-only
// and variable units (with and without fixed lineage), and every selection
// compiles and round-trips.
func TestGoGenerateSurfaceEmitsOnlyWhatItNames(t *testing.T) {
	t.Parallel()
	runtime := resolveSerializeGo(t)
	uFixed := unitFrom(t, surfaceSchema)
	uVar := unitFrom(t, surfaceVariableSchema)

	parsedEmpty, err := ParseSurfaces("")
	if err != nil {
		t.Fatal(err)
	}
	parsedAll, err := ParseSurfaces("wire,fixed,json,cook,block,view,message,retain")
	if err != nil {
		t.Fatal(err)
	}

	rootEntry, ok := FixedLineageOf(uFixed, "Root")
	if !ok {
		t.Fatal("FixedLineageOf(Root) failed")
	}
	lineage := map[string][]FixedLineageEntry{
		"Root": {rootEntry},
	}

	for _, unitCase := range []struct {
		name string
		u    *ir.Unit
	}{
		{"fixed", uFixed},
		{"variable", uVar},
	} {
		baseOut, err := Generate(unitCase.u)
		if err != nil {
			t.Fatalf("%s Generate: %v", unitCase.name, err)
		}
		defOut := genWithSurfaces(t, unitCase.u, DefaultSurfaces())
		emptyOut := genWithSurfaces(t, unitCase.u, parsedEmpty)
		allOut := genWithSurfaces(t, unitCase.u, parsedAll)
		assertSameOutput(t, unitCase.name+" Generate vs DefaultSurfaces", baseOut, defOut)
		assertSameOutput(t, unitCase.name+" Generate vs ParseSurfaces(\"\")", baseOut, emptyOut)
		assertSameOutput(t, unitCase.name+" Generate vs explicit all-8", baseOut, allOut)

		linDef, err := GenerateLineage(unitCase.u, lineage)
		if err != nil {
			t.Fatalf("%s GenerateLineage: %v", unitCase.name, err)
		}
		linAll, err := GenerateLineageSurface(unitCase.u, lineage, parsedAll)
		if err != nil {
			t.Fatalf("%s GenerateLineageSurface: %v", unitCase.name, err)
		}
		assertSameOutput(t, unitCase.name+" GenerateLineage vs GenerateLineageSurface(all-8)", linDef, linAll)
	}

	cases := []string{
		"wire",
		"fixed",
		"json",
		"cook",
		"block",
		"view",
		"message",
		"retain",
		"wire,json",
		"wire,fixed",
		"wire,retain",
		"wire,message",
		"wire,retain,message",
		"json,cook,block",
	}

	for _, list := range cases {
		list := list
		s, err := ParseSurfaces(list)
		if err != nil {
			t.Fatalf("ParseSurfaces(%q): %v", list, err)
		}
		if !s.Wire {
			t.Fatalf("ParseSurfaces(%q): Wire must always be true", list)
		}
		for _, unitCase := range []struct {
			name     string
			u        *ir.Unit
			variable bool
		}{
			{"fixed", uFixed, false},
			{"variable", uVar, true},
		} {
			unitCase := unitCase
			t.Run(unitCase.name+"/"+list, func(t *testing.T) {
				t.Parallel()
				out := genWithSurfaces(t, unitCase.u, s)
				body := tableModule(out)
				if body == "" {
					t.Fatal("the wire table module (<Base>Table.go) is always emitted")
				}
				wantFile := map[string]bool{
					"json":    s.JSON,
					"cook":    s.Cook,
					"block":   s.Block,
					"view":    s.View,
					"message": s.Message,
				}
				for name, suffix := range surfaceFileSuffix {
					got := false
					for f := range out {
						if strings.HasSuffix(f, suffix) {
							got = true
						}
					}
					if got != wantFile[name] {
						t.Errorf("surface %q: file *%s present=%v, want %v", list, suffix, got, wantFile[name])
					}
				}
				wantFixed := s.Fixed && !unitCase.variable
				for _, tok := range []string{"RootFixedWriteBody", "RootFixedPlanMax"} {
					if has := strings.Contains(body, tok); has != wantFixed {
						t.Errorf("surface %q: token %s present=%v, want %v", list, tok, has, wantFixed)
					}
				}
				if has := strings.Contains(body, "RootLoadRetain"); has != s.Retain {
					t.Errorf("surface %q: token RootLoadRetain present=%v, want %v", list, has, s.Retain)
				}
				if unitCase.variable {
					for _, tok := range []string{"VarRootLoadRetain", "VarRootSaveBodyRetain", "VarRootLoadBodyRetain", "RootSaveBodyRetain", "RootLoadBodyRetain"} {
						if has := strings.Contains(body, tok); has != s.Retain {
							t.Errorf("surface %q: variable token %s present=%v, want %v", list, tok, has, s.Retain)
						}
					}
					if s.Message {
						msgBody := moduleBySuffix(out, "TableMessage.go")
						for _, tok := range []string{"VarRootLoadMessageBodyRetain", "RootLoadMessageBodyRetain"} {
							if has := strings.Contains(msgBody, tok); has != s.Retain {
								t.Errorf("surface %q: message token %s present=%v, want %v", list, tok, has, s.Retain)
							}
						}
					}
				}
				assertCompilesAndRoundTrips(t, unitCase.u, s, unitCase.variable, runtime)
			})
		}
	}

	if _, err := ParseSurfaces("bogus"); err == nil {
		t.Error("ParseSurfaces(bogus) should error")
	}

	t.Run("cli", func(t *testing.T) {
		t.Parallel()
		repoRoot, err := filepath.Abs("../../..")
		if err != nil {
			t.Fatal(err)
		}
		tmp := t.TempDir()
		schemaPath := filepath.Join(tmp, "Probe.schema")
		if err := os.WriteFile(schemaPath, []byte(surfaceVariableSchema), 0o600); err != nil {
			t.Fatal(err)
		}
		outDir := filepath.Join(tmp, "out")
		cmd := exec.Command("go", "run", "./cmd/schema", "generate", "--lang", "go", "--surface", "wire,json", "--out", outDir, schemaPath)
		cmd.Dir = repoRoot
		if outBytes, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("schema generate --lang go --surface wire,json failed: %v\n%s", err, outBytes)
		}
		entries, err := os.ReadDir(outDir)
		if err != nil {
			t.Fatal(err)
		}
		gotFiles := map[string]bool{}
		for _, e := range entries {
			gotFiles[e.Name()] = true
		}
		for _, want := range []string{"Probe.go", "ProbeTable.go", "ProbeTableJson.go"} {
			if !gotFiles[want] {
				t.Errorf("CLI --surface wire,json missing %s (got %v)", want, gotFiles)
			}
		}
		for _, forbidden := range []string{"ProbeCook.go", "ProbeBlock.go", "ProbeView.go", "ProbeTableMessage.go"} {
			if gotFiles[forbidden] {
				t.Errorf("CLI --surface wire,json emitted unrequested %s", forbidden)
			}
		}

		badCmd := exec.Command("go", "run", "./cmd/schema", "generate", "--lang", "rust", "--surface", "wire", "--out", filepath.Join(tmp, "rust"), schemaPath)
		badCmd.Dir = repoRoot
		badOut, badErr := badCmd.CombinedOutput()
		if badErr == nil {
			t.Fatal("schema generate --lang rust --surface wire succeeded, want refusal")
		}
		if !strings.Contains(string(badOut), "--surface") || !strings.Contains(string(badOut), "rust") {
			t.Errorf("non-go refusal output should name --surface and rust, got: %s", badOut)
		}
	})
}
