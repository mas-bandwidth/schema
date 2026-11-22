package luatable

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
)

// TestLuaMessageStrictMode holds the OPT-IN STRICT MODE of the announcement
// read (docs/SPEC-TABLES.md §3.3, THE LANGUAGE-NEUTRAL SHAPE) to the Go
// reference's verdicts (internal/codegen/gotable/messageform_strict_test.go):
// with vocab.strict unset an endpoint of another build version reads clean;
// with it set the unit's own version reads clean and another is refused
// `build_version_mismatch`, setting no vocabulary, keeping the peer's build
// version, naming both versions (report.remote, report.own), never damage,
// and terminal: the next announcement is `second_announcement` and a batch is
// `no_vocabulary`.
func TestLuaMessageStrictMode(t *testing.T) {
	t.Parallel()
	if err := luaMessageStrictMode(t, nil); err != nil {
		t.Fatal(err)
	}
}

// TestLuaMessageStrictModeNegativeControl plants the old reader back (no
// strict check) in the emitted message.lua and requires the probe to go red.
func TestLuaMessageStrictModeNegativeControl(t *testing.T) {
	t.Parallel()
	const anchor = "  if vocab.strict and version ~= T.BuildVersion then\n"
	err := luaMessageStrictMode(t, func(src []byte) []byte {
		if strings.Count(string(src), anchor) != 1 {
			t.Fatalf("message.lua carries the strict check %d times, want 1", strings.Count(string(src), anchor))
		}
		return []byte(strings.Replace(string(src), anchor, "  if false then\n", 1))
	})
	if err == nil {
		t.Fatal("the reader without the strict check still passes the strict probe")
	}
	if !strings.Contains(err.Error(), "the strict read accepted another build version") {
		t.Fatalf("the planted reader failed for another reason: %v", err)
	}
}

func luaMessageStrictMode(t *testing.T, patch func([]byte) []byte) error {
	t.Helper()
	path := filepath.Join(t.TempDir(), "probe.schema")
	writeFile(t, path, "package probe\n\nfixed table Root {\n a int32\n b bool\n c uint8\n}\n")
	u := luarun.LoadUnit(t, path)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	if patch != nil {
		files["message.lua"] = patch(files["message.lua"])
	}
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	writeFile(t, filepath.Join(dir, "main.lua"), fmt.Sprintf(`local T = require(%q)
local own = T.BuildVersion
local remote = ~own
-- the unit's own announcement with its build version word replaced: byte 1
-- is the form, byte 2 the version's reference, byte 3 its kind (9)
assert(string.byte(T.Announcement, 3) == 9, "the announcement does not open with the build version")
local function endpoint(version)
  return T.Announcement:sub(1, 3) .. string.pack("<i8", version) .. T.Announcement:sub(12)
end

-- default_tolerant
local v, r = {}, {}
assert(T.AnnounceRead(v, endpoint(remote), r), "the tolerant read refused another build version")
assert(v.announced and v.buildVersion == remote and r.reason == nil, "tolerant read")

-- strict_same_version
v, r = { strict = true }, {}
assert(T.AnnounceRead(v, endpoint(own), r), "the strict read refused its own build version")
assert(v.announced and r.reason == nil and r.remote == nil, "strict same-version read")

-- strict_other_version, typed refusal
v, r = { strict = true }, {}
assert(not T.AnnounceRead(v, endpoint(remote), r), "the strict read accepted another build version")
assert(r.reason == "build_version_mismatch", "reason: " .. tostring(r.reason))
assert(not r.damaged, "the strict refusal is damage")
assert(r.remote == remote and r.own == own, "the refusal does not carry both versions")
assert(not v.announced and v.count == nil, "the strict refusal set a vocabulary")
assert(v.buildVersion == remote, "the peer's build version was not kept")
assert((r.unknown or 0) == 0, "the strict refusal moved a counter")

-- terminal
local second = {}
assert(not T.AnnounceRead(v, endpoint(own), second), "an announcement after a strict refusal was read")
assert(second.reason == "second_announcement", "after a strict refusal: " .. tostring(second.reason))
local mine = { strict = true }
assert(T.AnnounceRead(mine, T.Announcement, {}))
local batch = T.SaveMessages("Root", mine, { T.NewRoot() })
assert(batch, "save")
local body = {}
local values = T.LoadMessages("Root", v, batch, body)
assert((values == nil or #values == 0) and body.reason == "no_vocabulary", "a batch on the refused connection: " .. tostring(body.reason))
print("ok")
`, ModuleName(u)))
	_, stderr, err := luarun.Try(t, dir, "main.lua", nil)
	if err != nil {
		return fmt.Errorf("%w: %s", err, stderr)
	}
	return nil
}
