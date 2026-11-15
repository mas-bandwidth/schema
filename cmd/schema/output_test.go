package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
)

func TestGeneratedOutputIsConfined(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "out")
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"../escape", filepath.Join(parent, "absolute"), `..\escape`} {
		if err := writeGenerated(dir, map[string][]byte{name: []byte("new")}, "go", false, "", nil, false); err == nil {
			t.Fatalf("accepted %q", name)
		}
	}
	outside := filepath.Join(parent, "existing")
	if err := os.WriteFile(outside, []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := writeGenerated(dir, map[string][]byte{"link": []byte("new")}, "go", false, "", nil, false); err == nil {
		t.Fatal("followed escaping link")
	}
	data, err := os.ReadFile(outside)
	if err != nil || string(data) != "original" {
		t.Fatalf("outside file changed: %q, %v", data, err)
	}
	if err := writeGenerated(dir, map[string][]byte{"ok.h": []byte("ok")}, "go", false, "", nil, false); err != nil {
		t.Fatal(err)
	}
}

func TestGeneratedWritesNestedPaths(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	files := map[string][]byte{
		"a/b/c.txt": []byte("nested"),
		"top.txt":   []byte("top"),
	}
	if err := writeGenerated(dir, files, "go", false, "", nil, false); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "a", "b", "c.txt"))
	if err != nil || string(data) != "nested" {
		t.Fatalf("nested file: %q, %v", data, err)
	}
	data, err = os.ReadFile(filepath.Join(dir, "top.txt"))
	if err != nil || string(data) != "top" {
		t.Fatalf("top file: %q, %v", data, err)
	}
	if err := writeGenerated(dir, map[string][]byte{"../x": []byte("no")}, "go", false, "", nil, false); err == nil {
		t.Fatal("accepted ../x")
	}
}

const messagePackSchemaV1 = `package msg

fixed table Command {
	speed int32 | min = 0, max = 100
	mode  uint8 | min = 0, max = 7
}
`

const messagePackSchemaV2 = `package msg

fixed table Command {
	speed int32  | min = 0, max = 100
	mode  uint8  | min = 0, max = 7
	tag   uint16 | min = 0, max = 1000
}
`

type messagePackFixture struct {
	v1UnitDir        string
	v2UnitDir        string
	v1TreeDir        string
	v2SilentTree     string
	v2UnknownTree    string
	v2OutOfRangeTree string
	v1ExpectedJSON   map[string]int
}

func setupMessagePackFixture(t *testing.T) messagePackFixture {
	t.Helper()
	base := t.TempDir()
	f := messagePackFixture{
		v1UnitDir:        filepath.Join(base, "unit-v1"),
		v2UnitDir:        filepath.Join(base, "unit-v2"),
		v1TreeDir:        filepath.Join(base, "tree-v1"),
		v2SilentTree:     filepath.Join(base, "tree-v2-silent"),
		v2UnknownTree:    filepath.Join(base, "tree-v2-unknown"),
		v2OutOfRangeTree: filepath.Join(base, "tree-v2-out-of-range"),
		v1ExpectedJSON:   map[string]int{"speed": 42, "mode": 3},
	}
	writeTestFile(t, filepath.Join(f.v1UnitDir, "Command.schema"), messagePackSchemaV1)
	writeTestFile(t, filepath.Join(f.v2UnitDir, "Command.schema"), messagePackSchemaV2)
	writeTestFile(t, filepath.Join(f.v1TreeDir, "Command.json"), `{"speed": 42, "mode": 3}`)
	writeTestFile(t, filepath.Join(f.v2SilentTree, "Command.json"), `{"speed": 55, "mode": 5, "tag": 777}`)
	writeTestFile(t, filepath.Join(f.v2UnknownTree, "Command.json"), `{"speed": 55, "mode": 5, "tag": 777, "extra": 9}`)
	writeTestFile(t, filepath.Join(f.v2OutOfRangeTree, "Command.json"), `{"speed": 250, "mode": 5, "tag": 777}`)
	return f
}

