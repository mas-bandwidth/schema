package ci

// The Lua fuzz replay used to pass while replaying nothing (findings F-077,
// F-078): a saved case for a unit that was not generated, or for a kind with
// no reader, returned from the harness as if a reader had passed; a saved file
// that did not decode was left out of the count; an empty or missing replay
// directory reported "0 cases, 0 failed" and exited 0. The harness now refuses
// each, and make/schema-lua-nightly-fuzz.mk runs the committed controls under
// testdata/lua-nightly-fuzz/replay-controls before every replay. This test
// holds the three together in milliseconds, without a Lua interpreter: the
// harness carries each refusal, the recipe runs every committed control and
// nothing else, and each broken control differs from the healthy one in the
// one field it breaks.

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
)

const luaReplayControlsDir = "testdata/lua-nightly-fuzz/replay-controls"

// luaSavedCase is one line of a saved case, as the harness's decodeCase reads
// it: <kind> <unit> <table> <base64 payload>.
type luaSavedCase struct{ kind, unit, table, payload string }

var luaSavedCaseLine = regexp.MustCompile(`^(\S+)\s+(\S+)\s+(\S+)\s+(\S+)$`)

func decodeLuaSavedCase(text string) (luaSavedCase, bool) {
	m := luaSavedCaseLine.FindStringSubmatch(strings.TrimRight(text, " \t\r\n"))
	if m == nil {
		return luaSavedCase{}, false
	}
	if _, err := base64.StdEncoding.DecodeString(m[4]); err != nil {
		return luaSavedCase{}, false
	}
	return luaSavedCase{m[1], m[2], m[3], m[4]}, true
}

// luaReplayControlProblems checks the committed control directories (name ->
// file name -> contents) against the recipe's refused list and the corpus
// units the Makefile generates.
func luaReplayControlProblems(dirs map[string]map[string]string, refused []string, units []string) []string {
	var problems []string
	good, ok := dirs["good"]
	var base luaSavedCase
	switch {
	case !ok:
		problems = append(problems, "no good/ control: nothing shows the replay takes a healthy case")
	case len(good) != 1:
		problems = append(problems, fmt.Sprintf("good/ holds %d files; the recipe requires it to replay as exactly one case", len(good)))
	default:
		for _, text := range good {
			c, ok := decodeLuaSavedCase(text)
			if !ok {
				problems = append(problems, "good/ holds a case that does not decode")
			}
			base = c
		}
		if !slices.Contains(units, base.unit) {
			problems = append(problems, fmt.Sprintf("good/ names unit %q, which the Makefile's tables_generate block does not generate", base.unit))
		}
	}
	want := map[string]bool{"good": true}
	for _, name := range refused {
		want[name] = true
		if _, ok := dirs[name]; !ok {
			problems = append(problems, fmt.Sprintf("the recipe refuses %s/, which is not committed: the control replays a missing directory", name))
		}
	}
	for name := range dirs {
		if !want[name] {
			problems = append(problems, fmt.Sprintf("%s/ is committed and the recipe's LUA_FUZZ_REPLAY_REFUSED does not run it", name))
		}
	}
	// each broken control is the healthy case with the one field it breaks
	differs := func(name string, ok func(luaSavedCase) bool) {
		files, have := dirs[name]
		if !have || len(files) != 1 {
			problems = append(problems, fmt.Sprintf("%s/ holds %d files, want 1", name, len(files)))
			return
		}
		for _, text := range files {
			c, decoded := decodeLuaSavedCase(text)
			if !decoded || !ok(c) {
				problems = append(problems, fmt.Sprintf("%s/ is not the good case with only that field changed: %q", name, strings.TrimSpace(text)))
			}
		}
	}
	differs("unbound-unit", func(c luaSavedCase) bool {
		return c.unit != base.unit && !slices.Contains(units, c.unit) && c.kind == base.kind && c.table == base.table && c.payload == base.payload
	})
	differs("unsupported-kind", func(c luaSavedCase) bool {
		known := map[string]bool{"load": true, "strict": true, "text": true, "json": true}
		return !known[c.kind] && c.unit == base.unit && c.table == base.table && c.payload == base.payload
	})
	differs("unregistered-table", func(c luaSavedCase) bool {
		return c.table != base.table && c.kind == base.kind && c.unit == base.unit && c.payload == base.payload
	})
	if files, ok := dirs["unreadable-beside-good"]; ok {
		decoded, undecoded := 0, 0
		for _, text := range files {
			if c, ok := decodeLuaSavedCase(text); ok && c == base {
				decoded++
			} else if !ok {
				undecoded++
			}
		}
		if decoded != 1 || undecoded != 1 || len(files) != 2 {
			problems = append(problems, fmt.Sprintf("unreadable-beside-good/ holds %d good and %d undecodable of %d files, want 1 and 1 of 2", decoded, undecoded, len(files)))
		}
	}
	sort.Strings(problems)
	return problems
}

