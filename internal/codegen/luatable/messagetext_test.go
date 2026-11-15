package luatable

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
)

// THE MESSAGE FORM READS ONLY WELL-FORMED TEXT (docs/SPEC-TABLES.md §3.3,
// docs/SPEC.md §5): a string payload that is not well-formed UTF-8, or that
// carries a zero byte, is damage, and the whole batch is discarded. A bytes(N)
// payload is not text and reads as it rides.

const luaMsgTextSchema = `package msgtext

fixed table Msg {
    s string(4)
    b bytes(4)
}
`

func TestLuaLoadMessagesTextIsWellFormed(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "msgtext.schema")
	if err := os.WriteFile(p, []byte(luaMsgTextSchema), 0o600); err != nil {
		t.Fatal(err)
	}
	u := luarun.LoadUnit(t, p)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	mod := ModuleName(u)

	// The lawful batch from the leg's own SaveMessages under the unit's own
	// announcement.
	emit := fmt.Sprintf(`local T = require(%q)
local function hex(s)
  return (s:gsub(".", function(c) return string.format("%%02x", string.byte(c)) end))
end
local vocab = {}
for i = 1, T.MessageEntries do vocab[i] = {} end
assert(T.AnnounceRead(vocab, T.Announcement, {}), "AnnounceRead")
local m = T.NewMsg()
m.s = "abcd"
m.b = "wxyz"
local w = assert(T.SaveMessages("Msg", vocab, { m }), "SaveMessages")
print("ANNOUNCE=" .. hex(T.Announcement))
print("WIRE=" .. hex(w))
`, mod)
	out, stderr, err := try(t, files, emit, nil)
	if err != nil {
		t.Fatalf("encoder failed: %v\n%s", err, stderr)
	}
	kv := map[string][]byte{}
	for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if ok {
			kv[k] = unhexTestBytes(t, v)
		}
	}
	wire := kv["WIRE"]
	if len(kv["ANNOUNCE"]) == 0 || len(wire) == 0 {
		t.Fatalf("encoder printed no announcement or wire: %q", out)
	}

	patch := func(from, to string) []byte {
		t.Helper()
		if bytes.Count(wire, []byte(from)) != 1 {
			t.Fatalf("the lawful wire %x does not carry %q exactly once", wire, from)
		}
		return bytes.Replace(wire, []byte(from), []byte(to), 1)
	}

	inputs := map[string][]byte{
		"announce":  kv["ANNOUNCE"],
		"lawful":    wire,
		"ff":        patch("abcd", "ab\xffd"),
		"zero":      patch("abcd", "ab\x00d"),
		"overlong":  patch("abcd", "a\xc0\xafd"),
		"surrogate": patch("abcd", "a\xed\xa0\x80"),
		"truncated": patch("abcd", "ab\xe2\x82"),
		"bytesff":   patch("wxyz", "w\xffyz"),
	}

	probe := fmt.Sprintf(`local T = require(%q)
local inputs = require("inputs")
local vocab = {}
for i = 1, T.MessageEntries do vocab[i] = {} end
assert(T.AnnounceRead(vocab, inputs.announce, {}), "AnnounceRead")

local function load(name)
  local r = {}
  local got = T.LoadMessages("Msg", vocab, inputs[name], r)
  return got, r
end

local got = load("lawful")
assert(got ~= nil and #got == 1, "the lawful batch is not READ")
assert(got[1].s == "abcd" and got[1].b == "wxyz", "the lawful batch read wrong values")

for _, name in ipairs({ "ff", "zero", "overlong", "surrogate", "truncated" }) do
  local bad = load(name)
  if bad ~= nil and #bad ~= 0 then
    print("ACCEPTED " .. name .. " as " .. string.format("%%q", tostring(bad[1].s)))
  else
    print("REFUSED " .. name)
  end
end

local gotB = load("bytesff")
assert(gotB ~= nil and #gotB == 1, "a bytes(4) payload carrying 0xff is not READ")
assert(gotB[1].s == "abcd" and gotB[1].b == "w\xffyz", "the bytes(4) payload read wrong values")
print("bytes-ok")
`, mod)
	out, stderr, err = try(t, files, probe, inputs)
	if err != nil {
		t.Fatalf("probe failed: %v\nout=%s\nstderr=%s", err, out, stderr)
	}
	for _, name := range []string{"ff", "zero", "overlong", "surrogate", "truncated"} {
		if !strings.Contains(out, "REFUSED "+name+"\n") {
			t.Errorf("LoadMessages must refuse the batch whose string payload is %s: out=%s", name, out)
		}
	}
	if !strings.Contains(out, "bytes-ok") {
		t.Errorf("the bytes(4) control did not run: out=%s", out)
	}
}
