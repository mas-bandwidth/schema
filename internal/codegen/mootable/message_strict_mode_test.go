package mootable_test

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestMooMessageStrictMode holds the OPT-IN STRICT MODE of the announcement
// read (docs/SPEC-TABLES.md §3.3, THE LANGUAGE-NEUTRAL SHAPE) to the Go
// reference's verdicts (internal/codegen/gotable/messageform_strict_test.go):
// with `strict` unset an endpoint of another build version reads clean; with
// it set the unit's own version reads clean and another is refused
// `build_version_mismatch`, setting no vocabulary, keeping the peer's build
// version, naming both versions as Uint64 values (report.remote,
// report.own), never damage, and terminal: the next announcement is
// `second_announcement` and a batch is `no_vocabulary`.
func TestMooMessageStrictMode(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table_message.moo)")
	t.Parallel()
	g, inputs, want := mooStrictModeSetup(t)
	if got := g.run(t, mooStrictModeProbe(g), inputs); got != want {
		t.Fatalf("strict probe:\n%s\nwant:\n%s", got, want)
	}
}

// TestMooMessageStrictModeNegativeControl plants the old reader back (no
// strict check) in table_message.moo and requires the probe to go red.
func TestMooMessageStrictModeNegativeControl(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table_message.moo)")
	t.Parallel()
	g, inputs, want := mooStrictModeSetup(t)
	const anchor = "\tif v.strict and version ~= homeBuild then\n"
	src := string(g.files["table_message.moo"])
	if strings.Count(src, anchor) != 1 {
		t.Fatalf("table_message.moo carries the strict check %d times, want 1", strings.Count(src, anchor))
	}
	if err := os.WriteFile(filepath.Join(g.dir, "table_message.moo"), []byte(strings.Replace(src, anchor, "\tif false then\n", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := g.run(t, mooStrictModeProbe(g), inputs); got == want {
		t.Fatal("the reader without the strict check still passes the strict probe")
	}
}

// mooStrictModeSetup generates the probe unit and the two announcements: the
// unit's own, and the same bytes with the build version word (bytes 3 to 10:
// the form, the version's reference and its kind come first) replaced.
func mooStrictModeSetup(t *testing.T) (*generatedUnit, map[string][]byte, string) {
	t.Helper()
	g := generateFiles(t, map[string]string{"Strict.schema": "package strictprobe\n\nfixed table Root {\n a int32\n b bool\n c uint8\n}\n"})
	own := ir.BuildVersion(g.unit)
	remote := ^own
	mine := tablewire.Announce(g.unit)
	if mine[2] != 9 || binary.LittleEndian.Uint64(mine[3:]) != own {
		t.Fatal("the announcement does not open with the build version")
	}
	other := append([]byte(nil), mine...)
	binary.LittleEndian.PutUint64(other[3:], remote)
	inputs := map[string][]byte{"own": mine, "remote": other}
	ownText, remoteText := strconv.FormatUint(own, 10), strconv.FormatUint(remote, 10)
	want := strings.Join([]string{
		"default true announced=true kept=remote reason=nil",
		"strict-same true announced=true reason=nil remote=nil",
		"strict-other false announced=false count=0 kept=remote reason=build_version_mismatch damaged=false unknown=0 remote=" + remoteText + " own=" + ownText,
		"terminal false reason=second_announcement",
		"batch 0 reason=no_vocabulary",
	}, "\n")
	return g, inputs, want
}

func mooStrictModeProbe(g *generatedUnit) string {
	return `--!strict
local inputs = require("./inputs")
local MS = require("./table_message")
` + g.requireAll() + `
local function kept(v: any): string
	if v.buildVersion == buffer.readstring(inputs.remote, 3, 8) then return "remote" end
	return "other"
end

-- default_tolerant
local v = MS.NewVocabulary()
local r: any = {}
local ok = MS.AnnounceRead(v, inputs.remote, r)
print("default " .. tostring(ok) .. " announced=" .. tostring(v.announced) .. " kept=" .. kept(v) .. " reason=" .. tostring(r.reason))

-- strict_same_version
v = MS.NewVocabulary()
v.strict = true
r = {}
ok = MS.AnnounceRead(v, inputs.own, r)
print("strict-same " .. tostring(ok) .. " announced=" .. tostring(v.announced) .. " reason=" .. tostring(r.reason) .. " remote=" .. tostring(r.remote))

-- strict_other_version, typed refusal
v = MS.NewVocabulary()
v.strict = true
r = {}
ok = MS.AnnounceRead(v, inputs.remote, r)
print("strict-other " .. tostring(ok) .. " announced=" .. tostring(v.announced) .. " count=" .. tostring(v.count) .. " kept=" .. kept(v) .. " reason=" .. tostring(r.reason) .. " damaged=" .. tostring(r.damaged) .. " unknown=" .. tostring(r.unknown) .. " remote=" .. tostring(r.remote) .. " own=" .. tostring(r.own))

-- terminal
local second: any = {}
ok = MS.AnnounceRead(v, inputs.own, second)
print("terminal " .. tostring(ok) .. " reason=" .. tostring(second.reason))
local body: any = {}
local count = MS.LoadMessages(v, "Root", buffer.fromstring("\2\0"), 1, body)
print("batch " .. tostring(count) .. " reason=" .. tostring(body.reason))
`
}
