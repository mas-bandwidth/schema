package rusttable

// THE ANNOUNCEMENT'S SKIP IS BOUNDED (docs/SPEC-TABLES.md §3.3, §5;
// docs/SECURITY.md). announce_read steps over a field it cannot name by the
// field's kind, and every framed length it meets is compared to the bytes
// left by subtraction, so the cursor only ever moves forward and the read
// finishes on every input.
//
// The probe is a real Rust binary built twice, release and debug with
// overflow-checks on, because the defect the rule removes is a sum that wraps
// only when overflow checks are off. A hostile framed length near 2^64 and the
// same length inside a union are each refused in both profiles; a well-formed
// announcement carrying one unknown field of each framed kind is read. The
// clauses assert the VERDICT alone (docs/rulings.json refusal-verdict-only).
//
// A FUNCTIONAL TEST: it compiles and runs Rust with rustc. It skips by name
// when rustc is absent and SCHEMA_REQUIRE_CORPUS is unset, and fails by name
// when SCHEMA_REQUIRE_CORPUS=1. It reads only the generator's output: no
// clock beyond the probe's own finite timeout, no socket.

import (
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

const announceSkipBoundsSchema = `package probe

fixed table Probe
{
    x int32
}
`

// announceSkipBoundsProbe is the crate body: one verdict line per clause. A
// refusal clause is green when announce_read answers false, a read clause when
// it answers true, counts the unknown field and neither refuses nor damages.
const announceSkipBoundsProbe = `
fn say(id: &str, ok: bool) {
    if ok {
        println!("SKIP {id} ok");
    } else {
        println!("SKIP {id} BAD");
    }
}

fn refuses(data: &[u8]) -> bool {
    let mut storage = vec![TableMessageEntry::default(); 16];
    let mut vocabulary = TableVocabulary::new(&mut storage);
    let mut report = TableReport::default();
    !announce_read(&mut vocabulary, data, &mut report)
}

fn reads_unknown(data: &[u8]) -> bool {
    let mut storage = vec![TableMessageEntry::default(); 16];
    let mut vocabulary = TableVocabulary::new(&mut storage);
    let mut report = TableReport::default();
    announce_read(&mut vocabulary, data, &mut report)
        && report.unknown == 1
        && !report.refused
        && !report.damaged
}

fn main() {
    say("hostile_framed_refuses", refuses(&HOSTILE_FRAMED));
    say("hostile_union_refuses", refuses(&HOSTILE_UNION));
    say("wellformed_framed_12", reads_unknown(&WELLFORMED_FRAMED_12));
    say("wellformed_framed_13", reads_unknown(&WELLFORMED_FRAMED_13));
    say("wellformed_framed_14", reads_unknown(&WELLFORMED_FRAMED_14));
    say("wellformed_framed_16", reads_unknown(&WELLFORMED_FRAMED_16));
    say("wellformed_framed_31", reads_unknown(&WELLFORMED_FRAMED_31));
    say("wellformed_framed_32", reads_unknown(&WELLFORMED_FRAMED_32));
    say("wellformed_union_15", reads_unknown(&WELLFORMED_UNION_15));
}
`

func TestRustAnnouncementSkipFieldIsBounded(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "rustc (it builds and runs the announcement skip probe)")
	if _, err := exec.LookPath("rustc"); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and rustc is not on PATH, so the announcement skip cannot be built or run: %v", err)
		}
		t.Skipf("rustc is not on PATH and SCHEMA_REQUIRE_CORPUS is unset: this is a functional test that compiles and runs Rust: %v", err)
	}

	files, err := Generate(unitFrom(t, announceSkipBoundsSchema))
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	// A length twelve bytes back from the cursor puts the cursor on the field's
	// own reference byte, so an unbounded add re-reads the same field forever.
	hostileFramed := announcementWithUnknown(12, leb128(^uint64(0)-11), 0x1111111111111111)
	unionArm := append(leb128(1), 6)
	// The union field reaches its length fourteen bytes past its own reference.
	hostileUnion := announcementWithUnknown(15, append(unionArm, leb128(^uint64(0)-13)...), 0x2222222222222222)
	var framedCases []struct {
		id   string
		kind byte
	}
	for _, kind := range []byte{12, 13, 14, 16, 31, 32} {
		framedCases = append(framedCases, struct {
			id   string
			kind byte
		}{fmt.Sprintf("wellformed_framed_%d", kind), kind})
	}
	wellformed := map[string][]byte{}
	for i, c := range framedCases {
		payload := append(leb128(3), 0xaa, 0xbb, 0xcc)
		wellformed[c.id] = announcementWithUnknown(c.kind, payload, 0x3000000000000000+uint64(i))
	}
	wellformed["wellformed_union_15"] = announcementWithUnknown(15, append(append(leb128(1), 6), append(leb128(2), 0xaa, 0xbb)...), 0x4444444444444444)

	var b strings.Builder
	fmt.Fprintf(&b, "const HOSTILE_FRAMED: [u8; %d] = [%s];\n", len(hostileFramed), rustBytes(hostileFramed))
	fmt.Fprintf(&b, "const HOSTILE_UNION: [u8; %d] = [%s];\n", len(hostileUnion), rustBytes(hostileUnion))
	wellformedIDs := make([]string, 0, len(wellformed))
	for id := range wellformed {
		wellformedIDs = append(wellformedIDs, id)
	}
	sort.Strings(wellformedIDs)
	for _, id := range wellformedIDs {
		constName := strings.ToUpper(id)
		fmt.Fprintf(&b, "const %s: [u8; %d] = [%s];\n", constName, len(wellformed[id]), rustBytes(wellformed[id]))
	}
	b.WriteString(announceSkipBoundsProbe)

	profiles := []struct {
		name  string
		flags []string
	}{
		{"release", []string{"--edition", "2024", "-O"}},
		{"debug_overflow_checks", []string{"--edition", "2024", "-C", "overflow-checks=on"}},
	}
	for _, profile := range profiles {
		out, err := runAnnounceSkipBoundsProbe(t, files, b.String(), profile.flags, 30*time.Second)
		if err != nil {
			t.Fatalf("profile %s: the probe did not finish: %v\n%s", profile.name, err, out)
		}
		for _, id := range []string{"hostile_framed_refuses", "hostile_union_refuses"} {
			if line := skipBoundsLine(out, id); line != "SKIP "+id+" ok" {
				t.Errorf("profile %s: a hostile announcement was not refused: %s", profile.name, line)
			}
		}
		for _, id := range wellformedIDs {
			if line := skipBoundsLine(out, id); line != "SKIP "+id+" ok" {
				t.Errorf("profile %s: a well-formed announcement carrying an unknown field was not read: %s", profile.name, line)
			}
		}
	}

	// THE NEGATIVE CONTROL: the additive bound is planted back into the
	// generated runtime, and the probe must not stay green. Release is the
	// profile where the sum wraps, so that is where the witness is taken;
	// overflow checks make the debug profile red without a wrap.
	t.Run("negative_control_additive_bound", func(t *testing.T) {
		sabotaged := map[string][]byte{}
		for name, data := range files {
			sabotaged[name] = append([]byte{}, data...)
		}
		const bounded = "if !ok || n > (bytes.len() - *at) as u64 {"
		const additive = "if !ok || *at + (n as usize) > bytes.len() {"
		hit := false
		for name, data := range sabotaged {
			if strings.Contains(string(data), bounded) {
				sabotaged[name] = []byte(strings.ReplaceAll(string(data), bounded, additive))
				hit = true
			}
		}
		if !hit {
			t.Fatalf("NEGATIVE CONTROL: the bounded skip is not in the generated runtime")
		}
		out, err := runAnnounceSkipBoundsProbe(t, sabotaged, b.String(), []string{"--edition", "2024", "-O"}, 5*time.Second)
		if err != nil {
			return
		}
		if skipBoundsLine(out, "hostile_framed_refuses") == "SKIP hostile_framed_refuses ok" {
			t.Fatalf("NEGATIVE CONTROL FAILED: the additive bound stayed green on a hostile framed length\n%s", out)
		}
	})
}

