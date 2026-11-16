package pythontable

import (
	"encoding/hex"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpp"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpptable"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestPythonBlockCppIdentity compares live generated C++ and Python builders
// for the same nonempty block extents under both pinned Python interpreters.
func TestPythonBlockCppIdentity(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "generated C++ and both pinned Python interpreters")
	u := loadUnitFiles(t, filepath.Join("..", "..", "..", "tables", "block"))
	files, err := cpp.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := cpptable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	writeTree(t, dir, files)
	source := `#include "RenderBlock.h"
#include "PaddedBlock.h"
#include <cstdio>
#include <cstring>
using namespace blockdemo;
static void hex(const uint8_t *p, int64_t n) { for(int64_t i=0;i<n;i++) std::printf("%02x",p[i]); std::puts(""); }
int main() {
 RenderFrameCounts c{}; c.cameras=1; c.ships=3; c.turrets=2; c.missiles=1; c.dynamic_props=2; c.static_props=4; c.cosmetic_props=1; c.lasers=3; c.explosions=2;
 RenderFrameBlockStorage s; if(!s.Create(TableBlockDefaultAllocator())) return 1; std::memset(s.base,0,RenderFrameBlockMaxBytes);
 RenderFrameBlock b; if(!RenderFrameBlockBegin(b,s,c)) return 2; auto n=RenderFrameBlockBytes(b); RenderFrameBlock r; if(!RenderFrameBlockOpen(r,s.base,n)) return 3; hex((const uint8_t*)s.base,n); s.Destroy();
 PaddedFrameCounts pc{}; pc.rows=3; PaddedFrameBlockStorage ps; if(!ps.Create(TableBlockDefaultAllocator())) return 4; std::memset(ps.base,0,PaddedFrameBlockMaxBytes);
 PaddedFrameBlock pb; if(!PaddedFrameBlockBegin(pb,ps,pc)) return 5; n=PaddedFrameBlockBytes(pb); PaddedFrameBlock pr; if(!PaddedFrameBlockOpen(pr,ps.base,n)) return 6; hex((const uint8_t*)ps.base,n); ps.Destroy(); return 0;
}`
	path := filepath.Join(dir, "probe.cpp")
	if err := os.WriteFile(path, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	ser, err := filepath.Abs("../../../../serialize")
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "probe")
	args := []string{"-std=c++17", "-O1", "-I" + dir, "-I" + ser, path}
	for name := range files {
		if !strings.Contains(name, "/") && strings.HasSuffix(name, ".cpp") {
			args = append(args, filepath.Join(dir, name))
		}
	}
	args = append(args, "-o", bin)
	if out, err := exec.Command("c++", args...).CombinedOutput(); err != nil {
		t.Fatalf("compile C++: %v\n%s", err, out)
	}
	ref, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("run C++: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(ref)), "\n")
	if len(lines) != 2 {
		t.Fatalf("C++ gave %d lines", len(lines))
	}
	for _, line := range lines {
		if _, err := hex.DecodeString(line); err != nil {
			t.Fatal(err)
		}
	}
	py, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	pyDir := t.TempDir()
	writeTree(t, pyDir, py)
	probe := pyrun.ImportPrelude + `import blockdemo as T
from blockdemo._schema import blockbuild as B
for root, counts in [("RenderFrame", {"cameras":1,"ships":3,"turrets":2,"missiles":1,"dynamic_props":2,"static_props":4,"cosmetic_props":1,"lasers":3,"explosions":2}), ("PaddedFrame", {"rows":3})]:
    print(bytes(B.Begin(T, root, counts).bytes).hex())
`
	for _, p := range []struct {
		name string
		path func(testing.TB) string
	}{{"current", pyrun.Interpreter}, {"floor", pyrun.Floor}} {
		out, stderr, err := pyrun.Try(t, p.path(t), pyDir, "main.py", map[string][]byte{"main.py": []byte(probe)})
		if err != nil || out != string(ref) {
			t.Errorf("%s: Python/C++ block bytes differ: %v\n%s", p.name, err, stderr)
		}
	}
}

// TestPythonBlockDriverErrors proves a driver crash cannot masquerade as a
// legitimate block refusal on either interpreter.
func TestPythonBlockDriverErrors(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters")
	source, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "conformance", "python", "surfaces", "_blocks.py"))
	if err != nil {
		t.Fatal(err)
	}
	probe := pyrun.ImportPrelude + `import importlib
import types
import _blocks
class Refused(Exception): pass
pkg = types.SimpleNamespace(__name__="fake", Refused=Refused, BUILD_VERSION=0)
def fail(*args): raise ValueError("broken runtime")
importlib.import_module = lambda name: types.SimpleNamespace(BlockOpen=fail)
for call in (lambda: _blocks.open_block(pkg, "R", b""), lambda: _blocks.open_raw(pkg, "R", b"")):
    try: call()
    except ValueError as err: assert str(err) == "broken runtime"
    else: raise AssertionError("runtime crash was hidden")
print("ok")
`
	for _, p := range []struct {
		name string
		path func(testing.TB) string
	}{{"current", pyrun.Interpreter}, {"floor", pyrun.Floor}} {
		out, stderr, err := pyrun.Try(t, p.path(t), t.TempDir(), "main.py", map[string][]byte{"main.py": []byte(probe), "_blocks.py": source})
		if err != nil || out != "ok\n" {
			t.Errorf("%s: %v %s %s", p.name, err, out, stderr)
		}
	}
}
