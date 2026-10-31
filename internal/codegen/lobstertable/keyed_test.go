package lobstertable_test

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
)

// THE KEY ACCESSOR (docs/PORTING.md M5): an enum-keyed array stays a plain
// vector with key k in slot k-1, and the generated <Owner><Field>Slot, At and
// Each take the key. These tests run tabledemo's ScoreBoard.per_team ([Team]
// int32) and KeyedConfig.teams ([Team]TeamConfig) under the pinned toolchain
// with --runtime-no-asserts, so a refusal that leaned on an assert would not
// be one.

// keyedProgram answers one case named by its first argument: "use" prints the
// rows a correct accessor answers; "none", "past" and "negative" each ask the
// slot of one key the accessor must refuse, and print what came back if it
// did not.
const keyedProgram = `let c = command_line_arguments()[0]
if c == "none":
    print("slot {ScoreBoardPerTeamSlot(TeamNone)}")
elif c == "past":
    print("slot {ScoreBoardPerTeamSlot(Team(TeamMax + 1))}")
elif c == "negative":
    print("slot {ScoreBoardPerTeamSlot(Team(-1))}")
else:
    let sb = NewScoreBoard()
    sb.PerTeam[ScoreBoardPerTeamSlot(TeamRed)] = 10
    sb.PerTeam[ScoreBoardPerTeamSlot(TeamBlue)] = 20
    sb.PerTeam[ScoreBoardPerTeamSlot(TeamGreen)] = 30
    print("row set places by key|{sb.PerTeam}")
    print("row max is reachable|{ScoreBoardPerTeamSlot(TeamGreen)}")
    print("row at reads by key|{ScoreBoardPerTeamAt(sb, TeamBlue)}")
    var each = ""
    ScoreBoardPerTeamEach(sb) key, e:
        each += "{int(key)}={e},"
    print("row each yields key beside element|" + each)
    let kc = NewKeyedConfig()
    kc.Teams[KeyedConfigTeamsSlot(TeamBlue)].SpawnCount = 9
    print("row a table element by key|{KeyedConfigTeamsAt(kc, TeamBlue).SpawnCount} {kc.Teams[1].SpawnCount}")
`

// keyedUse is what the "use" case prints on a correct accessor.
var keyedUse = map[string]string{
	"set places by key":              "[10, 20, 30]",
	"max is reachable":               "2",
	"at reads by key":                "20",
	"each yields key beside element": "1=10,2=20,3=30,",
	"a table element by key":         "9 9",
}

// keyedRefusals are the refusal cases and the name each must carry.
var keyedRefusals = map[string]string{
	"none":     "keyed key refused: ScoreBoard.per_team key 0",
	"past":     "keyed key refused: ScoreBoard.per_team key 4",
	"negative": "keyed key refused: ScoreBoard.per_team key -1",
}

// keyedRows generates tabledemo, applies patch (when set) to the emitted
// descriptors.lobster, runs every case and returns one verdict per row: the
// use rows by their printed value, the refusal rows as "refused by name" or
// what the program answered instead.
func keyedRows(t *testing.T, patch func(string) string) map[string]string {
	t.Helper()
	units, _ := readManifest(t)
	paths, err := compiler.GatherPaths(prefix(units["tabledemo"]))
	if err != nil {
		t.Fatal(err)
	}
	g, err := generateLoadUnit(t, paths)
	if err != nil {
		t.Fatalf("tabledemo is refused: %v", err)
	}
	if patch != nil {
		path := filepath.Join(g.dir, "descriptors.lobster")
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(patch(string(src))), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var b strings.Builder
	for _, m := range g.mods {
		fmt.Fprintf(&b, "import .%s\n", m)
	}
	b.WriteString("import .Keyed\nnamespace tabledemo\n")
	b.WriteString(keyedProgram)
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	rows := map[string]string{}
	for _, c := range []string{"use", "none", "past", "negative"} {
		func() {
			out, code := lobsterrun.RunExitFlags(t, g.dir, "main.lobster", []string{"--runtime-no-asserts"}, nil, c)
			if c == "use" {
				for line := range strings.SplitSeq(string(out), "\n") {
					if name, v, ok := strings.Cut(strings.TrimPrefix(line, "row "), "|"); ok && strings.HasPrefix(line, "row ") {
						rows[name] = v
					}
				}
				if code != 0 {
					rows["use exits clean"] = fmt.Sprintf("exit %d: %s", code, out)
				}
				return
			}
			if code != 0 && strings.Contains(string(out), keyedRefusals[c]) {
				rows[c+" refused"] = "refused by name"
			} else {
				rows[c+" refused"] = fmt.Sprintf("exit %d: %s", code, strings.TrimSpace(string(out)))
			}
		}()
	}
	return rows
}

// TestLobsterKeyedAccessorCoversBothEnds holds the accessor to the method:
// None, a key past Max and a negative key refused by name with asserts
// compiled away, Max reachable, a write and a read by key landing in slot
// key-1, and the iteration yielding each key beside its element.
func TestLobsterKeyedAccessorCoversBothEnds(t *testing.T) {
	t.Parallel()
	rows := keyedRows(t, nil)
	want := map[string]string{}
	maps.Copy(want, keyedUse)
	for c := range keyedRefusals {
		want[c+" refused"] = "refused by name"
	}
	for name, w := range want {
		if rows[name] != w {
			t.Errorf("%s: got %q, want %q", name, rows[name], w)
		}
	}
	for name := range rows {
		if _, ok := want[name]; !ok {
			t.Errorf("unexpected row %s: %q", name, rows[name])
		}
	}
}

// TestLobsterKeyedNegativeControls is the reversed witness, one plant per
// rule of the method in the emitted runtime's keyed_slot, each of which must
// turn exactly its own rows: the None end of the guard removed, the past-Max
// end removed, and the shift misplaced (key k lands in slot k mod Max).
func TestLobsterKeyedNegativeControls(t *testing.T) {
	t.Parallel()
	const guard = "    if not ult(key - 1, max):\n"
	const shift = "    return key - 1\n"
	cases := []struct {
		name, from, to string
		red            []string
	}{
		{"none guard removed", guard, "    if key > max:\n", []string{"none refused", "negative refused"}},
		{"past max guard removed", guard, "    if key < 1:\n", []string{"past refused"}},
		{"key misplaced by one", shift, "    return key % max\n", []string{"set places by key", "max is reachable", "each yields key beside element", "a table element by key"}},
	}
	clean := keyedRows(t, nil)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := keyedRows(t, func(src string) string {
				if n := strings.Count(src, c.from); n != 1 {
					t.Errorf("%s: the anchor %q is in the emitted runtime %d times, want 1", c.name, c.from, n)
				}
				return strings.Replace(src, c.from, c.to, 1)
			})
			want := map[string]bool{}
			for _, r := range c.red {
				want[r] = true
			}
			for name := range clean {
				turned := got[name] != clean[name]
				if want[name] && !turned {
					t.Errorf("%s: row %q stayed green", c.name, name)
				}
				if !want[name] && turned {
					t.Errorf("%s: row %q turned (%q) and is not this plant's", c.name, name, got[name])
				}
			}
		})
	}
}