func readLuaReplayControls(t *testing.T, root string) map[string]map[string]string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(root, luaReplayControlsDir))
	if err != nil {
		t.Fatal(err)
	}
	dirs := map[string]map[string]string{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		files := map[string]string{}
		cases, err := os.ReadDir(filepath.Join(root, luaReplayControlsDir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range cases {
			data, err := os.ReadFile(filepath.Join(root, luaReplayControlsDir, e.Name(), c.Name()))
			if err != nil {
				t.Fatal(err)
			}
			files[c.Name()] = string(data)
		}
		dirs[e.Name()] = files
	}
	return dirs
}

func TestLuaFuzzReplayControls(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	mk := readCI(t, root, "make/schema-lua-nightly-fuzz.mk")

	// the harness refuses each way a replay used to read nothing
	harness := luaFuzzHarness(t, mk)
	for _, want := range []string{
		`was not generated, so no reader takes this case`,
		`no reader for kind`,
		`registers no table`,
		`the saved case does not decode`,
		`holds no saved case; nothing was replayed`,
		`is not a directory; nothing was replayed`,
	} {
		if !strings.Contains(harness, want) {
			t.Errorf("the embedded Lua fuzz harness no longer refuses with %q", want)
		}
	}
	// the controls run before every replay, and count what they ran
	for _, want := range []string{
		`schema-lua-fuzz-replay: schema-lua-fuzz-toolchain schema-lua-fuzz-negative-control schema-lua-fuzz-replay-controls`,
		`LUA_FUZZ_REPLAY_CONTROLS := ` + luaReplayControlsDir,
		`grep -qx 'lua fuzz replay: 1 cases, 0 failed' $$w/good.log`,
		`[ $$dirs -eq 5 ]`,
		`[ $$refused -eq 6 ]`,
	} {
		if !strings.Contains(mk, want) {
			t.Errorf("make/schema-lua-nightly-fuzz.mk no longer holds %q", want)
		}
	}
	m := regexp.MustCompile(`(?m)^LUA_FUZZ_REPLAY_REFUSED\s*:=\s*(.*)$`).FindStringSubmatch(mk)
	if m == nil {
		t.Fatal("make/schema-lua-nightly-fuzz.mk does not list LUA_FUZZ_REPLAY_REFUSED")
	}
	refused := strings.Fields(m[1])
	if len(refused) != 4 {
		t.Errorf("LUA_FUZZ_REPLAY_REFUSED lists %d controls; the recipe's counts are for 4", len(refused))
	}
	var units []string
	for _, dir := range tablesGenerateDirs(t, readCI(t, root, "Makefile")) {
		units = append(units, filepath.Base(dir))
	}
	dirs := readLuaReplayControls(t, root)
	if problems := luaReplayControlProblems(dirs, refused, units); len(problems) != 0 {
		t.Fatalf("the committed controls: %q", problems)
	}

	// negative controls: each broken copy of the committed set is named
	clone := func() map[string]map[string]string {
		out := map[string]map[string]string{}
		for name, files := range dirs {
			out[name] = map[string]string{}
			for f, text := range files {
				out[name][f] = text
			}
		}
		return out
	}
	for _, c := range []struct {
		name   string
		mutate func(map[string]map[string]string) []string
		want   string
	}{
		{"a control the recipe does not run", func(d map[string]map[string]string) []string {
			d["planted"] = map[string]string{"x.case": "json examples RootConfig e30=\n"}
			return refused
		}, "planted/ is committed and the recipe's LUA_FUZZ_REPLAY_REFUSED does not run it"},
		{"a control the recipe runs that is gone", func(d map[string]map[string]string) []string {
			delete(d, "unsupported-kind")
			return refused
		}, "the recipe refuses unsupported-kind/, which is not committed"},
		{"an unbound unit that is generated", func(d map[string]map[string]string) []string {
			d["unbound-unit"] = map[string]string{"ordinary.case": "json examples RootConfig e30=\n"}
			return refused
		}, "unbound-unit/ is not the good case with only that field changed"},
		{"no healthy case", func(d map[string]map[string]string) []string {
			delete(d, "good")
			return refused
		}, "no good/ control"},
		{"a good case for a unit nobody generates", func(d map[string]map[string]string) []string {
			d["good"] = map[string]string{"ordinary.case": "json nowhere RootConfig e30=\n"}
			return refused
		}, `good/ names unit "nowhere"`},
		{"an unreadable control with no undecodable file", func(d map[string]map[string]string) []string {
			d["unreadable-beside-good"]["unreadable.case"] = d["good"]["ordinary.case"]
			return refused
		}, "unreadable-beside-good/ holds 2 good and 0 undecodable"},
	} {
		d := clone()
		list := c.mutate(d)
		if got := strings.Join(luaReplayControlProblems(d, list, units), "\n"); !strings.Contains(got, c.want) {
			t.Errorf("%s: problems %q, want one holding %q", c.name, got, c.want)
		}
	}
}