func skipBoundsLine(out, id string) string {
	prefix := "SKIP " + id + " "
	for l := range strings.SplitSeq(out, "\n") {
		if strings.HasPrefix(l, prefix) {
			return l
		}
	}
	return fmt.Sprintf("[no SKIP line for %s in the probe's output: %s]", id, strings.TrimSpace(out))
}

// announcementWithUnknown appends one unknown field of the given kind to a
// minimal well-formed announcement: a third trailer entry carries the field's
// id and a third body field carries its kind and payload. Both reserved fields
// and their strict shapes stay exactly where they are.
func announcementWithUnknown(kind byte, payload []byte, id uint64) []byte {
	out := []byte{0x01}
	out = append(out, 0x01, 0x09)
	out = binary.LittleEndian.AppendUint64(out, 0x1234)
	out = append(out, 0x02, 0x0e, 0x02, 0x06, 0x00)
	out = append(out, 0x00)
	out = binary.LittleEndian.AppendUint64(out, 0xffff_ffff_ffff_fffe)
	out = binary.LittleEndian.AppendUint64(out, 0xffff_ffff_ffff_fffd)
	out = binary.LittleEndian.AppendUint64(out, 2)

	count := binary.LittleEndian.Uint64(out[len(out)-8:])
	end := len(out) - 8 - int(count)*8
	forged := []byte{out[0]}
	forged = append(forged, out[1:end-1]...)
	forged = append(forged, 0x03, kind)
	forged = append(forged, payload...)
	forged = append(forged, 0x00)
	forged = append(forged, out[end:len(out)-8]...)
	forged = binary.LittleEndian.AppendUint64(forged, id)
	return binary.LittleEndian.AppendUint64(forged, count+1)
}

// leb128 is the wire's unsigned LEB128, used here to spell one framed length.
func leb128(n uint64) []byte {
	var out []byte
	for {
		b := byte(n & 0x7f)
		n >>= 7
		if n != 0 {
			b |= 0x80
		}
		out = append(out, b)
		if n == 0 {
			return out
		}
	}
}

// runAnnounceSkipBoundsProbe writes the generated modules beside a crate root,
// builds them with rustc in the profile's flags, and runs the probe under a
// finite timeout so a read that does not finish is a test failure rather than a
// hung gate.
func runAnnounceSkipBoundsProbe(t *testing.T, files map[string][]byte, body string, flags []string, limit time.Duration) (string, error) {
	t.Helper()
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
	main.WriteString(body)
	if err := os.WriteFile(filepath.Join(dir, "main.rs"), []byte(main.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "announceskip")
	linkFlags := append([]string{}, flags...)
	if linker, err := exec.LookPath("cc"); err == nil {
		linkFlags = append(linkFlags, "-C", "linker="+linker)
	}
	build := exec.Command("rustc", append(linkFlags, "-o", bin, "main.rs")...)
	build.Dir = dir
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("the probe did not build: %v\n%s", err, out)
	}
	ctx, cancel := context.WithTimeout(context.Background(), limit)
	defer cancel()
	run := exec.CommandContext(ctx, bin)
	run.Dir = dir
	out, err := run.CombinedOutput()
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	return string(out), err
}
