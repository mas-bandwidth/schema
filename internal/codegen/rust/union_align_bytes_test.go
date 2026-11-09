package rust

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestUnionThenFixedByteArrayWire writes a struct whose fixed [4]uint8 array
// follows a union field and pins the wire to the SPEC's per-byte layout: the
// union's tag bit is followed immediately by the array bits, with no padding
// between them. It then reads the same bytes back and checks the union and the
// array survive the round trip.
func TestUnionThenFixedByteArrayWire(t *testing.T) {
	slowtest.Gate(t, "cargo and the serialize.rs runtime (it builds and runs the generated crate)")

	const src = `package unionalign

type Small {
    v uint8 | min = 0, max = 127
}

union U {
    a Small
}

type T {
    align
    u    U
    data [4]uint8
}
`
	u := unitFromSource(t, "UnionAlign.schema", src)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}

	serializePath := serializeRSPath(t)
	cargoBin := cargoPath(t)

	dir := t.TempDir()
	srcDir := filepath.Join(dir, "src")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cargoToml := fmt.Sprintf(`[package]
name = "unionalignprobe"
version = "0.0.0"
edition = "2024"

[dependencies]
serialize = { package = "serialize-official", path = %q }
`, serializePath)
	if err := os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte(cargoToml), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(srcDir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// T at u = None is one zero tag bit followed by [1, 2, 3, 4] packed
	// LSB-first (§4.3): the four array bytes straddle bit boundaries, so the
	// five wire bytes are 02 04 06 08 00, not 00 01 02 03 04.
	main := `use serialize::{ReadStream, WriteStream};
use unionalignprobe::*;

fn main() {
    let mut value = T::default();
    value.u = U::None;
    value.data = [1, 2, 3, 4];

    let mut buffer = [0u8; 64];
    let mut ws = WriteStream::new(&mut buffer);
    write_t(&mut ws, &value).expect("write T");
    ws.flush();
    let wire = ws.data();

    let expected: [u8; 5] = [0x02, 0x04, 0x06, 0x08, 0x00];
    assert_eq!(wire, &expected[..], "wire bytes");

    let mut decoded = T::default();
    let mut rs = ReadStream::new(wire, wire.len());
    read_t(&mut rs, &mut decoded).expect("read T");
    assert!(matches!(decoded.u, U::None), "union round trip");
    assert_eq!(decoded.data, [1, 2, 3, 4], "array round trip");
    println!("OK");
}
`
	if err := os.WriteFile(filepath.Join(srcDir, "main.rs"), []byte(main), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(cargoBin, "run", "--quiet")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "PATH="+filepath.Dir(cargoBin)+":"+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("generated crate failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "OK") {
		t.Fatalf("probe output: %s", out)
	}
}

// serializeRSPath finds the sibling serialize.rs runtime the generated code
// targets: $SERIALIZE_RS first, then the sibling checkout, then a local copy.
func serializeRSPath(t *testing.T) string {
	t.Helper()
	if p := os.Getenv("SERIALIZE_RS"); p != "" {
		if fi, err := os.Stat(filepath.Join(p, "Cargo.toml")); err == nil && !fi.IsDir() {
			abs, _ := filepath.Abs(p)
			return abs
		}
	}
	for _, rel := range []string{
		"../../../../serialize.rs",
		"../../../serialize.rs",
		"../../serialize.rs",
	} {
		p, err := filepath.Abs(rel)
		if err != nil {
			continue
		}
		if fi, err := os.Stat(filepath.Join(p, "Cargo.toml")); err == nil && !fi.IsDir() {
			return p
		}
	}
	t.Skip("no serialize.rs runtime: set SERIALIZE_RS or stage the sibling checkout")
	return ""
}

// cargoPath is the leg's own toolchain: $CARGO, then PATH, then the rustup
// install used by the make targets.
func cargoPath(t *testing.T) string {
	t.Helper()
	if c := os.Getenv("CARGO"); c != "" {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	if c, err := exec.LookPath("cargo"); err == nil {
		return c
	}
	for _, c := range []string{
		"/opt/homebrew/opt/rustup/bin/cargo",
		os.Getenv("HOME") + "/.cargo/bin/cargo",
	} {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	t.Skip("no cargo toolchain")
	return ""
}
