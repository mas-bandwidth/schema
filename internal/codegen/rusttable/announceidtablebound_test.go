package rusttable

// TestRustAnnouncementIdTableBound pins docs/SPEC-TABLES.md §3 for the
// MESSAGE FORM's announcement reader (announce_read): the announcement is an
// ordinary form-1 FILE, so its trailer id table takes §3's bound. A reader
// refuses as malformed any table whose entry count exceeds 4096 before doing
// any distinctness work, whatever the buffer would hold, and proves
// distinctness with the same open-addressing probe the variable file loader
// uses rather than a pairwise scan.
//
// A FUNCTIONAL TEST: it compiles and runs Rust with rustc alone (the
// announcement runtime is self-contained). It skips by name when rustc is
// absent and SCHEMA_REQUIRE_CORPUS is unset, and fails by name when
// SCHEMA_REQUIRE_CORPUS=1. No clock, no socket, no network.

import (
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const announceIdTableBoundSchema = `package probe

fixed table Packet
{
    x uint32
}
`

// announceWireWithIds builds a minimal form-1 announcement whose body carries
// the build version (reference 1) and the vocabulary (reference 2), both over
// the reserved ids, and whose trailer holds ids in the given order. The
// remaining trailer entries are unreferenced, which is still a table the
// reader must hold distinct.
func announceWireWithIds(ids []uint64) []byte {
	var wire []byte
	wire = append(wire, ir.TableWireForm)
	wire = appendLeb(wire, 1) // the build version: reference 1, kind 9
	wire = append(wire, 9)
	wire = binary.LittleEndian.AppendUint64(wire, 0x1234)
	wire = appendLeb(wire, 2) // the vocabulary: reference 2, kind 14
	wire = append(wire, 14, 2, 6, 0)
	wire = append(wire, 0) // the root terminator
	for _, id := range ids {
		wire = binary.LittleEndian.AppendUint64(wire, id)
	}
	return binary.LittleEndian.AppendUint64(wire, uint64(len(ids)))
}

// announceBoundIds is n distinct ids whose first two entries are the
// announcement's own reserved build-version and vocabulary ids.
func announceBoundIds(n int) []uint64 {
	ids := make([]uint64, n)
	ids[0] = 0xffff_ffff_ffff_fffe
	ids[1] = 0xffff_ffff_ffff_fffd
	for i := 2; i < n; i++ {
		ids[i] = 0x4000_0000_0000_0000 + uint64(i)
	}
	return ids
}

// instrumentComparisons rewrites one generated message runtime in a temporary
// copy so every id distinctness comparison lands in a counter. It is a test
// seam, not a clock: the call counts answer whether the 4097-id refusal was
// taken before any distinctness work.
func instrumentComparisons(t *testing.T, src string) string {
	t.Helper()
	const anchor = "use crate::*;\n"
	if !strings.Contains(src, anchor) {
		t.Fatal("the message runtime no longer opens with `use crate::*;`")
	}
	src = strings.Replace(src, anchor, anchor+
		"\npub static ID_COMPARISONS: std::sync::atomic::AtomicUsize = std::sync::atomic::AtomicUsize::new(0);\n"+
		"fn id_eq(a: u64, b: u64) -> bool { ID_COMPARISONS.fetch_add(1, std::sync::atomic::Ordering::Relaxed); a == b }\n", 1)
	replaced := false
	for _, pair := range [][2]string{
		{
			"if read_u64(data, end + (i as usize) * 8) == read_u64(data, end + (j as usize) * 8) {",
			"if id_eq(read_u64(data, end + (i as usize) * 8), read_u64(data, end + (j as usize) * 8)) {",
		},
		{
			"if probe_slots[h] == id {",
			"if id_eq(probe_slots[h], id) {",
		},
	} {
		if strings.Contains(src, pair[0]) {
			src = strings.ReplaceAll(src, pair[0], pair[1])
			replaced = true
		}
	}
	if !replaced {
		t.Fatal("the announcement reader carries no id comparison the seam can count")
	}
	return src
}

// runAnnounceCrate writes the generated modules, the wires and one crate root,
// builds it with rustc and runs it once.
func runAnnounceCrate(t *testing.T, bin string, modules map[string][]byte, runtime string, wires map[string][]byte, main string) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{BuildVersionModule + ".rs", VariableRuntimeModule + ".rs"} {
		if data, ok := modules[name]; ok {
			if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := os.WriteFile(filepath.Join(dir, MessageRuntimeModule+".rs"), []byte(runtime), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, data := range wires {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "main.rs"), []byte(main), 0o600); err != nil {
		t.Fatal(err)
	}
	binPath := filepath.Join(dir, bin)
	build := exec.Command("rustc", "--edition", "2024", "-O", "-o", binPath, "main.rs")
	build.Dir = dir
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("the announcement probe did not build: %v\n%s", err, out)
	}
	run := exec.Command(binPath)
	run.Dir = dir
	out, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("the announcement probe failed: %v\n%s", err, out)
	}
	return string(out)
}

