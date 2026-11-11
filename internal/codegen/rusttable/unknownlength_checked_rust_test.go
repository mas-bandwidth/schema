package rusttable

// A LENGTH READ FROM THE WIRE IS COMPARED TO THE BYTES LEFT BEFORE ANY
// ADDITION (docs/SPEC-TABLES.md §5; docs/SECURITY.md: no crash on hostile
// input, and a build profile never changes a verdict). The plain variable load
// and the message announcement answer the same in a debug build (overflow
// checks on) as in a release build: a length past the bytes left is damage, not
// an offset sum that wraps.
//
// A FUNCTIONAL TEST: it compiles and runs the generated Rust with rustc alone,
// twice, once with overflow checks on and once optimized. It skips by name when
// rustc is absent and SCHEMA_REQUIRE_CORPUS is unset, and fails by name when
// SCHEMA_REQUIRE_CORPUS=1. The Go half reads only the generator's output: no
// clock, no socket.

import (
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

const unknownLengthSchema = `package probe

fixed table FixedRoot
{
    a int32
}

table Root
{
    x int32
}
`

// unknownLengthVarWire is a form-1 file whose one field is an unknown keyed
// collection of union elements: the collection's element entry carries a union
// arm whose declared length is 2^64-1. The entry length is well formed, so only
// the arm length is past the bytes left.
func unknownLengthVarWire() []byte {
	arm := []byte{0x01, 0x01} // the union reference, then the arm kind
	arm = append(arm, hugeLeb()...)
	entry := arm
	content := []byte{15, 0x01, 0x01, byte(len(entry))} // element kind 15, count, key, entry length
	content = append(content, entry...)

	out := []byte{1, 1, 16, byte(len(content))} // form, id reference, kind 16, framed content length
	out = append(out, content...)
	out = append(out, 0x00) // the body terminator
	out = binary.LittleEndian.AppendUint64(out, 0x1111111111111111)
	out = binary.LittleEndian.AppendUint64(out, 1)
	return out
}

// unknownLengthAnnouncementHugeFramed is an announcement whose vocabulary
// field's framed length is 2^64-1.
func unknownLengthAnnouncementHugeFramed() []byte {
	out := []byte{0x01, 0x01, 0x09}
	out = append(out, make([]byte, 8)...)
	out = append(out, 0x02, 0x0e)
	out = append(out, hugeLeb()...)
	out = append(out, 0x00)
	out = binary.LittleEndian.AppendUint64(out, 0xfffffffffffffffe)
	out = binary.LittleEndian.AppendUint64(out, 0xfffffffffffffffd)
	out = binary.LittleEndian.AppendUint64(out, 2)
	return out
}

// unknownLengthAnnouncementHugeInner is an announcement whose vocabulary frame
// is exactly the bytes left, but whose declared element length is 2^64-1.
func unknownLengthAnnouncementHugeInner() []byte {
	out := []byte{0x01, 0x01, 0x09}
	out = append(out, make([]byte, 8)...)
	out = append(out, 0x02, 0x0e, 0x0b, 0x06) // reference, kind 14, framed 11, element kind 6
	out = append(out, hugeLeb()...)
	out = append(out, 0x00)
	out = binary.LittleEndian.AppendUint64(out, 0xfffffffffffffffe)
	out = binary.LittleEndian.AppendUint64(out, 0xfffffffffffffffd)
	out = binary.LittleEndian.AppendUint64(out, 2)
	return out
}

// hugeLeb is 2^64-1 in ten canonical bytes: the largest length the wire can
// spell, and the largest one an unchecked offset sum can wrap.
func hugeLeb() []byte {
	return []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x01}
}

