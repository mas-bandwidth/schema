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

// TestPacketFiniteRust is the Rust leg of the packet finite rule (SPEC §4.3,
// serialize format 2.0), over the shared test/packet-finite/Finite.schema.
// Flat holds three fields, so the emitter packs it as one flat run and places
// the float bits itself; Single32 and Single64 keep the per-field form and
// reach serialize.rs's own float calls. Both forms must give one verdict: every
// NaN and infinity pattern is refused on read with NonFiniteFloat, negative
// zero, a subnormal and the largest finite value read back bit exact, and a
// non-finite write is the debug assertion the runtime's write is.
func TestPacketFiniteRust(t *testing.T) {
	slowtest.Gate(t, "cargo and the serialize.rs runtime (it builds and runs the generated crate)")

	src, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "packet-finite", "Finite.schema"))
	if err != nil {
		t.Fatal(err)
	}
	u := unitFromSource(t, "Finite.schema", string(src))
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
name = "packetfinite"
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
	if err := os.WriteFile(filepath.Join(srcDir, "main.rs"), []byte(packetFiniteRustMain), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(cargoBin, "run", "--quiet")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "PATH="+filepath.Dir(cargoBin)+":"+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the finite checker failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "packet finite: OK") {
		t.Fatalf("the finite checker did not report:\n%s", out)
	}
}

// The wire is LSB first (SPEC §4.3): Flat is a's 32 bits, b's 64, then c, so
// a is bytes 0..4 and b bytes 4..12, little endian; a single is its own bytes.
// The layout is checked against the generated writer before it is used.
const packetFiniteRustMain = `use packetfinite::*;
use serialize::{ReadStream, WriteStream};

const F32_BAD: [u32; 5] = [0x7f800000, 0xff800000, 0x7fc00000, 0x7f800001, 0xffffffff];
const F64_BAD: [u64; 5] = [
    0x7ff0000000000000,
    0xfff0000000000000,
    0x7ff8000000000000,
    0x7ff0000000000001,
    0xffffffffffffffff,
];
const F32_GOOD: [u32; 3] = [0x80000000, 0x00000001, 0x7f7fffff];
const F64_GOOD: [u64; 3] = [0x8000000000000000, 0x0000000000000001, 0x7fefffffffffffff];

fn flat_bytes(a: u32, b: u64) -> Vec<u8> {
    let mut v = Vec::new();
    v.extend_from_slice(&a.to_le_bytes());
    v.extend_from_slice(&b.to_le_bytes());
    v.push(1);
    v
}

fn non_finite(r: Result) -> bool {
    matches!(r, Err(Error::Stream(serialize::Error::NonFiniteFloat)))
}

fn read_flat_bytes(bytes: &[u8]) -> (Result, Flat) {
    let mut value = Flat::default();
    let mut rs = ReadStream::new(bytes, bytes.len());
    let r = read_flat(&mut rs, &mut value);
    (r, value)
}

fn read_single32_bytes(bytes: &[u8]) -> (Result, Single32) {
    let mut value = Single32::default();
    let mut rs = ReadStream::new(bytes, bytes.len());
    let r = read_single32(&mut rs, &mut value);
    (r, value)
}

fn read_single64_bytes(bytes: &[u8]) -> (Result, Single64) {
    let mut value = Single64::default();
    let mut rs = ReadStream::new(bytes, bytes.len());
    let r = read_single64(&mut rs, &mut value);
    (r, value)
}

fn write_panics(f: impl FnOnce(&mut WriteStream<'_>) + std::panic::UnwindSafe) -> bool {
    std::panic::catch_unwind(|| {
        let mut buffer = [0u8; 64];
        let mut ws = WriteStream::new(&mut buffer);
        f(&mut ws);
    })
    .is_err()
}

fn main() {
    let mut failed = false;
    let mut check = |ok: bool, what: String| {
        if !ok {
            println!("FAILED: {what}");
            failed = true;
        }
    };

    // the layout the vectors assume is the one the generated writer writes
    let mut buffer = [0u8; 64];
    let mut ws = WriteStream::new(&mut buffer);
    let value = Flat { a: 1.5, b: -2.25, c: true };
    write_flat(&mut ws, &value).expect("write Flat");
    ws.flush();
    check(ws.data() == &flat_bytes(1.5f32.to_bits(), (-2.25f64).to_bits())[..], "Flat layout".to_string());

    for bits in F32_BAD {
        check(non_finite(read_flat_bytes(&flat_bytes(bits, 0)).0), format!("Flat.a read refuses 0x{bits:08x}"));
        check(non_finite(read_single32_bytes(&bits.to_le_bytes()).0), format!("Single32 read refuses 0x{bits:08x}"));
    }
    for bits in F64_BAD {
        check(non_finite(read_flat_bytes(&flat_bytes(0, bits)).0), format!("Flat.b read refuses 0x{bits:016x}"));
        check(non_finite(read_single64_bytes(&bits.to_le_bytes()).0), format!("Single64 read refuses 0x{bits:016x}"));
    }
    for bits in F32_GOOD {
        let (r, v) = read_flat_bytes(&flat_bytes(bits, 0));
        check(r.is_ok() && v.a.to_bits() == bits, format!("Flat.a reads 0x{bits:08x} bit exact"));
        let (r, v) = read_single32_bytes(&bits.to_le_bytes());
        check(r.is_ok() && v.x.to_bits() == bits, format!("Single32 reads 0x{bits:08x} bit exact"));
    }
    for bits in F64_GOOD {
        let (r, v) = read_flat_bytes(&flat_bytes(0, bits));
        check(r.is_ok() && v.b.to_bits() == bits, format!("Flat.b reads 0x{bits:016x} bit exact"));
        let (r, v) = read_single64_bytes(&bits.to_le_bytes());
        check(r.is_ok() && v.x.to_bits() == bits, format!("Single64 reads 0x{bits:016x} bit exact"));
    }

    // a non-finite write is caller error: a debug assertion, as the runtime's is
    std::panic::set_hook(Box::new(|_| {}));
    check(write_panics(|ws| { let _ = write_flat(ws, &Flat { a: f32::NAN, b: 0.0, c: false }); }), "Flat.a write of NaN asserts".to_string());
    check(write_panics(|ws| { let _ = write_flat(ws, &Flat { a: 0.0, b: f64::INFINITY, c: false }); }), "Flat.b write of infinity asserts".to_string());
    check(write_panics(|ws| { let _ = write_single32(ws, &Single32 { x: f32::NEG_INFINITY }); }), "Single32 write of -infinity asserts".to_string());
    check(write_panics(|ws| { let _ = write_single64(ws, &Single64 { x: f64::NAN }); }), "Single64 write of NaN asserts".to_string());
    let _ = std::panic::take_hook();

    if failed {
        std::process::exit(1);
    }
    println!("packet finite: OK");
}
`