func TestRustAnnouncementIdTableBound(t *testing.T) {
	slowtest.Gate(t, "an external toolchain")

	t.Parallel()

	u := unitFrom(t, announceIdTableBoundSchema)
	modules, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	src, ok := modules[MessageRuntimeModule+".rs"]
	if !ok {
		t.Fatalf("no %s.rs in the generated unit", MessageRuntimeModule)
	}
	runtime := string(src)

	if _, err := exec.LookPath("rustc"); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and rustc is not on PATH: %v", err)
		}
		t.Skipf("rustc is not on PATH: %v", err)
	}

	ids4096 := announceBoundIds(4096)
	wire4096 := announceWireWithIds(ids4096)
	ids4097 := announceBoundIds(4097)
	wire4097 := announceWireWithIds(ids4097)
	idsDup := announceBoundIds(4096)
	idsDup[100] = idsDup[101]
	wireDup := announceWireWithIds(idsDup)
	wires := map[string][]byte{
		"w4096.wire": wire4096,
		"w4097.wire": wire4097,
		"wdup.wire":  wireDup,
	}

	// The verdicts, over the runtime exactly as generated.
	verdictMain := announceVerdictsMain(modules)
	out := runAnnounceCrate(t, "announce_verdicts", modules, runtime, wires, verdictMain)
	if !strings.Contains(out, "ANNOUNCEMENT ID TABLE BOUND OK") {
		t.Fatalf("unexpected probe output:\n%s", out)
	}

	// The work: a seam counts every id distinctness comparison, so the 4097-id
	// refusal must make none, while the duplicate is found by one.
	counterMain := announceComparisonsMain(modules)
	out = runAnnounceCrate(t, "announce_comparisons", modules, instrumentComparisons(t, runtime), wires, counterMain)
	if !strings.Contains(out, "ANNOUNCEMENT ID TABLE BOUND COMPARISONS OK") {
		t.Fatalf("unexpected comparison probe output:\n%s", out)
	}

	for _, decl := range []string{
		"const ID_TABLE_PROBE_BOUND: usize = 4096;",
		"const ID_TABLE_PROBE_SLOTS: usize = 8192;",
	} {
		if !strings.Contains(runtime, decl) {
			t.Fatalf("%s.rs is missing %q", MessageRuntimeModule, decl)
		}
	}
	body, err := extractRustFn(runtime, "fn announce_read_once(")
	if err != nil {
		t.Fatal(err)
	}
	boundIdx := strings.Index(body, "trailer_count > ID_TABLE_PROBE_BOUND as u64")
	if boundIdx < 0 {
		t.Fatalf("announce_read_once is missing the ID_TABLE_PROBE_BOUND refusal")
	}
	probeIdx := strings.Index(body, "let mut probe_slots = [0u64; ID_TABLE_PROBE_SLOTS];")
	if probeIdx < 0 {
		t.Fatalf("announce_read_once is missing the ID_TABLE_PROBE_SLOTS fixed probe array")
	}
	hashIdx := strings.Index(body, "0x9e3779b97f4a7c15")
	if hashIdx < 0 {
		t.Fatalf("announce_read_once is missing the multiplicative probe hash")
	}
	if boundIdx > probeIdx || boundIdx > hashIdx {
		t.Errorf("announce_read_once checks ID_TABLE_PROBE_BOUND after the probe walk")
	}
	for _, forbidden := range []string{"for j in 0..i", "ids.contains"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("announce_read_once still carries %q", forbidden)
		}
	}
}

