package gotable

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestMessageFormGoldensCarryTheBuildVersionForm pins the announce goldens to
// the current ir.BuildVersionForm (docs/SPEC-TABLES.md §20.2). It runs in the
// plain gate — no slowtest.Gate — so the next form bump cannot leave the
// goldens stale unseen: it computes the expected word from
// ir.BuildVersion(unit), so bumping the form without regenerating fails it.
func TestMessageFormGoldensCarryTheBuildVersionForm(t *testing.T) {
	t.Parallel()
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt")
	units, _, _ := parseManifest(t, manifest)

	type connRow struct {
		name, unit, path string
		word             uint64
	}
	conns := map[string]connRow{}
	data, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 5 && fields[0] == "connection" {
			word, err := strconv.ParseUint(strings.TrimPrefix(fields[3], "0x"), 16, 64)
			if err != nil {
				t.Fatalf("connection %s build version %q: %v", fields[1], fields[3], err)
			}
			conns[fields[1]] = connRow{name: fields[1], unit: fields[2], path: fields[4], word: word}
		}
	}
	if len(conns) == 0 {
		t.Fatalf("%s carries no connection rows", manifest)
	}

	// Every unit a connection names is parsed once, here, before the parallel
	// subtests: loadUnitPaths is not concurrency-safe and the subtests must not
	// share a written cache.
	loaded := map[string]*ir.Unit{}
	for _, c := range conns {
		if _, ok := loaded[c.unit]; ok {
			continue
		}
		paths, ok := units[c.unit]
		if !ok {
			t.Fatalf("manifest names unit %q with no unit line", c.unit)
		}
		loaded[c.unit] = loadUnitPaths(t, root, paths)
	}

	// The announcement's build-version word: an ordinary form-1 file (docs/
	// SPEC-TABLES.md §3.3, §20.2) whose body opens with reference 1, kind 9
	// (u64) framing the eight build-version bytes, at offset 3.
	const buildVersionOffset = 3
	for _, c := range conns {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			expected := ir.BuildVersion(loaded[c.unit])
			if c.word != expected {
				t.Fatalf("connection %s: manifest word 0x%016x != expected ir.BuildVersion 0x%016x at ir.BuildVersionForm %d",
					c.name, c.word, expected, ir.BuildVersionForm)
			}
			announcement, err := os.ReadFile(filepath.Join(root, c.path))
			if err != nil {
				t.Fatal(err)
			}
			if len(announcement) < buildVersionOffset+8 {
				t.Fatalf("connection %s: announcement %s is too short (%d bytes)", c.name, c.path, len(announcement))
			}
			committed := binary.LittleEndian.Uint64(announcement[buildVersionOffset : buildVersionOffset+8])
			if committed != expected {
				t.Fatalf("connection %s: committed word 0x%016x != expected ir.BuildVersion 0x%016x at ir.BuildVersionForm %d",
					c.name, committed, expected, ir.BuildVersionForm)
			}
		})
	}
}

// The original graph announcement is a historical reader fixture. A new build
// gets a separately named announcement; changing the manifest must not silently
// turn the original peer's build word into this writer's current word.
func TestGraphAnnouncementKeepsItsHistoricalEpoch(t *testing.T) {
	t.Parallel()
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	oldBytes, err := os.ReadFile(filepath.Join(root, "testdata/wire/tables/graph_conn.bin"))
	if err != nil {
		t.Fatal(err)
	}
	currentBytes, err := os.ReadFile(filepath.Join(root, "testdata/wire/tables/graph_conn_current.bin"))
	if err != nil {
		t.Fatal(err)
	}
	u := loadUnitPaths(t, root, []string{"tables/pointers"})
	if !bytes.Equal(currentBytes, tablewire.Announce(u)) {
		t.Fatal("current graph announcement is not the checked writer's output")
	}
	if len(oldBytes) != len(currentBytes) || len(oldBytes) < 11 ||
		!bytes.Equal(oldBytes[:3], currentBytes[:3]) || !bytes.Equal(oldBytes[11:], currentBytes[11:]) {
		t.Fatal("graph vocabulary, framing or size changed across identity epochs")
	}
	if binary.LittleEndian.Uint64(oldBytes[3:11]) != 0xfdaabe4726d81d5c {
		t.Fatal("the original graph announcement's historical build was changed")
	}
	var old, current tablewire.Vocabulary
	var report tabletext.Report
	if err := old.AnnounceRead(oldBytes, &report); err != nil || !report.Silent() {
		t.Fatalf("historical graph announcement: %v %+v", err, report)
	}
	report = tabletext.Report{}
	if err := current.AnnounceRead(currentBytes, &report); err != nil || !report.Silent() {
		t.Fatalf("current graph announcement: %v %+v", err, report)
	}
	if old.BuildVersion() == current.BuildVersion() || current.BuildVersion() != ir.BuildVersion(u) ||
		!reflect.DeepEqual(old.Entries(), current.Entries()) {
		t.Fatal("graph announcement epochs or unchanged vocabulary were not preserved")
	}
}
