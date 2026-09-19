package rusttable

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/rust"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// accessorDescriptorSchema declares one block (a fixed table) and one cook (a
// variable table with a pointer edge), so both reading tiers are exercised:
// the block's projection record, and the cook's node record with its pointer
// SLOT. It is the same shape the Go reference at
// internal/codegen/gotable/accessor_descriptor_test.go holds.
const accessorDescriptorSchema = `package probe
fixed table Block
{
    id   uint32
    flag bool
}
table Node
{
    value int32
    next  *Node
}
`

// accessorDescriptorProbe is the Rust integration test the crate runs. It reads
// every field of both records TWICE — once through the generated ACCESSOR the
// declaration named, once through the DESCRIPTOR's own offset — and refuses
// when the two spellings of one layout disagree. The pointer slot is held
// separately, because its position is what a self-relative delta is relative
// to (§6.3).
//
// The body is a plain raw string: no Sprintf and therefore no doubled percent
// verbs, and no backtick anywhere inside it.
const accessorDescriptorProbe = `use agreement::*;

#[test]
fn accessor_descriptor_agreement() {
    // ---- the BLOCK projection, accessor against descriptor ----
    let info = BlockBlock::type_info();
    let mut proj = BlockBlockProjection::default();
    proj.id = 0x11223344;
    proj.flag = true;
    let base = &proj as *const BlockBlockProjection as *const u8;
    let mut compared = 0usize;
    for f in info.fields.iter() {
        match f.name {
            "id" => {
                assert_eq!(
                    core::mem::offset_of!(BlockBlockProjection, id),
                    f.offset as usize,
                    "block.id: the accessor's offset is not the descriptor's"
                );
                let via = unsafe {
                    core::ptr::read_unaligned(base.add(f.offset as usize) as *const u32)
                };
                assert_eq!(
                    proj.id,
                    via,
                    "block.id: the accessor and the descriptor disagree about the value"
                );
                compared += 1;
            }
            "flag" => {
                assert_eq!(
                    core::mem::offset_of!(BlockBlockProjection, flag),
                    f.offset as usize,
                    "block.flag: the accessor's offset is not the descriptor's"
                );
                let via = unsafe {
                    core::ptr::read_unaligned(base.add(f.offset as usize) as *const u8)
                };
                assert_eq!(
                    proj.flag,
                    via != 0,
                    "block.flag: the accessor and the descriptor disagree about the value"
                );
                compared += 1;
            }
            _ => {}
        }
    }
    assert_eq!(
        compared,
        info.num_fields as usize,
        "block: the accessor walk compared {} fields but the descriptor declares {}",
        compared,
        info.num_fields
    );

    // ---- the COOK node, accessor against descriptor, and its pointer SLOT ----
    let cinfo = node_cook_info();
    let mut row = NodeRow::default();
    row.value = 7;
    row.next = 0;
    let cbase = &row as *const NodeRow as *const u8;
    let mut ccompared = 0usize;
    for f in cinfo.fields.iter() {
        match f.name {
            "value" => {
                assert_eq!(
                    core::mem::offset_of!(NodeRow, value),
                    f.offset as usize,
                    "cook.value: the accessor's offset is not the descriptor's"
                );
                let via = unsafe {
                    core::ptr::read_unaligned(cbase.add(f.offset as usize) as *const i32)
                };
                assert_eq!(
                    row.value,
                    via,
                    "cook.value: the accessor and the descriptor disagree about the value"
                );
                ccompared += 1;
            }
            "next" => {
                assert_eq!(
                    core::mem::offset_of!(NodeRow, next),
                    f.offset as usize,
                    "cook.next: the slot accessor's offset is not the descriptor's"
                );
                let via = unsafe {
                    core::ptr::read_unaligned(cbase.add(f.offset as usize) as *const i64)
                };
                assert_eq!(
                    row.next,
                    via,
                    "cook.next: the accessor and the descriptor disagree about the delta"
                );
                ccompared += 1;
            }
            _ => {}
        }
    }
    assert_eq!(
        ccompared,
        cinfo.num_fields as usize,
        "cook: the accessor walk compared {} fields but the descriptor declares {}",
        ccompared,
        cinfo.num_fields
    );
}
`

