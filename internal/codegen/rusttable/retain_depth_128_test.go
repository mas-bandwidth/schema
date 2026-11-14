package rusttable

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

func TestRustRetainKeepsDepth128(t *testing.T) {
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
	model := tabletext.NewModel(u)
	scene := model.Lookup("Scene")
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate graphdemo: %v", err)
	}
	framingDir := filepath.Join(root, "testdata", "wire", "tables", "framing")
	read := func(name string) []byte {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(framingDir, name+".bin"))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		return data
	}

	reads := []string{"depth_tables_128", "depth_arrays_128", "depth_union_tables_64", "depth_keyed_128"}
	refused := []string{"depth_tables_129", "depth_arrays_129", "depth_union_tables_65", "depth_keyed_129", "depth_tables_5000"}
	for _, name := range reads {
		var report tabletext.Report
		ok, err := tablewire.Decode(model, model.New(scene), read(name), &report)
		if !ok || report.Unknown != 1 {
			t.Errorf("engine %s: ok=%v err=%v unknown=%d, want ok=true unknown=1", name, ok, err, report.Unknown)
		}
	}
	for _, name := range refused {
		var report tabletext.Report
		ok, err := tablewire.Decode(model, model.New(scene), read(name), &report)
		if ok {
			t.Errorf("engine %s: accepted a depth the binary bound refuses (err=%v)", name, err)
		}
	}
	var body strings.Builder
	body.WriteString(`
    let mut failed = false;
    let mut check = |ok: bool, what: &str| {
        if !ok { println!("FAIL: {what}"); failed = true; }
    };
`)
	for i, name := range reads {
		wire := read(name)
		fmt.Fprintf(&body, "    let wire_%d: [u8; %d] = [%s];\n", i, len(wire), rustBytes(wire))
		fmt.Fprintf(&body, `
    {
        let mut plain_report = TableReport::default();
        let plain = scene_load(&wire_%d, &mut plain_report);
        if plain.is_none() { println!("PLAIN_REPORT %s: {:?}", plain_report); }
        check(plain.is_some(), "%s: strict load refused a readable depth");
        let mut report = TableReport::default();
        let mut retain = TableRetain { bytes: Vec::with_capacity(1 << 20), ids: Vec::with_capacity(1 << 17) };
        let value = scene_load_retain(&wire_%d, &mut retain, &mut report);
        if value.is_none() { println!("REPORT %s: {:?}", report); }
        check(value.is_some(), "%s: retaining load refused a readable depth");
        check(report.retain_lost == 0, "%s: retaining load lost an unknown field");
        if let Some(value) = value {
            let saved = scene_save_retain(&value, &mut retain, &mut report);
            check(saved == wire_%d, "%s: retained save changed the file bytes");
            check(report.retain_lost == 0, "%s: retaining save lost an unknown field");
        }
    }
`, i, name, name, i, name, name, name, i, name, name)
	}
	for i, name := range refused {
		wire := read(name)
		fmt.Fprintf(&body, "    let refused_%d: [u8; %d] = [%s];\n", i, len(wire), rustBytes(wire))
		fmt.Fprintf(&body, `
    {
        let mut report = TableReport::default();
        let mut retain = TableRetain { bytes: Vec::with_capacity(1 << 20), ids: Vec::with_capacity(1 << 17) };
        let value = scene_load_retain(&refused_%d, &mut retain, &mut report);
        check(value.is_none(), "%s: retaining load accepted a refused depth");
    }
`, i, name)
	}
	body.WriteString("    if failed { std::process::exit(1); }\n    println!(\"RETAIN_DEPTH_128 ok\");\n")
	out, err := runRustGeneratedFilesProbe(t, files, body.String())
	if err != nil {
		t.Fatalf("Rust retaining depth probe failed: %v\n%s", err, out)
	}
}
