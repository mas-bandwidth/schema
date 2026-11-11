package rusttable

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

func TestRustNodeTypeReservedIdRefused(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "rustc (it builds and runs the generated Rust)")
	if _, err := exec.LookPath("rustc"); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and rustc is not on PATH: %v", err)
		}
		t.Skipf("rustc is not on PATH: %v", err)
	}
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	u := vcLoadUnit(t, root, "tables/pointers")
	names := []string{
		"node_type_ref_zero",
		"node_type_node_table_id",
		"node_type_build_version_id",
		"node_type_vocabulary_id",
	}
	var constants strings.Builder
	var rows strings.Builder
	for _, name := range names {
		wire, err := os.ReadFile(filepath.Join(root, "testdata", "wire", "tables", "framing", name+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&constants, "const %s: [u8; %d] = [%s];\n", rustConstName(name), len(wire), rustBytes(wire))
		fmt.Fprintf(&rows, "        (\"%s\", &%s[..]),\n", name, rustConstName(name))
	}
	chain, err := os.ReadFile(filepath.Join(root, "testdata", "wire", "tables", "framing", "node_chain_two.bin"))
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintf(&constants, "const NODE_CHAIN_TWO: [u8; %d] = [%s];\n", len(chain), rustBytes(chain))
	body := constants.String() + `
    let mut failed = false;
    for (name, wire) in [
` + rows.String() + `
    ] {
        let mut report = TableReport::default();
        let value = scene_load(wire, &mut report);
        if value.is_some() || !report.damaged {
            println!("FAIL: {name}: load did not refuse reserved node type");
            failed = true;
        }
    }
    let mut report = TableReport::default();
    let value = scene_load(&NODE_CHAIN_TWO, &mut report);
    if report.damaged || value.is_none() {
        println!("FAIL: node_chain_two: valid control did not load");
        failed = true;
    }
    if failed { std::process::exit(1); }
    println!("NODE TYPE RESERVED ok");
`
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	out, err := runRustGeneratedFilesProbe(t, files, body)
	if err != nil {
		t.Fatalf("reserved node type probe failed: %v\n%s", err, out)
	}
}

func rustConstName(name string) string {
	switch name {
	case "node_type_ref_zero":
		return "NODE_TYPE_REF_ZERO"
	case "node_type_node_table_id":
		return "NODE_TYPE_NODE_TABLE_ID"
	case "node_type_build_version_id":
		return "NODE_TYPE_BUILD_VERSION_ID"
	case "node_type_vocabulary_id":
		return "NODE_TYPE_VOCABULARY_ID"
	default:
		panic("unexpected node type vector")
	}
}
