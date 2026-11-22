package luautable_test

import (
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
)

// TestLuauMessageStrictMode holds the OPT-IN STRICT MODE of the announcement
// read (docs/SPEC-TABLES.md §3.3, THE LANGUAGE-NEUTRAL SHAPE) to the Go
// reference's verdicts (internal/codegen/gotable/messageform_strict_test.go):
// with `strict` unset an endpoint of another build version reads clean; with
// it set the reader's own version reads clean and another is refused
// `build_version_mismatch`, setting no vocabulary, keeping the peer's build
// version, naming both versions (report.remote, report.own), never damage,
// and terminal: the next announcement is `second_announcement` and a batch is
// `no_vocabulary`.
func TestLuauMessageStrictMode(t *testing.T) {
	t.Parallel()
	if got := luauMessageStrictMode(t, nil); got != "ok" {
		t.Fatalf("strict probe: %s", got)
	}
}

// TestLuauMessageStrictModeNegativeControl plants the old reader back (no
// strict check) in schema_message.luau and requires the probe to go red.
func TestLuauMessageStrictModeNegativeControl(t *testing.T) {
	t.Parallel()
	const anchor = "\tif v.strict and v.buildVersion ~= v.ownVersion then\n"
	got := luauMessageStrictMode(t, func(src []byte) []byte {
		if strings.Count(string(src), anchor) != 1 {
			t.Fatalf("schema_message.luau carries the strict check %d times, want 1", strings.Count(string(src), anchor))
		}
		return []byte(strings.Replace(string(src), anchor, "\tif false then\n", 1))
	})
	if got == "ok" {
		t.Fatal("the reader without the strict check still passes the strict probe")
	}
}

const luauStrictModeProbe = `
local T = require("./ProbeTable")
local M = require("./schema_message")

local own = T.TableMessageBuildVersion
local remote = integer.bnot(own)
local failures = {}
local function check(ok: boolean, what: string)
	if not ok then table.insert(failures, what) end
end

-- the unit's own announcement with its build version word replaced: byte 0 is
-- the form, byte 1 the version's reference, byte 2 its kind (9)
local function endpoint(version: any): buffer
	local b = buffer.create(buffer.len(T.TableMessageAnnouncement))
	buffer.copy(b, 0, T.TableMessageAnnouncement)
	assert(buffer.readu8(b, 2) == 9, "the announcement does not open with the build version")
	buffer.writeinteger(b, 3, version)
	return b
end

-- default_tolerant
local v = M.NewVocabulary(own)
local r: any = {}
check(M.AnnounceRead(v, endpoint(remote), r), "the tolerant read refused another build version")
check(v.announced and v.buildVersion == remote and r.reason == nil, "tolerant read")

-- strict_same_version
v = M.NewVocabulary(own)
check(v.strict == false, "a new vocabulary is strict")
v.strict = true
r = {}
check(M.AnnounceRead(v, endpoint(own), r), "the strict read refused its own build version")
check(v.announced and r.reason == nil and r.remote == nil, "strict same-version read")

-- strict_other_version, typed refusal
v = M.NewVocabulary(own)
v.strict = true
r = {}
check(not M.AnnounceRead(v, endpoint(remote), r), "the strict read accepted another build version")
check(r.reason == "build_version_mismatch", "reason: " .. tostring(r.reason))
check(not r.malformed, "the strict refusal is damage")
check(r.remote == remote and r.own == own, "the refusal does not carry both versions")
check(not v.announced and v.count == 0, "the strict refusal set a vocabulary")
check(v.buildVersion == remote, "the peer's build version was not kept")
check((r.unknown or 0) == 0, "the strict refusal moved a counter")

-- terminal
local second: any = {}
check(not M.AnnounceRead(v, endpoint(own), second), "an announcement after a strict refusal was read")
check(second.reason == "second_announcement", "after a strict refusal: " .. tostring(second.reason))
-- a form-2 batch of one body: refused for want of a vocabulary before a body is read
local batch = buffer.fromstring("\2\0")
local body: any = {}
local count = M.LoadMessages(v, "Root", batch, 1, body)
check(count == 0 and body.reason == "no_vocabulary", "a batch on the refused connection: " .. tostring(body.reason))

if #failures == 0 then print("ok") else print(table.concat(failures, "; ")) end
`

// luauMessageStrictMode runs the probe, schema_message.luau edited by patch
// when set, and answers its one line.
func luauMessageStrictMode(t *testing.T, patch func([]byte) []byte) string {
	t.Helper()
	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "Probe.schema")
	if err := os.WriteFile(schemaPath, []byte("package probe\n\nfixed table Root {\n a int32\n b bool\n c uint8\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load([]string{schemaPath})
	if err != nil {
		t.Fatal(err)
	}
	files, err := luau.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := luautable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	if patch != nil {
		files["schema_message.luau"] = patch(files["schema_message.luau"])
	}
	out := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(out, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(out, "main.luau"), []byte(luauStrictModeProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(luaurun.Run(t, out, "main.luau", nil)))
}
