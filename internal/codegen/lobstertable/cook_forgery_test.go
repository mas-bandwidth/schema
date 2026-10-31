package lobstertable_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// cookShapes is the chain each cook case's fixture is generated with
// (test/conformance/harness/run.go, cookShape): the root, then the arguments.
var cookShapes = map[string][]string{
	"Scene":       {"Scene", "--ref", "head", "--chain", "ListNode", "--next", "next"},
	"SceneValued": {"Scene", "--ref", "head", "--chain", "ListNode", "--next", "next", "--values"},
	"Depot":       {"Depot", "--ref", "head", "--chain", "ListNode", "--next", "next"},
	"Album":       {"Album", "--ref", "head", "--chain", "ListNode", "--next", "next"},
	"TreeNode":    {"TreeNode", "--ref", "left", "--chain", "TreeNode", "--next", "left"},
	"ListNode":    {"ListNode", "--ref", "next", "--chain", "ListNode", "--next", "next"},
}

// cookgen writes the cooks the harness materialises for its cook surfaces
// and patches its cook battery over (test/conformance/harness/run.go,
// materialise), one per case, by test/cookgen.
func cookgen(t *testing.T, cases ...string) map[string][]byte {
	t.Helper()
	slowtest.Gate(t, "the Lobster toolchain and the Go cook")
	dir := t.TempDir()
	bin := filepath.Join(dir, "cookgen")
	build := exec.Command("go", "build", "-o", bin, "./test/cookgen")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building cookgen: %v\n%s", err, out)
	}
	out := map[string][]byte{}
	for _, c := range cases {
		shape, ok := cookShapes[c]
		if !ok {
			t.Fatalf("no fixture shape for cook case %s", c)
		}
		file := filepath.Join(dir, c+".cook")
		args := append([]string{"--bytes", "4096", "--root", shape[0], "--out", file}, shape[1:]...)
		cmd := exec.Command(bin, args...)
		cmd.Dir = root
		if msg, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("cookgen %s: %v\n%s", c, err, msg)
		}
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		out[c] = data
	}
	return out
}