func announceModuleRoot(modules map[string][]byte) string {
	var b strings.Builder
	b.WriteString("mod build_version;\nmod message_runtime;\n")
	b.WriteString("pub use build_version::*;\npub use message_runtime::*;\n")
	if _, ok := modules[VariableRuntimeModule+".rs"]; ok {
		b.WriteString("mod variable_runtime;\npub use variable_runtime::*;\n")
	}
	return b.String()
}

func announceVerdictsMain(modules map[string][]byte) string {
	return announceModuleRoot(modules) + `
fn main() {
    let w4096 = std::fs::read("w4096.wire").expect("read w4096.wire");
    let mut storage4096 = vec![TableMessageEntry::default(); 4096];
    let mut vocab4096 = TableVocabulary::new(&mut storage4096);
    let mut report4096 = TableReport::default();
    let ok4096 = announce_read(&mut vocab4096, &w4096, &mut report4096);
    assert!(ok4096 && vocab4096.announced, "an announcement with 4096 distinct ids must be read");

    let w4097 = std::fs::read("w4097.wire").expect("read w4097.wire");
    let mut storage4097 = vec![TableMessageEntry::default(); 4096];
    let mut vocab4097 = TableVocabulary::new(&mut storage4097);
    let mut report4097 = TableReport::default();
    let ok4097 = announce_read(&mut vocab4097, &w4097, &mut report4097);
    assert!(!ok4097 && !vocab4097.announced, "an announcement with 4097 distinct ids must be refused");

    let wdup = std::fs::read("wdup.wire").expect("read wdup.wire");
    let mut storage_dup = vec![TableMessageEntry::default(); 4096];
    let mut vocab_dup = TableVocabulary::new(&mut storage_dup);
    let mut report_dup = TableReport::default();
    let ok_dup = announce_read(&mut vocab_dup, &wdup, &mut report_dup);
    assert!(!ok_dup && !vocab_dup.announced, "an announcement with a repeated id among 4096 must be refused");

    println!("ANNOUNCEMENT ID TABLE BOUND OK");
}
`
}

func announceComparisonsMain(modules map[string][]byte) string {
	root := announceModuleRoot(modules)
	if !strings.Contains(root, "pub use message_runtime::*;") {
		panic("the comparison probe needs the message runtime on the crate root")
	}
	return root + `
use std::sync::atomic::Ordering;

fn main() {
    let w4097 = std::fs::read("w4097.wire").expect("read w4097.wire");
    message_runtime::ID_COMPARISONS.store(0, Ordering::Relaxed);
    let mut storage4097 = vec![TableMessageEntry::default(); 4096];
    let mut vocab4097 = TableVocabulary::new(&mut storage4097);
    let mut report4097 = TableReport::default();
    let ok4097 = announce_read(&mut vocab4097, &w4097, &mut report4097);
    assert!(!ok4097 && !vocab4097.announced, "an announcement with 4097 distinct ids must be refused");
    let before = message_runtime::ID_COMPARISONS.load(Ordering::Relaxed);
    assert_eq!(before, 0, "the 4097-id refusal made {before} id distinctness comparisons before refusing");

    let wdup = std::fs::read("wdup.wire").expect("read wdup.wire");
    message_runtime::ID_COMPARISONS.store(0, Ordering::Relaxed);
    let mut storage_dup = vec![TableMessageEntry::default(); 4096];
    let mut vocab_dup = TableVocabulary::new(&mut storage_dup);
    let mut report_dup = TableReport::default();
    let ok_dup = announce_read(&mut vocab_dup, &wdup, &mut report_dup);
    assert!(!ok_dup && !vocab_dup.announced, "an announcement with a repeated id among 4096 must be refused");
    let duplicate = message_runtime::ID_COMPARISONS.load(Ordering::Relaxed);
    assert!(duplicate > 0, "the seam counted no comparison for the repeated id");

    println!("ANNOUNCEMENT ID TABLE BOUND COMPARISONS OK");
}
`
}
