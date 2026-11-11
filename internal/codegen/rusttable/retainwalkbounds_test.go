package rusttable

import (
	"bytes"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// THE RETAINING WALK'S LENGTH BOUNDS (docs/SPEC-TABLES.md §5, §6.6): every
// length it reads is compared against the bytes left by subtraction, and an
// element loop never runs past the bytes its frame still holds. The strict
// walk in front of it already answers these files, so the retaining read must
// answer the same verdict instead of walking the same bytes again: a refusal,
// never a hang and never a panic. A well-formed unknown field is still
// retained and its retained save is byte-exact.
//
// A FUNCTIONAL TEST: it compiles and runs Rust with rustc, in release (-O) and
// with overflow checks enabled. It skips by name when rustc is absent and
// SCHEMA_REQUIRE_CORPUS is unset, and fails by name when it is set.
const retainWalkProbe = `
mod variable_runtime;
mod table_enums;
mod probe_table;
use probe_table::*;
use variable_runtime::*;
#[allow(unused_imports)]
use table_enums::*;

fn main() {
    let mode = std::env::args().nth(1).unwrap();
    let path = std::env::args().nth(2).unwrap();
    let wire = std::fs::read(&path).unwrap();
    let mut report = TableReport::default();
    if mode == "plain" {
        match card_load(&wire, &mut report) {
            Some(_) => println!("LOADED unknown={}", report.unknown),
            None => println!("REFUSED"),
        }
        return;
    }
    let mut retain = TableRetain { bytes: Vec::with_capacity(1 << 20), ids: Vec::with_capacity(1 << 17), nodes: Vec::new() };
    match card_load_retain(&wire, &mut retain, &mut report) {
        Some(v) => {
            println!("LOADED unknown={} retained={} retain_lost={}", report.unknown, report.retained, report.retain_lost);
            if let Some(out_path) = std::env::args().nth(3) {
                let saved = card_save_retain(&v, &mut retain, &mut report);
                std::fs::write(&out_path, &saved).unwrap();
                println!("SAVED len={}", saved.len());
            }
        }
        None => println!("REFUSED"),
    }
}
`

func TestRustRetainWalkLengthBounds(t *testing.T) {
	slowtest.Gate(t, "rustc (it builds and runs the generated Rust)")
	t.Parallel()
	if _, err := exec.LookPath("rustc"); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and rustc is not on PATH, so the retaining walk's bounds cannot be built or run: %v", err)
		}
		t.Skipf("rustc is not on PATH and SCHEMA_REQUIRE_CORPUS is unset: this is a functional test that compiles and runs Rust: %v", err)
	}

	schema := testdataCards(t, "older.schema")
	release := buildRetainWalkProbe(t, schema, false)
	debug := buildRetainWalkProbe(t, schema, true)
	profiles := []struct {
		name string
		bin  string
	}{{"release", release}, {"debug", debug}}

	dir := t.TempDir()
	hostile := []struct {
		name string
		wire string
	}{
		{
			// an unknown array of text elements: a claimed count near 2^63 and
			// an element length near 2^64
			name: "array-element-length",
			wire: "01010e150c80808080808080808001f6ffffffffffffffff010088776655443322110100000000000000",
		},
		{
			// an unknown framed field: a length near 2^64
			name: "framed-length",
			wire: "01010d0c010dffffffffffffffffff010088776655443322110100000000000000",
		},
	}
	for _, h := range hostile {
		raw, err := hex.DecodeString(h.wire)
		if err != nil {
			t.Fatalf("%s: %v", h.name, err)
		}
		path := filepath.Join(dir, h.name+".wire")
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		for _, p := range profiles {
			plain := runRetainWalkProbe(t, p.bin, "plain", path, "")
			retain := runRetainWalkProbe(t, p.bin, "retain", path, "")
			if plain != "REFUSED" {
				t.Errorf("%s/%s: the strict read did not refuse the file: %q", h.name, p.name, plain)
			}
			if retain != "REFUSED" {
				t.Errorf("%s/%s: the retaining read did not refuse the file: %q", h.name, p.name, retain)
			}
			if plain != retain {
				t.Errorf("%s/%s: the two reads disagree: strict %q retaining %q", h.name, p.name, plain, retain)
			}
		}
	}

	// THE CONTROL: a well-formed unknown field the retaining read keeps, and
	// the retaining save writes back byte-exact. The Go leg's retained save is
	// the canonical file the Rust save must reproduce.
	olderSrc, err := os.ReadFile(schema)
	if err != nil {
		t.Fatal(err)
	}
	model := tabletext.NewModel(unitFrom(t, string(olderSrc)))
	base, err := tablewire.Encode(model, model.New(model.Lookup("Card")))
	if err != nil {
		t.Fatalf("the control's base file did not encode: %v", err)
	}
	planted := plantUnknown(t, base, "")
	control := goRetainedSave(t, planted)
	controlPath := filepath.Join(dir, "control.wire")
	if err := os.WriteFile(controlPath, control, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, p := range profiles {
		plain := runRetainWalkProbe(t, p.bin, "plain", controlPath, "")
		if plain != "LOADED unknown=1" {
			t.Errorf("control/%s: the strict read did not keep the unknown field: %q", p.name, plain)
		}
		savedPath := filepath.Join(dir, "control-"+p.name+".wire")
		text := runRetainWalkProbe(t, p.bin, "retain", controlPath, savedPath)
		if !strings.HasPrefix(text, "LOADED unknown=1 retained=1 ") {
			t.Errorf("control/%s: the retaining read did not keep the unknown field: %q", p.name, text)
		}
		saved, err := os.ReadFile(savedPath)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(saved, control) {
			t.Errorf("control/%s: the retaining save is not byte-exact:\n got: %x\nwant: %x", p.name, saved, control)
		}
	}
}

func buildRetainWalkProbe(t *testing.T, schemaPath string, debug bool) string {
	t.Helper()
	slowtest.Gate(t, "rustc (it builds the generated Rust)")

	src, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatal(err)
	}
	modules, err := Generate(unitFrom(t, string(src)))
	if err != nil {
		t.Fatalf("generate %s: %v", schemaPath, err)
	}
	root := t.TempDir()
	for _, name := range []string{"variable_runtime.rs", "probe_table.rs"} {
		data, ok := modules[name]
		if !ok {
			t.Fatalf("missing module %s; got %v", name, keys(modules))
		}
		if err := os.WriteFile(filepath.Join(root, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "table_enums.rs"), modules["table_enums.rs"], 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.rs"), []byte(retainWalkProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(root, "probe")
	args := []string{"--edition", "2024"}
	if debug {
		args = append(args, "-C", "overflow-checks=on")
	} else {
		args = append(args, "-O")
	}
	args = append(args, "-o", bin, "main.rs")
	cmd := exec.Command("rustc", args...)
	cmd.Dir = root
	cmd.Env = os.Environ()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the retaining probe did not build (debug=%v): %v\n%s", debug, err, out)
	}
	return bin
}

func runRetainWalkProbe(t *testing.T, bin, mode, path, savePath string) string {
	t.Helper()
	ctx, cancel := slowtest.ProbeContext(t)
	defer cancel()
	args := []string{mode, path}
	if savePath != "" {
		args = append(args, savePath)
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("%s probe did not finish within the test bound: %v\n%s", mode, ctx.Err(), out)
	}
	if err != nil {
		t.Fatalf("%s probe did not complete: %v\n%s", mode, err, out)
	}
	return strings.TrimSpace(string(out))
}
