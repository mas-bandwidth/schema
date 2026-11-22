package goosetable

import (
	"encoding/binary"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose/gooserun"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestGooseMessageStrictMode holds the OPT-IN STRICT MODE of the announcement
// read (docs/SPEC-TABLES.md §3.3, THE LANGUAGE-NEUTRAL SHAPE) to the Go
// reference's verdicts (internal/codegen/gotable/messageform_strict_test.go):
// with `strict` unset an endpoint of another build version reads clean; with
// it set the unit's own version (own_build_version, the unit's
// MessageBuildVersion) reads clean and another is refused
// `build_version_mismatch`, setting no vocabulary, keeping the peer's build
// version, naming both versions (the report's remote and own), never damage,
// and terminal: the next announcement is `second_announcement` and a batch is
// `no_vocabulary`.
func TestGooseMessageStrictMode(t *testing.T) {
	t.Parallel()
	got, want := gooseMessageStrictMode(t, nil)
	if got != want {
		t.Fatalf("strict probe:\n%s\nwant:\n%s", got, want)
	}
}

// TestGooseMessageStrictModeNegativeControl plants the old reader back (no
// strict check) in message.goose and requires the probe to go red.
func TestGooseMessageStrictModeNegativeControl(t *testing.T) {
	t.Parallel()
	src := string(Runtime()["message.goose"])
	const anchor = "    if v.strict && version != v.own_build_version {\n"
	if strings.Count(src, anchor) != 1 {
		t.Fatalf("message.goose carries the strict check %d times, want 1", strings.Count(src, anchor))
	}
	override := map[string][]byte{"message.goose": []byte(strings.Replace(src, anchor, "    if false && v.strict && version != v.own_build_version {\n", 1))}
	if got, want := gooseMessageStrictMode(t, override); got == want {
		t.Fatal("the reader without the strict check still passes the strict probe")
	}
}

// gooseMessageStrictMode runs the probe, the generated files replaced by
// override, and answers its output and the output the Go reference implies.
func gooseMessageStrictMode(t *testing.T, override map[string][]byte) (string, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "probe.schema")
	if err := os.WriteFile(path, []byte("package strictprobe\n\nfixed table Root {\n a int32\n b bool\n c uint8\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := loadSchemaPaths([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	own := ir.BuildVersion(u)
	remote := ^own
	// the unit's own announcement with its build version word replaced: byte
	// 0 is the form, byte 1 the version's reference, byte 2 its kind (9)
	mine := tablewire.Announce(u)
	if mine[2] != 9 || binary.LittleEndian.Uint64(mine[3:]) != own {
		t.Fatal("the announcement does not open with the build version")
	}
	other := append([]byte(nil), mine...)
	binary.LittleEndian.PutUint64(other[3:], remote)

	files, err := goose.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	maps.Copy(files, override)
	var mods []string
	for name := range files {
		if before, ok := strings.CutSuffix(name, ".goose"); ok {
			mods = append(mods, before)
		}
	}
	sort.Strings(mods)
	var b strings.Builder
	b.WriteString("import os;\n")
	for _, m := range mods {
		fmt.Fprintf(&b, "import .%s;\n", m)
	}
	fmt.Fprintf(&b, `
fn line(name: const u8[:], ok: bool, voc: const schema_rt::MsgVocabulary&, rep: const schema_rt::MsgReport&) {
    print(name, " ", (if ok { 1 } else { 0 }), " ", (if voc.announced { 1 } else { 0 }), " ", voc.count, " ", voc.build_version, " ", schema_rt::msg_reason_name(rep.reason), " ", (if rep.damaged { 1 } else { 0 }), " ", rep.unknown, " ", rep.remote, " ", rep.own);
}

fn main() {
    var mine: u8[>..] = [];
    var other: u8[>..] = [];
    if !read_file("own.bin", mine) || !read_file("remote.bin", other) { print("read-failed"); return; }
    let own = %[1]s::MessageBuildVersion;

    var v = schema_rt::MsgVocabulary { .. };
    var r = schema_rt::MsgReport { .. };
    var ok = schema_rt::announce_read(v, other, r);
    line("default", ok, v, r);

    v = schema_rt::MsgVocabulary { strict: true, own_build_version: own, .. };
    r = schema_rt::MsgReport { .. };
    ok = schema_rt::announce_read(v, mine, r);
    line("strict-same", ok, v, r);

    v = schema_rt::MsgVocabulary { strict: true, own_build_version: own, .. };
    r = schema_rt::MsgReport { .. };
    ok = schema_rt::announce_read(v, other, r);
    line("strict-other", ok, v, r);

    var second = schema_rt::MsgReport { .. };
    ok = schema_rt::announce_read(v, mine, second);
    line("terminal", ok, v, second);

    var values: %[1]s::Root[1] = [%[1]s::Root { .. }; 1];
    var body = schema_rt::MsgReport { .. };
    let batch: u8[2] = [2, 0];
    let count = %[1]s::RootLoadMessages(values[0..1], v, batch[0..2], body);
    print("batch ", count, " ", schema_rt::msg_reason_name(body.reason));
}
`, goose.Namespace(u))
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "main.goose"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	got := strings.TrimSpace(string(gooserun.Run(t, dir, "main.goose", map[string][]byte{"own.bin": mine, "remote.bin": other})))
	entries := len(tablewire.MessageVocabulary(u))
	want := strings.Join([]string{
		fmt.Sprintf("default 1 1 %d %d ok 0 0 0 0", entries, remote),
		fmt.Sprintf("strict-same 1 1 %d %d ok 0 0 0 0", entries, own),
		fmt.Sprintf("strict-other 0 0 0 %d build_version_mismatch 0 0 %d %d", remote, remote, own),
		fmt.Sprintf("terminal 0 0 0 %d second_announcement 0 0 0 0", remote),
		"batch 0 no_vocabulary",
	}, "\n")
	return got, want
}
