package goosetable_test

// A TABLE VALUE WHOSE POINTERS FORM A LONG CHAIN IS A LAWFUL VALUE
// (docs/SPEC-TABLES.md §3.1): the node table is flat, so a chain is not a
// depth. Load, save, text and release walk it in bounded stack and in time
// linear in the node count. The control is the manifest's node_chain_two.

import (
	"bytes"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose/gooserun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goosetable"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// chainDeadline bounds the whole Goose run, as a functional test's own wall
// clock does; gooserun.Command still bounds the process it starts.
const chainDeadline = 120 * time.Second

const longChainProbe = `import os;
import .conformance_unit;
import .wire_load;
import .wire_save;
import .json_value;

fn exercise(name: const u8[:], root: const u8[:], want_json: bool) {
    var data: u8[>..] = [];
    if !read_file(str(name, ".bin"), data) {
        write_stderr(str("probe: cannot read ", name, "\n"));
        exit(1);
    }
    var canonical: u8[>..] = [];
    if !read_file(str(name, "_canonical.bin"), canonical) {
        write_stderr(str("probe: cannot read ", name, " canonical\n"));
        exit(1);
    }
    var vals: schema_rt::LoadVal[>..] = [];
    var nodes: schema_rt::LoadNode[>..] = [];
    var report = schema_rt::LoadReport { .. };
    let at = schema_rt::load_table(conformance_unit(), root, data, vals, nodes, report);
    if at < 0 || report.refused || report.damaged {
        print(name, " load 0");
        exit(2);
    }
    print(name, " load 1");
    var out: u8[>..<] = [];
    let refusal = schema_rt::save_table(conformance_unit(), root, data, vals, nodes, at, out);
    if refusal != 0 || out != canonical {
        print(name, " save 0 ", refusal);
        exit(3);
    }
    print(name, " save 1");
    var text: u8[>..] = [];
    let n = schema_rt::json_save_table(conformance_unit(), conformance_text(), root, data, vals, nodes, at, text);
    if (n >= 0) != want_json {
        print(name, " json 0 ", n);
        exit(4);
    }
    print(name, " json 1");
}

fn main() {
    exercise("node_chain_two", "ListNode", true);
    exercise("node_chain_200000", "Scene", false);
    print("released");
}
`

func TestFunctionalGooseLongPointerChain(t *testing.T) {
	t.Parallel()
	c := readWireCorpus(t)
	files := wireUnitFiles(t, "graphdemo", c.units["graphdemo"], nil)
	gathered, err := compiler.GatherPaths(prefix(c.units["graphdemo"]))
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(gathered)
	if err != nil {
		t.Fatalf("graphdemo does not load: %v", err)
	}
	glue := string(files["conformance_unit.goose"])
	glue += "fn conformance_text() -> schema_rt::JsonUnit { return " + u.Package + "::" + goosetable.TextUnitName + "; }\n"
	files["conformance_unit.goose"] = []byte(glue)
	files["main.goose"] = []byte(longChainProbe)
	// The framing vectors are read pins, not canonical saves (an id the value
	// never names still rides in their id table), so the engine's own save is
	// the oracle the leg's save is held to, as the other legs hold it.
	m := tabletext.NewModel(u)
	for name, rootName := range map[string]string{"node_chain_two": "ListNode", "node_chain_200000": "Scene"} {
		b, err := os.ReadFile(filepath.Join(root, "testdata", "wire", "tables", "framing", name+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		files[name+".bin"] = b
		inst := m.New(m.Lookup(rootName))
		var report tabletext.Report
		ok, err := tablewire.Decode(m, inst, b, &report)
		if err != nil || !ok {
			t.Fatalf("%s: the engine refused the pinned vector: ok=%t err=%v report=%+v", name, ok, err, report)
		}
		canonical, err := tablewire.Encode(m, inst)
		if err != nil {
			t.Fatalf("%s: the engine could not save the pinned vector: %v", name, err)
		}
		files[name+"_canonical.bin"] = canonical
	}
	goose, why := gooserun.Compiler()
	if goose == "" {
		t.Fatalf("gooserun: the Goose compiler does not resolve: %s", why)
	}
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := gooserun.Command(t, goose, "run", "main.goose")
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	var expired atomic.Bool
	timer := time.AfterFunc(chainDeadline, func() {
		expired.Store(true)
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	})
	defer timer.Stop()
	err = cmd.Run()
	if expired.Load() {
		t.Fatalf("long pointer chain exceeded %s\nstdout:\n%s\nstderr:\n%s", chainDeadline, stdout.Bytes(), stderr.Bytes())
	}
	if err != nil {
		t.Fatalf("long pointer chain probe failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.Bytes(), stderr.Bytes())
	}
	want := "node_chain_two load 1\nnode_chain_two save 1\nnode_chain_two json 1\n" +
		"node_chain_200000 load 1\nnode_chain_200000 save 1\nnode_chain_200000 json 1\nreleased\n"
	if string(stdout.Bytes()) != want {
		t.Fatalf("long pointer chain probe printed\n%s\nwant\n%s", stdout.Bytes(), want)
	}
}
