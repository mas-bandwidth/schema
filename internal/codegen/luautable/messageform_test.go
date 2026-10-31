package luautable_test

// THE MESSAGE FORM'S GOLDENS (docs/SPEC-TABLES.md §3.3): every `message` row
// of testdata/conformance/tables/MANIFEST.txt, read against its `connection`
// row's announcement, saved again, and compared byte for byte with the batch
// wire the C++ reference wrote. A batch with damage in its second body
// delivers the bodies before it and is malformed, terminal for the batch.

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
)

type messageConnRow struct {
	unit, announcement string
}

type messageRow struct {
	name, connection, root, batch string
}

func messageManifest(t *testing.T) (map[string][]string, map[string]messageConnRow, []messageRow) {
	t.Helper()
	f, err := os.Open(filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	units := map[string][]string{}
	conns := map[string]messageConnRow{}
	var rows []messageRow
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) == 0 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		switch fields[0] {
		case "unit":
			if len(fields) >= 3 {
				units[fields[1]] = fields[2:]
			}
		case "connection":
			if len(fields) >= 5 {
				conns[fields[1]] = messageConnRow{unit: fields[2], announcement: fields[4]}
			}
		case "message":
			if len(fields) >= 6 {
				rows = append(rows, messageRow{name: fields[1], connection: fields[2], root: fields[3], batch: fields[5]})
			}
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return units, conns, rows
}

// buildMessageUnit emits one manifest unit's type and table surface into a
// fresh directory and returns it, the module names to require, and the files.
func buildMessageUnit(t *testing.T, paths []string) (string, []string, map[string][]byte) {
	t.Helper()
	files, err := compiler.GatherPaths(prefix(paths))
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(files)
	if err != nil {
		t.Fatal(err)
	}
	out, err := luau.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := luautable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(out, tables)
	dir := t.TempDir()
	var mods []string
	for name, data := range out {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(name, "Table.luau") {
			mods = append(mods, strings.TrimSuffix(name, ".luau"))
		}
	}
	sort.Strings(mods)
	return dir, mods, out
}

func messageProbe(mods []string, rootName, load string) string {
	var b strings.Builder
	b.WriteString("local M = require(\"./schema_message\")\nlocal inputs = require(\"./inputs\")\n")
	for _, m := range mods {
		fmt.Fprintf(&b, "require(\"./%s\")\n", m)
	}
	fmt.Fprintf(&b, `local v = M.NewVocabulary()
local report = { malformed = false, verdict = 0, reason = "", unknown = 0 }
if not M.AnnounceRead(v, inputs.ann, report) then print("announce_fail") return end
local count, bodies = M.%s(v, %q, inputs.batch, 256, report)
print("count " .. tostring(count) .. " malformed " .. tostring(report.malformed))
if count < 0 then return end
local out, n = M.SaveMessages(v, %q, bodies, report)
if out == nil then print("save_fail") return end
print(hex(out))
`, load, rootName, rootName)
	return hexProbe + b.String()
}

func messageMain(t *testing.T, dir string, mods []string, rootName string, ann, batch []byte) string {
	t.Helper()
	return messageMainWith(t, dir, mods, rootName, "LoadMessages", ann, batch)
}

// messageMainWith runs the probe through one named batch read: the plain
// LoadMessages (strict) or LoadMessagesTolerant (docs/SPEC-TABLES.md §6.5).
func messageMainWith(t *testing.T, dir string, mods []string, rootName, load string, ann, batch []byte) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "main.luau"), []byte(messageProbe(mods, rootName, load)), 0o644); err != nil {
		t.Fatal(err)
	}
	out := luaurun.Run(t, dir, "main.luau", map[string][]byte{"ann": ann, "batch": batch})
	return strings.TrimSpace(string(out))
}

