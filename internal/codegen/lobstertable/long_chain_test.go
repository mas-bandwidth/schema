package lobstertable_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// A CHAIN IS NOT A DEPTH (docs/SPEC-TABLES.md §3.1; docs/SECURITY.md): a legal
// file whose declared pointers form a chain of any length the file carries
// loads, saves byte for byte and releases in bounded stack on the leg's
// default stack, in time linear in the node count. The text form carries its
// own depth cap (§16.7), so the JSON write of a long chain is refused rather
// than written, and the refusal too must cost bounded stack. node_chain_two is
// the small control: it loads, saves to the engine's canonical bytes, and
// writes JSON.
func TestFunctionalLobsterLongPointerChain(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Lobster toolchain")
	framing := filepath.Join(root, "testdata", "wire", "tables", "framing")
	paths, err := compiler.GatherPaths(prefix([]string{"tables/pointers"}))
	if err != nil {
		t.Fatal(err)
	}
	g, err := generateLoadUnit(t, paths)
	if err != nil {
		t.Fatalf("graphdemo is refused: %v", err)
	}
	// each file beside the engine's save of the value it reads: a leg's save
	// is the engine's canonical bytes, which a hand-built file need not be
	m := tabletext.NewModel(g.u)
	inputs := map[string][]byte{}
	for _, c := range []struct{ name, root string }{{"node_chain_two", "ListNode"}, {"node_chain_200000", "Scene"}} {
		data, err := os.ReadFile(filepath.Join(framing, c.name+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		inst := m.New(m.Lookup(c.root))
		var report tabletext.Report
		if ok, err := tablewire.Decode(m, inst, data, &report); err != nil || !ok {
			t.Fatalf("the engine refused %s: ok=%t err=%v", c.name, ok, err)
		}
		canonical, err := tablewire.Encode(m, inst)
		if err != nil {
			t.Fatalf("the engine could not save %s: %v", c.name, err)
		}
		inputs[c.name] = data
		inputs[c.name+"_canonical"] = canonical
	}
	var b strings.Builder
	for _, m := range g.mods {
		fmt.Fprintf(&b, "import .%s\n", m)
	}
	b.WriteString(longChainProbe)
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, data := range inputs {
		if err := os.WriteFile(filepath.Join(g.dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	lobster, why := lobsterrun.Interpreter()
	if lobster == "" {
		t.Fatalf("the Lobster toolchain does not resolve: %s", why)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, lobster, "main.lobster")
	cmd.Dir = g.dir
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("long pointer chain exceeded 120 s: %v\n%s", ctx.Err(), out)
	}
	if err != nil {
		t.Fatalf("long pointer chain probe failed: %v\n%s", err, out)
	}
	for _, want := range []string{
		"node_chain_two loaded saved json released",
		"node_chain_200000 loaded",
		"node_chain_200000 saved",
		"node_chain_200000 json refused",
		"node_chain_200000 released",
	} {
		if !strings.Contains(string(out), want+"\n") {
			t.Fatalf("long pointer chain probe did not reach %q:\n%s", want, out)
		}
	}
	if strings.Contains(string(out), "LEAKS FOUND") {
		t.Fatalf("long pointer chain leaked records:\n%s", out)
	}
}

const longChainProbe = `import .load
import .schema_save
import .json
import .strict_and_refusals

def exercise():
    let control = read_file("node_chain_two")
    assert control
    let crep = schema_rt.LoadReport {}
    let cv, cwhy = schema_rt.load_strict("ListNode", control, crep)
    assert cv and cwhy == ""
    assert schema_rt.save(cv) == read_file("node_chain_two_canonical")
    let ctext, cjwhy = schema_rt.json_write(cv)
    assert cjwhy == "" and length(ctext) > 0
    schema_rt.release(cv)
    print("node_chain_two loaded saved json released")

    let data = read_file("node_chain_200000")
    assert data
    let rep = schema_rt.LoadReport {}
    let value, why = schema_rt.load_strict("Scene", data, rep)
    if not value:
        print("node_chain_200000 refused: " + why)
        return
    print("node_chain_200000 loaded")
    if schema_rt.save(value) != read_file("node_chain_200000_canonical"):
        print("node_chain_200000 save differs from the engine save")
        return
    print("node_chain_200000 saved")
    let text, jwhy = schema_rt.json_write(value)
    if jwhy == "" or length(text) != 0:
        print("node_chain_200000 json written past the depth cap")
        return
    print("node_chain_200000 json refused")
    schema_rt.release(value)
    print("node_chain_200000 released")

exercise()
`