// TestAccessorDescriptorAgreement is the Rust half of the J1 technique
// (docs/PORTING.md, schema#421). The generated ACCESSOR and the generated
// DESCRIPTOR are two independent derivations of one layout, and a reading tier
// that only ever walks the descriptors could read the descriptors twice and
// never know. This reads both ways and requires agreement, so a moved accessor
// or a moved descriptor offset is seen without a pinned dump that happens to
// cover the field.
//
// The crate composition MIRRORS compiler/target_rust.go (rust.Generate, then
// rusttable.Generate merged in, then rust.Lib) rather than calling it — a
// cycle: the compiler package imports rusttable, so rusttable cannot import
// compiler back.
func TestAccessorDescriptorAgreement(t *testing.T) {
	if os.Getenv("SCHEMA_RUST_CRATE") == "" {
		t.Skipf("SCHEMA_RUST_CRATE is not set; the crate agreement probe is a gate run by make, not a plain go test sweep")
	}

	u := unitFrom(t, accessorDescriptorSchema)

	files, err := rust.Generate(u)
	if err != nil {
		t.Fatalf("rust.Generate: %v", err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatalf("rusttable.Generate: %v", err)
	}
	for name, data := range tables {
		if _, dup := files[name]; dup {
			t.Fatalf("generated file %s is claimed twice", name)
		}
		files[name] = data
	}
	lib, err := rust.Lib(u, Modules(tables))
	if err != nil {
		t.Fatalf("rust.Lib: %v", err)
	}
	files["lib.rs"] = lib

	serializePath := locateSerialize(t)
	cargoBin := locateCargo(t)

	dir := t.TempDir()
	srcDir := filepath.Join(dir, "src")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cargoToml := fmt.Sprintf(`[package]
name = "agreement"
version = "0.0.0"
edition = "2024"

[features]
default = ["block", "cook"]
block = []
cook = []

[dependencies]
%s`, serializeDep(serializePath))
	if err := os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte(cargoToml), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(srcDir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	testsDir := filepath.Join(dir, "tests")
	if err := os.MkdirAll(testsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(testsDir, "agreement.rs"), []byte(accessorDescriptorProbe), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(cargoBin, "test", "--quiet")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "PATH="+filepath.Dir(cargoBin)+":"+os.Getenv("PATH"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the generated accessor/descriptor agreement crate does not pass its probe: %v\n%s", err, out)
	}

	blockFields := 0
	if blocks := ir.Blocks(u); blocks != nil {
		for _, bl := range blocks.Tables {
			blockFields += len(bl.Projection.Fields)
		}
	}
	cookFields := 0
	for name := range ir.VariableTables(u) {
		if st := u.Tables[name]; st != nil {
			cookFields += len(st.Fields)
		}
	}
	t.Logf("accessor/descriptor agreement compared %d fields (%d block, %d cook)", blockFields+cookFields, blockFields, cookFields)
}

// locateSerialize finds the serialize.rs runtime the generated Rust targets. A
// SCHEMA_RUST_CRATE run is a gate, so a missing runtime is a hard failure, not
// a silent pass.
func locateSerialize(t *testing.T) string {
	t.Helper()
	var path string
	if p := os.Getenv("SERIALIZE_RS"); p != "" {
		if fi, err := os.Stat(filepath.Join(p, "Cargo.toml")); err == nil && !fi.IsDir() {
			path, _ = filepath.Abs(p)
		}
	}
	if path == "" {
		for _, rel := range []string{
			"../../../../serialize.rs",
			"../../../../../serialize.rs",
			"../../../serialize.rs",
			"../../serialize.rs",
		} {
			p, err := filepath.Abs(rel)
			if err != nil {
				continue
			}
			if fi, err := os.Stat(filepath.Join(p, "Cargo.toml")); err == nil && !fi.IsDir() {
				path = p
				break
			}
		}
	}
	if path == "" {
		t.Fatalf("SCHEMA_RUST_CRATE=1 is set but serialize.rs was not found (set SERIALIZE_RS to its checkout)")
	}
	return path
}

// locateCargo finds cargo on PATH or in the rustup keg. A SCHEMA_RUST_CRATE
// run is a gate, so a missing toolchain is a hard failure, not a silent pass.
func locateCargo(t *testing.T) string {
	t.Helper()
	bin, err := exec.LookPath("cargo")
	if err != nil {
		if fi, err2 := os.Stat("/opt/homebrew/opt/rustup/bin/cargo"); err2 == nil && !fi.IsDir() {
			bin = "/opt/homebrew/opt/rustup/bin/cargo"
		}
	}
	if bin == "" {
		t.Fatalf("SCHEMA_RUST_CRATE=1 is set but cargo was not found on PATH or in the rustup keg")
	}
	return bin
}

// serializeDep writes the [dependencies] line naming the located serialize.rs
// checkout. The generated Rust refers to the crate as serialize, so the key
// is always serialize; the package rename is only spelled out when the
// checkout's own package name differs from that key.
func serializeDep(path string) string {
	name := "serialize"
	if data, err := os.ReadFile(filepath.Join(path, "Cargo.toml")); err == nil {
		s := string(data)
		if i := strings.Index(s, "[package]"); i >= 0 {
			rest := s[i:]
			if j := strings.Index(rest, "name = \""); j >= 0 {
				k := j + len("name = \"")
				if m := strings.Index(rest[k:], "\""); m >= 0 {
					name = rest[k : k+m]
				}
			}
		}
	}
	if name == "serialize" {
		return fmt.Sprintf("serialize = { path = %q }\n", path)
	}
	return fmt.Sprintf("serialize = { package = %q, path = %q }\n", name, path)
}