// TestLuauMessageFormMatchesTheGoldens holds every message row to its pinned
// batch bytes: LoadMessages against the connection's announcement, then
// SaveMessages, byte for byte (docs/SPEC-TABLES.md §3.3, "THE TWO FORMS ARE
// TWO ENCODINGS OF ONE VALUE, and the pin is a ROUND TRIP").
func TestLuauMessageFormMatchesTheGoldens(t *testing.T) {
	t.Parallel()
	units, conns, rows := messageManifest(t)
	if len(rows) != 13 {
		t.Fatalf("the manifest names %d message rows, want the 13 this card pins", len(rows))
	}
	compared := 0
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			conn, ok := conns[row.connection]
			if !ok {
				t.Fatalf("message %s names connection %s with no connection row", row.name, row.connection)
			}
			ann, err := os.ReadFile(filepath.Join(root, conn.announcement))
			if err != nil {
				t.Fatal(err)
			}
			batch, err := os.ReadFile(filepath.Join(root, row.batch))
			if err != nil {
				t.Fatal(err)
			}
			dir, mods, _ := buildMessageUnit(t, units[conn.unit])
			out := messageMain(t, dir, mods, row.root, ann, batch)
			lines := strings.Split(out, "\n")
			if len(lines) < 2 {
				t.Fatalf("the probe printed %q", out)
			}
			if !strings.HasPrefix(lines[0], "count 1 malformed false") {
				t.Fatalf("LoadMessages: %s", lines[0])
			}
			got, err := hex.DecodeString(lines[len(lines)-1])
			if err != nil {
				t.Fatalf("the probe printed %q: %v", out, err)
			}
			if string(got) != string(batch) {
				t.Fatalf("SaveMessages differs from %s: got %d bytes, want %d", row.batch, len(got), len(batch))
			}
			compared++
		})
	}
	t.Cleanup(func() { t.Logf("compared %d message rows against their batch goldens", compared) })
}

// TestLuauMessageFormNegativeControl patches the emitted runtime in memory,
// one anchor matched exactly once, so the batch count is read one short; the
// byte comparison must go red.
func TestLuauMessageFormNegativeControl(t *testing.T) {
	t.Parallel()
	units, conns, rows := messageManifest(t)
	var row messageRow
	for _, r := range rows {
		if r.name == "login_full" {
			row = r
			break
		}
	}
	if row.name == "" {
		t.Fatal("the manifest names no login_full message row")
	}
	conn := conns[row.connection]
	ann, err := os.ReadFile(filepath.Join(root, conn.announcement))
	if err != nil {
		t.Fatal(err)
	}
	batch, err := os.ReadFile(filepath.Join(root, row.batch))
	if err != nil {
		t.Fatal(err)
	}
	dir, mods, files := buildMessageUnit(t, units[conn.unit])
	const anchor = "local count = buffer.readu8(data, 1) + 1"
	runtime := string(files["schema_message.luau"])
	if strings.Count(runtime, anchor) != 1 {
		t.Fatalf("the negative control's anchor appears %d times, want exactly once", strings.Count(runtime, anchor))
	}
	patched := strings.Replace(runtime, anchor, "local count = buffer.readu8(data, 1) + 0", 1)
	if err := os.WriteFile(filepath.Join(dir, "schema_message.luau"), []byte(patched), 0o644); err != nil {
		t.Fatal(err)
	}
	out := messageMain(t, dir, mods, row.root, ann, batch)
	lines := strings.Split(out, "\n")
	if len(lines) >= 2 {
		if got, err := hex.DecodeString(lines[len(lines)-1]); err == nil && string(got) == string(batch) {
			t.Fatalf("the patched runtime still reproduced the batch: %q", out)
		}
	}
	if strings.Contains(out, "count 1 malformed false") && len(lines) >= 2 {
		t.Fatalf("the count was not read one short: %q", out)
	}
	t.Logf("the batch comparison went red: %q", out)
}

// TestLuauMessageFormDamageStopsAtTheSecondBody holds §3.3's terminal rule:
// damage inside body 2 delivers body 1 on the TOLERANT read, counts one
// malformed, and reads nothing after it; the plain read is STRICT and hands
// back no body, refusing the batch (docs/SPEC-TABLES.md §6.5). The fixture
// flips 63 (thirty past E) into the second body's first reference.
func TestLuauMessageFormDamageStopsAtTheSecondBody(t *testing.T) {
	t.Parallel()
	units, conns, _ := messageManifest(t)
	conn := conns["backend_conn"]
	ann, err := os.ReadFile(filepath.Join(root, conn.announcement))
	if err != nil {
		t.Fatal(err)
	}
	batch, err := os.ReadFile(filepath.Join(root, "testdata/wire/tables/backend_batch_damaged_second_message.bin"))
	if err != nil {
		t.Fatal(err)
	}
	dir, mods, _ := buildMessageUnit(t, units[conn.unit])
	out := messageMainWith(t, dir, mods, "LoginRequest", "LoadMessagesTolerant", ann, batch)
	if !strings.HasPrefix(out, "count 1 malformed true") {
		t.Fatalf("damage in body 2 did not stop at body 1: %q", out)
	}
	out = messageMainWith(t, dir, mods, "LoginRequest", "LoadMessages", ann, batch)
	if !strings.HasPrefix(out, "count -1 malformed true") {
		t.Fatalf("the plain batch read handed back a body of a damaged batch: %q", out)
	}
}
