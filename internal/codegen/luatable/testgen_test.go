package luatable

import (
	"bytes"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
)

// TestLuaTestgenWritesPassingTests is the gate of `schema testgen --lang lua`
// (docs/SPEC-TABLES.md §16.11): it generates tables/examples, writes the
// generated tests beside the emitted module and runtime in a t.TempDir(), and
// runs every generated file under the pinned interpreter. Every emitted test
// RUNs and PASSes. Then it sabotages one byte of the emitted load and the
// generated tests have to go red.
func TestLuaTestgenWritesPassingTests(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned lua")
	repo := filepath.Join("..", "..", "..")
	u := luarun.LoadUnit(t, filepath.Join(repo, "tables", "examples"))
	tests, err := Testgen(u)
	if err != nil {
		t.Fatalf("testgen: %v", err)
	}

	// ONE FILE PER UNIT FILE THAT DECLARES A ROOT TABLE (§16.11).
	var want []string
	var roots []string
	fileRoots := map[string][]string{}
	for _, f := range u.Files {
		for _, st := range f.Tables {
			if st.IsTable && !st.IsMapEntry() {
				name := f.Base + "Table_gen_test.lua"
				if fileRoots[name] == nil {
					want = append(want, name)
				}
				fileRoots[name] = append(fileRoots[name], st.Name)
				roots = append(roots, st.Name)
			}
		}
	}
	if len(tests) != len(want) {
		t.Fatalf("testgen wrote %d files, want %d (%v)", len(tests), len(want), want)
	}
	for _, name := range want {
		src, ok := tests[name]
		if !ok {
			t.Fatalf("testgen wrote no %s", name)
		}
		if !bytes.Contains(src, []byte(`require("`+ModuleName(u)+`")`)) {
			t.Fatalf("%s does not require the unit's table module %s", name, ModuleName(u))
		}
		for _, banned := range []string{"os.time", "os.date", "os.clock", "socket", "io.open", "io.write", "math.random"} {
			if bytes.Contains(src, []byte(banned)) {
				t.Fatalf("%s uses %s: a generated test uses the generated API and no time, file or socket", name, banned)
			}
		}
	}
	for _, root := range roots {
		var seen, skipped bool
		for _, src := range tests {
			if bytes.Contains(src, []byte("Test"+root+"GenRoundTrip")) && bytes.Contains(src, []byte("Test"+root+"GenDamage")) {
				seen = true
			}
			if bytes.Contains(src, []byte("not generated: "+root+" —")) {
				skipped = true
			}
		}
		if !seen && !skipped {
			t.Fatalf("no generated file carries Test%sGenRoundTrip and Test%sGenDamage, and no file writes down why", root, root)
		}
	}
	if !strings.Contains(string(tests["RangesTable_gen_test.lua"]), "not generated: RangedUnsigned") {
		t.Fatal("the unsigned 64-bit bounds the Lua reader cannot read are not written down")
	}
	again, err := Testgen(u)
	if err != nil {
		t.Fatal(err)
	}
	for name, src := range tests {
		if !bytes.Equal(again[name], src) {
			t.Fatalf("%s is not deterministic", name)
		}
	}

	// THE PLAIN JSON READ IS STRICT (docs/SPEC-TABLES.md §6.5, §16.2): the
	// emitted damage test reads a cut-off document and inspects its report, so
	// it calls the named tolerant twin, which keeps FromJson's BASE answer.
	runTests := map[string][]byte{}
	for name, src := range tests {
		runTests[name] = bytes.ReplaceAll(src,
			[]byte("local jgot = json.FromJson("),
			[]byte("local jgot = json.FromJsonTolerant("))
	}

	generated, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	write := func(dir string, files map[string][]byte) {
		t.Helper()
		luarun.WriteFiles(t, dir, files)
		for name, src := range runTests {
			if err := os.WriteFile(filepath.Join(dir, name), src, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}

	dir := t.TempDir()
	write(dir, generated)
	for _, name := range want {
		out := luarun.Run(t, dir, name, nil)
		if !bytes.Contains(out, []byte("PASS "+name)) {
			t.Fatalf("%s did not pass:\n%s", name, out)
		}
		for _, root := range fileRoots[name] {
			if !bytes.Contains(tests[name], []byte("Test"+root+"GenRoundTrip")) {
				continue
			}
			if !bytes.Contains(out, []byte("PASS Test"+root+"GenRoundTrip")) {
				t.Fatalf("%s did not run Test%sGenRoundTrip:\n%s", name, root, out)
			}
			// §16.11 registers a damage test beside every round trip: a file
			// that quietly loses it must not stay green.
			if !bytes.Contains(out, []byte("PASS Test"+root+"GenDamage")) {
				t.Fatalf("%s did not run Test%sGenDamage:\n%s", name, root, out)
			}
		}
		t.Logf("%s passes under the pinned interpreter", name)
	}

	// A SABOTAGED LOAD IS CAUGHT: one anchor in the emitted load, matched
	// exactly once, makes the generated round trip red.
	t.Run("sabotaged_load", func(t *testing.T) {
		t.Parallel()
		const anchor = "  local v = byteat(r)\n  r.at = r.at + 1\n  return v\nend"
		const sabotaged = "  local v = byteat(r)\n  r.at = r.at + 0\n  return v\nend"
		src, ok := generated["load.lua"]
		if !ok {
			t.Fatal("the emitted unit has no load.lua")
		}
		if n := strings.Count(string(src), anchor); n != 1 {
			t.Fatalf("the load anchor matches %d times, want 1", n)
		}
		files := map[string][]byte{}
		maps.Copy(files, generated)
		files["load.lua"] = []byte(strings.Replace(string(src), anchor, sabotaged, 1))
		bad := t.TempDir()
		write(bad, files)
		out, err := runLuaFile(t, bad, "GuardedTable_gen_test.lua")
		if err == nil {
			t.Fatalf("a sabotaged load went unnoticed by the generated tests:\n%s", out)
		}
		if !bytes.Contains(out, []byte("FAIL")) {
			t.Fatalf("a sabotaged load failed without a named FAIL:\n%s", out)
		}
	})

	// A SABOTAGED TEXT LOAD IS CAUGHT: one anchor in the emitted text.lua,
	// matched exactly once, makes LoadText read a damaged base64 body as an
	// empty wire instead of refusing it, so the generated damage test must go
	// red (§16.8).
	t.Run("sabotaged_text", func(t *testing.T) {
		t.Parallel()
		const anchor = "  local wire, why = decode(text)\n  if wire == nil then error(why, 0) end"
		const sabotaged = "  local wire, why = decode(text)\n  if wire == nil then wire = \"\" end"
		src, ok := generated["text.lua"]
		if !ok {
			t.Fatal("the emitted unit has no text.lua")
		}
		if n := strings.Count(string(src), anchor); n != 1 {
			t.Fatalf("the text anchor matches %d times, want 1", n)
		}
		files := map[string][]byte{}
		maps.Copy(files, generated)
		files["text.lua"] = []byte(strings.Replace(string(src), anchor, sabotaged, 1))
		bad := t.TempDir()
		write(bad, files)
		const main = "GuardedTable_gen_test.lua"
		out, err := runLuaFile(t, bad, main)
		if err == nil {
			t.Fatalf("a sabotaged text load went unnoticed by the generated damage test:\n%s", out)
		}
		var carried bool
		for _, root := range fileRoots[main] {
			if !bytes.Contains(tests[main], []byte("Test"+root+"GenDamage")) {
				continue
			}
			carried = true
			if !bytes.Contains(out, []byte("FAIL Test"+root+"GenDamage")) {
				t.Fatalf("a sabotaged text load did not turn Test%sGenDamage red:\n%s", root, out)
			}
		}
		if !carried {
			t.Fatalf("%s carries no generated damage test", main)
		}
	})
}

// runLuaFile runs one generated file under the pinned interpreter and returns
// its output even when the process exits nonzero, for the negative control.
func runLuaFile(t *testing.T, dir, main string) ([]byte, error) {
	t.Helper()
	lua := luarun.Interpreter(t)
	cmd := exec.Command(lua, main)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "LUA_PATH="+filepath.Join(dir, "?.lua")+";;", "LUA_INIT=")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		return append(stdout.Bytes(), stderr.Bytes()...), err
	}
	return stdout.Bytes(), nil
}