// unknownLengthProbe is the crate root's body: the two hostile files and the
// two well-formed controls, each clause printed by name. A panic is a failing
// verdict, so the probe keeps going and names every clause.
func unknownLengthProbe(varWire, announceFramed, announceInner []byte) string {
	var b strings.Builder
	b.WriteString("fn clause<F: FnOnce() -> bool>(id: &str, f: F) {\n")
	b.WriteString("    match std::panic::catch_unwind(std::panic::AssertUnwindSafe(f)) {\n")
	b.WriteString("        Ok(true) => println!(\"PROBE {id} ok\"),\n")
	b.WriteString("        Ok(false) => println!(\"PROBE {id} BAD verdict\"),\n")
	b.WriteString("        Err(_) => println!(\"PROBE {id} BAD panic\"),\n")
	b.WriteString("    }\n")
	b.WriteString("}\n\n")
	fmt.Fprintf(&b, "const VAR_WIRE: [u8; %d] = [%s];\n", len(varWire), rustBytes(varWire))
	fmt.Fprintf(&b, "const ANNOUNCE_FRAMED: [u8; %d] = [%s];\n", len(announceFramed), rustBytes(announceFramed))
	fmt.Fprintf(&b, "const ANNOUNCE_INNER: [u8; %d] = [%s];\n\n", len(announceInner), rustBytes(announceInner))
	b.WriteString("fn main() {\n")
	b.WriteString("    clause(\"var/control\", || {\n")
	b.WriteString("        let wire = root_save(&Root { x: 7 });\n")
	b.WriteString("        root_load(&wire, &mut TableReport::default()).is_some()\n")
	b.WriteString("    });\n")
	b.WriteString("    clause(\"var/refuse\", || root_load(&VAR_WIRE, &mut TableReport::default()).is_none());\n")
	b.WriteString("    clause(\"announce/control\", || {\n")
	b.WriteString("        let mut storage = vec![TableMessageEntry::default(); 64];\n")
	b.WriteString("        let mut vocab = TableVocabulary::new(&mut storage);\n")
	b.WriteString("        announce_read(&mut vocab, &TABLE_ANNOUNCEMENT, &mut TableReport::default())\n")
	b.WriteString("    });\n")
	b.WriteString("    clause(\"announce/refuse-framed\", || {\n")
	b.WriteString("        let mut storage = vec![TableMessageEntry::default(); 64];\n")
	b.WriteString("        let mut vocab = TableVocabulary::new(&mut storage);\n")
	b.WriteString("        !announce_read(&mut vocab, &ANNOUNCE_FRAMED, &mut TableReport::default())\n")
	b.WriteString("    });\n")
	b.WriteString("    clause(\"announce/refuse-inner\", || {\n")
	b.WriteString("        let mut storage = vec![TableMessageEntry::default(); 64];\n")
	b.WriteString("        let mut vocab = TableVocabulary::new(&mut storage);\n")
	b.WriteString("        !announce_read(&mut vocab, &ANNOUNCE_INNER, &mut TableReport::default())\n")
	b.WriteString("    });\n")
	b.WriteString("}\n")
	return b.String()
}

// TestRustUnknownLengthCheckedInEveryProfile builds and runs the probe with
// overflow checks on and optimized: every hostile length is refused in both and
// neither the hostile file nor the control panics.
func TestRustUnknownLengthCheckedInEveryProfile(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "rustc (it builds and runs the generated Rust)")
	if _, err := exec.LookPath("rustc"); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and rustc is not on PATH, so the length rule cannot be built or run: %v", err)
		}
		t.Skipf("rustc is not on PATH and SCHEMA_REQUIRE_CORPUS is unset: this is a functional test that compiles and runs Rust: %v", err)
	}
	files, err := Generate(unitFrom(t, unknownLengthSchema))
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	dir := t.TempDir()
	var mods []string
	for name, data := range files {
		mod := strings.TrimSuffix(name, ".rs")
		if strings.Contains(name, "/") || !strings.HasSuffix(name, ".rs") || mod == "lib" || mod == "probe" {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
		mods = append(mods, mod)
	}
	sort.Strings(mods)
	var main strings.Builder
	main.WriteString("#![allow(dead_code, unused_imports)]\n")
	for _, m := range mods {
		fmt.Fprintf(&main, "mod %s;\npub use %s::*;\n", m, m)
	}
	main.WriteString(unknownLengthProbe(unknownLengthVarWire(), unknownLengthAnnouncementHugeFramed(), unknownLengthAnnouncementHugeInner()))
	if err := os.WriteFile(filepath.Join(dir, "main.rs"), []byte(main.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, profile := range []struct {
		name string
		args []string
	}{
		{"debug", []string{"-C", "overflow-checks=on"}},
		{"release", []string{"-O"}},
	} {
		t.Run(profile.name, func(t *testing.T) {
			t.Parallel()
			bin := filepath.Join(t.TempDir(), "unknownlength")
			args := []string{"--edition", "2024"}
			if cc, err := exec.LookPath("cc"); err == nil {
				if abs, err := filepath.Abs(cc); err == nil {
					cc = abs
				}
				args = append(args, "-C", "linker="+cc)
			}
			args = append(args, profile.args...)
			args = append(args, "-o", bin, "main.rs")
			build := exec.Command("rustc", args...)
			build.Dir = dir
			if out, err := build.CombinedOutput(); err != nil {
				t.Fatalf("the %s probe did not build: %v\n%s", profile.name, err, out)
			}
			run := exec.Command(bin)
			run.Dir = dir
			out, err := run.CombinedOutput()
			lines := map[string]string{}
			for line := range strings.SplitSeq(string(out), "\n") {
				if fields := strings.Fields(line); len(fields) >= 2 && fields[0] == "PROBE" {
					lines[fields[1]] = line
				}
			}
			for _, id := range []string{"var/control", "var/refuse", "announce/control", "announce/refuse-framed", "announce/refuse-inner"} {
				if got := lines[id]; got != "PROBE "+id+" ok" {
					if got == "" {
						t.Errorf("%s: the probe reported no %s clause: %v\n%s", profile.name, id, err, out)
					} else {
						t.Errorf("%s: %s", profile.name, got)
					}
				}
			}
		})
	}
}