func cookForgeryRun(t *testing.T, sabotage func(string) string) ([]forgeryRow, map[string]string) {
	t.Helper()
	units, _ := readManifest(t)
	paths, err := compiler.GatherPaths(prefix(units["graphdemo"]))
	if err != nil {
		t.Fatal(err)
	}
	g, err := generateLoadUnit(t, paths)
	if err != nil {
		t.Fatalf("graphdemo is refused: %v", err)
	}
	if sabotage != nil {
		p := filepath.Join(g.dir, "cook.lobster")
		src, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(sabotage(string(src))), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	scene := cookgen(t, "Scene")["Scene"]
	rows := forgeryRows(t, "cook")
	if len(rows) == 0 {
		t.Fatal("the manifest names no cook forgery: the comparison is vacuous")
	}
	datas := make([][]byte, len(rows))
	for i, r := range rows {
		if r.base != "Scene" {
			t.Fatalf("%s: base %s, want the Scene cook this test generates", r.name, r.base)
		}
		datas[i] = forge(t, r, scene)
	}
	got := openReasons(t, g.dir, append(g.mods, "cook"), rows, func(r forgeryRow) string {
		return fmt.Sprintf("schema_rt.cook_open_named(%q, DATA, LEAD, NOBUF)", r.subject)
	}, datas)
	return rows, got
}

// TestLobsterCookForgeries opens the manifest's cook battery — the reference's
// 111 and the rows after them — through the cooked form's Open
// (docs/SPEC-TABLES.md §7, §7.1) over the Scene cook the harness patches, and
// holds each to its verdict and, where a `refusal` line names one, its first
// failing clause.
func TestLobsterCookForgeries(t *testing.T) {
	t.Parallel()
	rows, got := cookForgeryRun(t, nil)
	if bad := checkReasons(rows, got); len(bad) > 0 {
		t.Fatalf("cook forgeries that disagree with the manifest: %v", bad)
	}
	t.Logf("%d cook forgeries answer the manifest's verdict and reason", len(rows))
}

// TestLobsterCookForgeriesNegativeControl removes the reserved-word clause and
// requires exactly the three reserved-word rows to go red.
func TestLobsterCookForgeriesNegativeControl(t *testing.T) {
	t.Parallel()
	const reserved = "    if read_uint64_le(data, 48) != 0 or read_uint64_le(data, 56) != 0:\n        return \"reserved_not_zero\"\n"
	rows, got := cookForgeryRun(t, func(src string) string {
		if !strings.Contains(src, reserved) {
			t.Fatal("the control's anchor, the reserved-word clause, is not in cook.lobster")
		}
		return strings.Replace(src, reserved, "", 1)
	})
	bad := checkReasons(rows, got)
	want := "[cook_reserved0_one cook_reserved1_one cook_reserved0_saturated]"
	if fmt.Sprint(bad) != want {
		t.Fatalf("with the reserved-word clause removed, the rows that went red are %v, want exactly %s", bad, want)
	}
	t.Logf("negative control: the reserved-word clause removed, exactly %s went red", want)
}

// cookDumpRun answers THE CANONICAL NODE DUMP (test/conformance/go/cooks.go)
// of every `cook` case of the manifest and returns the cases whose dump is
// not the pinned one.
func cookDumpRun(t *testing.T, sabotage func(string) string) []string {
	t.Helper()
	units, _ := readManifest(t)
	paths, err := compiler.GatherPaths(prefix(units["graphdemo"]))
	if err != nil {
		t.Fatal(err)
	}
	g, err := generateLoadUnit(t, paths)
	if err != nil {
		t.Fatalf("graphdemo is refused: %v", err)
	}
	if sabotage != nil {
		p := filepath.Join(g.dir, "cook.lobster")
		src, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(sabotage(string(src))), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cases := []string{"Scene", "Depot", "Album", "TreeNode", "ListNode", "SceneValued"}
	cooks := cookgen(t, cases...)
	// the dump is compared as hex lines, one case a line, so its newlines ride
	var p strings.Builder
	for _, m := range append(g.mods, "cook") {
		fmt.Fprintf(&p, "import .%s\n", m)
	}
	p.WriteString("def hx(s:string) -> string:\n    var o = \"\"\n    for(s) c:\n        o += number_to_string(c, 16, 2)\n    return lowercase(o)\n")
	inputs := map[string][]byte{}
	for i, c := range cases {
		inputs[c] = cooks[c]
		fmt.Fprintf(&p, "let d%d = read_file(%q)\nassert d%d\n", i, c, i)
		fmt.Fprintf(&p, "let dump%d, why%d = schema_rt.cook_dump(d%d, %q)\n", i, i, i, cookShapes[c][0])
		fmt.Fprintf(&p, "print(%q + hx(dump%d))\n", c+" ", i)
	}
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(p.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimSpace(string(lobsterrun.Run(t, g.dir, "main.lobster", inputs))), "\n") {
		name, h, _ := strings.Cut(line, " ")
		got[name] = h
	}
	var bad []string
	for _, c := range cases {
		want, err := os.ReadFile(filepath.Join(root, "testdata", "conformance", "tables", "cook", c+".dump"))
		if err != nil {
			t.Fatal(err)
		}
		if got[c] != fmt.Sprintf("%x", want) {
			bad = append(bad, c)
		}
	}
	return bad
}

// TestLobsterCookDump walks every manifest cook through its own derefs and
// holds the node dump to the pinned one byte for byte (§7, §7.5).
func TestLobsterCookDump(t *testing.T) {
	t.Parallel()
	if bad := cookDumpRun(t, nil); len(bad) > 0 {
		t.Fatalf("cook dumps that are not the pinned ones: %v", bad)
	}
}

// TestLobsterCookDumpNegativeControl drops the count companion's line and
// requires exactly the two cases with a counted array, Scene and SceneValued,
// to go red.
func TestLobsterCookDumpNegativeControl(t *testing.T) {
	t.Parallel()
	const count = "    if cf.count >= 0 and s != \"str\" and s != \"text\" and s != \"bytes\":\n        cd_emit(w, name + \"#count\", \"{used}\")\n"
	bad := cookDumpRun(t, func(src string) string {
		if !strings.Contains(src, count) {
			t.Fatal("the control's anchor, the #count line, is not in cook.lobster")
		}
		return strings.Replace(src, count, "", 1)
	})
	if fmt.Sprint(bad) != "[Scene SceneValued]" {
		t.Fatalf("with the #count line dropped, the cases that went red are %v, want exactly [Scene SceneValued]", bad)
	}
}