func decodeMessageWithAnnouncement(unitDir, root, annPath, msgPath string) (map[string]int, error) {
	c := compiler.New()
	paths, err := compiler.GatherPaths([]string{unitDir})
	if err != nil {
		return nil, fmt.Errorf("gather paths: %w", err)
	}
	u, err := c.Load(paths)
	if err != nil {
		return nil, fmt.Errorf("load unit: %w", err)
	}
	ann, err := os.ReadFile(annPath)
	if err != nil {
		return nil, fmt.Errorf("read announcement: %w", err)
	}
	msg, err := os.ReadFile(msgPath)
	if err != nil {
		return nil, fmt.Errorf("read message: %w", err)
	}
	tmp, err := os.MkdirTemp("", "schema-msg-decode-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	report, err := c.UnpackMessage(u, root, ann, msg, tmp, true)
	if err != nil {
		return nil, fmt.Errorf("unpack message: %w", err)
	}
	if !report.Silent() {
		return nil, fmt.Errorf("non-silent unpack report: %+v", report)
	}
	raw, err := os.ReadFile(filepath.Join(tmp, root+".json"))
	if err != nil {
		return nil, fmt.Errorf("read unpacked json: %w", err)
	}
	var got map[string]int
	if err := json.Unmarshal(raw, &got); err != nil {
		return nil, fmt.Errorf("unmarshal unpacked json: %w", err)
	}
	return got, nil
}

// packMessageRunner executes a `pack --root Command --message --announce <annPath> --out <msgPath> <treeDir> <unitDir>`
// attempt and returns its exit code and combined output.
type packMessageRunner func(annPath, msgPath, treeDir, unitDir string) (int, string)

// verifyRefusedMessagePackPreservesOutputs runs a non-silent `pack --message --announce`
// against both pre-existing V1 outputs and fresh destination paths, returning an error
// if either prior file is modified, if the preserved V1 message no longer decodes with
// its preserved announcement, or if any fresh output file is created.
func verifyRefusedMessagePackPreservesOutputs(t *testing.T, bin string, f messagePackFixture, runRefused packMessageRunner, treeDir string, wantLines ...string) error {
	t.Helper()

	// 1. Pre-existing outputs from V1 must stay byte-for-byte and mtime-identical,
	// and the V1 message must still decode against the V1 announcement.
	existingDir := t.TempDir()
	annPath := filepath.Join(existingDir, "Command.ann")
	msgPath := filepath.Join(existingDir, "Command.msg")
	run(t, bin, "pack", "--root", "Command", "--message", "--announce", annPath, "--out", msgPath, f.v1TreeDir, f.v1UnitDir)
	beforeAnn := stat(t, annPath)
	beforeMsg := stat(t, msgPath)

	code, out := runRefused(annPath, msgPath, treeDir, f.v2UnitDir)
	if code != 1 {
		return fmt.Errorf("refused pack exited %d, want 1\n%s", code, out)
	}
	for _, want := range wantLines {
		if !strings.Contains(out, want) {
			return fmt.Errorf("output missing %q:\n%s", want, out)
		}
	}
	afterAnn := stat(t, annPath)
	afterMsg := stat(t, msgPath)
	if !bytes.Equal(afterAnn.data, beforeAnn.data) {
		return fmt.Errorf("refused pack overwrote pre-existing announcement %s (%d -> %d bytes)", annPath, len(beforeAnn.data), len(afterAnn.data))
	}
	if !afterAnn.mod.Equal(beforeAnn.mod) {
		return fmt.Errorf("refused pack changed announcement mtime: %v -> %v", beforeAnn.mod, afterAnn.mod)
	}
	if !bytes.Equal(afterMsg.data, beforeMsg.data) {
		return fmt.Errorf("refused pack overwrote pre-existing message %s (%d -> %d bytes)", msgPath, len(beforeMsg.data), len(afterMsg.data))
	}
	if !afterMsg.mod.Equal(beforeMsg.mod) {
		return fmt.Errorf("refused pack changed message mtime: %v -> %v", beforeMsg.mod, afterMsg.mod)
	}
	decoded, err := decodeMessageWithAnnouncement(f.v1UnitDir, "Command", annPath, msgPath)
	if err != nil {
		return fmt.Errorf("preserved V1 outputs failed independent decode: %w", err)
	}
	if decoded["speed"] != f.v1ExpectedJSON["speed"] || decoded["mode"] != f.v1ExpectedJSON["mode"] || len(decoded) != len(f.v1ExpectedJSON) {
		return fmt.Errorf("decoded preserved V1 message = %v, want %v", decoded, f.v1ExpectedJSON)
	}

	// 2. Fresh destinations must not be created when the report refuses the pack.
	freshDir := t.TempDir()
	freshAnn := filepath.Join(freshDir, "fresh.ann")
	freshMsg := filepath.Join(freshDir, "fresh.msg")
	code, out = runRefused(freshAnn, freshMsg, treeDir, f.v2UnitDir)
	if code != 1 {
		return fmt.Errorf("fresh refused pack exited %d, want 1\n%s", code, out)
	}
	if _, err := os.Stat(freshAnn); err == nil {
		return fmt.Errorf("refused pack created fresh announcement %s", freshAnn)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat fresh announcement %s: %w", freshAnn, err)
	}
	if _, err := os.Stat(freshMsg); err == nil {
		return fmt.Errorf("refused pack created fresh message %s", freshMsg)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat fresh message %s: %w", freshMsg, err)
	}
	entries, err := os.ReadDir(freshDir)
	if err != nil {
		return err
	}
	if len(entries) != 0 {
		return fmt.Errorf("refused pack left %d entries in fresh directory %s", len(entries), freshDir)
	}
	return nil
}

// TestPackMessageAnnounceRefusalPreservesOutputs verifies that `schema pack --message --announce`
// evaluates the report gate before writing either the announcement or the message:
//   - a refused run (a value out of the declared range, docs/SPEC-TABLES.md §6.5)
//     over a changed vocabulary exits 1, preserves both pre-existing files
//     byte-for-byte (still decodable as a pair), and leaves neither fresh
//     destination behind;
//   - a report that counts only lawful evolution (an unknown field from a newer
//     writer) exits 0, publishes both files, and decodes cleanly against the new
//     announcement;
//   - a silent run exits 0, publishes both files, and decodes cleanly;
//   - a known-bad ordering control that writes `--announce` before checking the report
//     fails the same preservation assertion.
func TestPackMessageAnnounceRefusalPreservesOutputs(t *testing.T) {
	t.Parallel()
	bin := buildCLI(t)
	f := setupMessagePackFixture(t)

	cliRunner := func(annPath, msgPath, treeDir, unitDir string) (int, string) {
		return runExit(t, bin, "pack", "--root", "Command", "--message",
			"--announce", annPath, "--out", msgPath, treeDir, unitDir)
	}

	for _, tc := range []struct {
		name    string
		run     packMessageRunner
		treeDir string
		want    []string
	}{
		{
			name:    "out-of-range value refused",
			run:     cliRunner,
			treeDir: f.v2OutOfRangeTree,
			want:    []string{"out_of_range"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := verifyRefusedMessagePackPreservesOutputs(t, bin, f, tc.run, tc.treeDir, tc.want...); err != nil {
				t.Fatal(err)
			}
		})
	}

	t.Run("an unknown field from a newer writer publishes both and decodes", func(t *testing.T) {
		outDir := t.TempDir()
		annPath := filepath.Join(outDir, "Command.ann")
		msgPath := filepath.Join(outDir, "Command.msg")
		// Seed with V1 outputs first to verify the newer pair replaces both cleanly.
		run(t, bin, "pack", "--root", "Command", "--message", "--announce", annPath, "--out", msgPath, f.v1TreeDir, f.v1UnitDir)
		v1Ann := stat(t, annPath)
		v1Msg := stat(t, msgPath)

		code, out := runExit(t, bin, "pack", "--root", "Command", "--message",
			"--announce", annPath, "--out", msgPath, f.v2UnknownTree, f.v2UnitDir)
		if code != 0 {
			t.Fatalf("pack of an unknown field exited %d, want 0:\n%s", code, out)
		}
		if !strings.Contains(out, "report: unknown 1, widened 0") {
			t.Fatalf("pack did not print the unknown report:\n%s", out)
		}
		v2Ann := stat(t, annPath)
		v2Msg := stat(t, msgPath)
		if bytes.Equal(v1Ann.data, v2Ann.data) {
			t.Fatal("changed vocabulary produced identical announcement bytes")
		}
		if bytes.Equal(v1Msg.data, v2Msg.data) {
			t.Fatal("updated message produced identical message bytes")
		}
		decoded, err := decodeMessageWithAnnouncement(f.v2UnitDir, "Command", annPath, msgPath)
		if err != nil {
			t.Fatalf("V2 outputs failed independent decode: %v", err)
		}
		want := map[string]int{"speed": 55, "mode": 5, "tag": 777}
		if decoded["speed"] != want["speed"] || decoded["mode"] != want["mode"] || decoded["tag"] != want["tag"] {
			t.Fatalf("decoded V2 message = %v, want %v", decoded, want)
		}
		// Also verify CLI unpack --announce round-trips the pair.
		cliUnpackDir := t.TempDir()
		run(t, bin, "unpack", "--root", "Command", "--in", msgPath, "--announce", annPath, "--one-file", cliUnpackDir, f.v2UnitDir)
	})

	t.Run("silent input publishes both silently and decodes", func(t *testing.T) {
		outDir := t.TempDir()
		annPath := filepath.Join(outDir, "Command.ann")
		msgPath := filepath.Join(outDir, "Command.msg")
		code, out := runExit(t, bin, "pack", "--root", "Command", "--message",
			"--announce", annPath, "--out", msgPath, f.v2SilentTree, f.v2UnitDir)
		if code != 0 || out != "" {
			t.Fatalf("silent pack exited %d with output %q, want 0 and empty output", code, out)
		}
		decoded, err := decodeMessageWithAnnouncement(f.v2UnitDir, "Command", annPath, msgPath)
		if err != nil {
			t.Fatalf("silent V2 outputs failed independent decode: %v", err)
		}
		want := map[string]int{"speed": 55, "mode": 5, "tag": 777}
		if decoded["speed"] != want["speed"] || decoded["mode"] != want["mode"] || decoded["tag"] != want["tag"] {
			t.Fatalf("decoded silent V2 message = %v, want %v", decoded, want)
		}
	})

	t.Run("known-bad ordering control fails preservation assertion", func(t *testing.T) {
		// Simulate the pre-fix ordering: writing --announce before checking the refusal.
		badOrderingRunner := func(annPath, msgPath, treeDir, unitDir string) (int, string) {
			c := compiler.New()
			paths, err := compiler.GatherPaths([]string{unitDir})
			if err != nil {
				t.Fatal(err)
			}
			u, err := c.Load(paths)
			if err != nil {
				t.Fatal(err)
			}
			wire, _, r, err := c.PackMessages(u, []compiler.MessageTree{{Root: "Command", Dir: treeDir}})
			// Pre-fix bug: writes announcement before checking whether the report refuses.
			if werr := writeAtomic(annPath, c.Announce(u)); werr != nil {
				t.Fatal(werr)
			}
			if err != nil {
				return 1, err.Error()
			}
			if !r.Silent() {
				out := fmt.Sprintf("report: unknown %d, widened %d\n",
					r.Unknown, r.Widened)
				return 1, out
			}
			if err := writeAtomic(msgPath, wire); err != nil {
				t.Fatal(err)
			}
			return 0, ""
		}

		err := verifyRefusedMessagePackPreservesOutputs(t, bin, f, badOrderingRunner,
			f.v2OutOfRangeTree, "out_of_range")
		if err == nil {
			t.Fatal("known-bad ordering control passed verifyRefusedMessagePackPreservesOutputs; the assertion is not sensitive to announcement-before-report ordering")
		}
		if !strings.Contains(err.Error(), "overwrote pre-existing announcement") {
			t.Fatalf("known-bad ordering control failed for unexpected reason: %v", err)
		}
	})
}
