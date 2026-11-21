package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	green      = "wire-fuzz: seed 1, 126 seeds over 126 roots, 5000 enumerated + 100000 random = 105000 mutants, 0 divergences, 3.1 s, 32258 mutants/s\n"
	oneAbsent  = "wire-fuzz: seed 1, 125 seeds over 126 roots, 1 seeds absent (roots the leg has no codec for), 5000 enumerated + 100000 random = 105000 mutants, 0 divergences, 3.1 s, 32258 mutants/s\n"
	noMutants  = "wire-fuzz: seed 1, 126 seeds over 126 roots, 0 enumerated + 0 random = 0 mutants, 0 divergences, 0.0 s, 0 mutants/s\n"
	noSeeds    = "wire-fuzz: seed 1, 0 seeds over 126 roots, 126 seeds absent (roots the leg has no codec for), 0 enumerated + 0 random = 0 mutants, 0 divergences, 0.0 s, 0 mutants/s\n"
	divergence = "harness: FAILED after 41 mutants: the report differs\n    corpus seed Scene (tables.Scene), pass enumerated #3, run seed 1, mutation flip\n"
)

func none() absentHold { return absentHold{count: -1} }

// TestEveryLegIsHeldToItsRoster: the rule that was Python's alone is every
// leg's. A Go run with zero mutants and a Go run with an absent seed were both
// `pass`; the reviewer's two inputs are the first two reds here, and the same
// inputs are red for Python, as they were.
func TestEveryLegIsHeldToItsRoster(t *testing.T) {
	for _, leg := range []string{"go", "python", "c", "squirrel"} {
		if got := score(leg, "plain", "1", green, 0, 3, 100000, "", "owed.txt", none(), "absent.txt"); got != "pass (105000 mutants, 3 s)" {
			t.Fatalf("CONTROL FAILED: %s: a whole green run scores %q", leg, got)
		}
		for _, tc := range []struct {
			name, log, want string
			status          int
		}{
			{"zero mutants", noMutants, "RED: empty wire-fuzz mutation count", 0},
			{"an absent seed", oneAbsent, "RED: incomplete wire-fuzz roster: 1 seeds absent", 0},
			{"no seed at all", noSeeds, "RED: empty wire-fuzz roster: 0 seeds over 126 roots", 0},
			{"exit 0 and no summary", "the driver printed something else\n", "RED: exit 0 and no summary line", 0},
			{"an empty log", "", "RED: exit 0 and no summary line", 0},
			{"a summary with no mutation count", "wire-fuzz: seed 1, 1 seeds over 1 roots, 0 divergences\n", "RED: missing wire-fuzz mutation count", 0},
			{"one mutant under a budget of 100000", "wire-fuzz: seed 1, 126 seeds over 126 roots, 0 enumerated + 1 random = 1 mutants, 0 divergences, 0.0 s\n", "RED: short wire-fuzz run: 1 random mutants ran and the gate asked for 100000", 0},
			{"the enumerated pass alone", "wire-fuzz: seed 1, 126 seeds over 126 roots, 5000 enumerated + 0 random = 5000 mutants, 0 divergences, 0.0 s\n", "RED: short wire-fuzz run: 0 random mutants ran and the gate asked for 100000", 0},
			{"no enumerated mutant", "wire-fuzz: seed 1, 126 seeds over 126 roots, 0 enumerated + 100000 random = 100000 mutants, 0 divergences, 0.0 s\n", "RED: no enumerated mutant ran", 0},
			{"counts that do not add up", "wire-fuzz: seed 1, 126 seeds over 126 roots, 5 enumerated + 100000 random = 7 mutants, 0 divergences, 0.0 s\n", "RED: the summary's mutation counts do not add up", 0},
			{"another seed's summary", strings.Replace(green, "seed 1,", "seed 2,", 1), "RED: the summary is not seed 1's", 0},
			{"the retain arm's summary", strings.Replace(green, "wire-fuzz:", "wire-fuzz retain:", 1), "RED: the summary is not the plain arm's", 0},
			{"a summary with divergences", "wire-fuzz: seed 1, 1 seeds over 1 roots, 1 enumerated + 1 random = 2 mutants, 3 divergences\n", "RED: exit 0 and the summary does not say 0 divergences", 0},
			{"a divergence", divergence, "RED: tables.Scene, pass enumerated #3, mutant 42: the report differs", 1},
			{"a green summary and a red exit", green, "RED: exit 1: ", 1},
		} {
			if got := score(leg, "plain", "1", tc.log, tc.status, 3, 100000, "", "owed.txt", none(), "absent.txt"); !strings.HasPrefix(got, tc.want) {
				t.Errorf("%s, %s: got %q, want %q", leg, tc.name, got, tc.want)
			}
		}
	}
}

// TestAbsentSeedsAreHeldByNameAndCount: a leg with no codec for some roots
// says so in the absent list, with the exact count; a different count, and a
// line whose leg now answers every root, are red.
func TestAbsentSeedsAreHeldByNameAndCount(t *testing.T) {
	held := absentHold{count: 1, why: "no variable class"}
	if got := score("lua", "plain", "1", oneAbsent, 0, 3, 100000, "", "owed.txt", held, "absent.txt"); got != "pass (105000 mutants, 3 s)" {
		t.Errorf("an absent seed held by name and count: %q", got)
	}
	if got := score("lua", "plain", "1", oneAbsent, 0, 3, 100000, "", "owed.txt", absentHold{count: 2, why: "w"}, "absent.txt"); !strings.Contains(got, "1 seeds absent and absent.txt holds 2") {
		t.Errorf("another count: %q", got)
	}
	if got := score("lua", "plain", "1", green, 0, 3, 100000, "", "owed.txt", held, "absent.txt"); !strings.Contains(got, "no seed is absent and absent.txt still holds 1") {
		t.Errorf("a stale line: %q", got)
	}
	path := filepath.Join(t.TempDir(), "absent.txt")
	if err := os.WriteFile(path, []byte("# leg arm count why\nlua plain 3 no variable class\nlua retain none\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if h, err := heldAbsent(path, "lua", "plain"); err != nil || h.count != 3 || h.why != "no variable class" {
		t.Errorf("a held line reads as %+v, %v", h, err)
	}
	if h, err := heldAbsent(path, "go", "plain"); err != nil || h.count != -1 {
		t.Errorf("a leg with no line reads as %+v, %v", h, err)
	}
	if _, err := heldAbsent(path, "lua", "retain"); err == nil {
		t.Error("a line with no count and reason was read")
	}
}

func TestOwedArms(t *testing.T) {
	green := strings.Replace(green, "wire-fuzz:", "wire-fuzz retain:", 1)
	noMutants := strings.Replace(noMutants, "wire-fuzz:", "wire-fuzz retain:", 1)
	if got := score("go", "retain", "1", green, 0, 3, 100000, "the retain arm is not written", "owed.txt", none(), "absent.txt"); got != "RED: owed but green (105000 mutants): remove 'go retain' from owed.txt" {
		t.Errorf("owed and green: %q", got)
	}
	if got := score("go", "retain", "1", divergence, 1, 3, 100000, "the retain arm is not written", "owed.txt", none(), "absent.txt"); !strings.HasPrefix(got, "owed: the retain arm is not written (runs red: tables.Scene") {
		t.Errorf("owed and red: %q", got)
	}
	// an owed arm that proves nothing is still owed, never green
	if got := score("go", "retain", "1", noMutants, 0, 3, 100000, "the retain arm is not written", "owed.txt", none(), "absent.txt"); !strings.HasPrefix(got, "owed: ") {
		t.Errorf("owed and empty: %q", got)
	}
}

func roster(t *testing.T, leg string) string {
	t.Helper()
	dir := t.TempDir()
	for _, arm := range []string{"plain", "retain"} {
		for _, seed := range []string{"1", "2", "3"} {
			row := leg + "\t" + arm + "\t" + seed + "\tpass (100000 mutants, 3 s)\n"
			if err := os.WriteFile(filepath.Join(dir, leg+"."+arm+"."+seed+".row"), []byte(row), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	return dir
}

// TestTheTableReadsTheRosterByName: the roster is every arm at every seed. A
// leg with five of its six rows printed "every arm passes" when the missing
// row was not the last one (the reviewer's go.plain.1.row); every missing row
// is red now, first, middle or last, and so is a row from another run.
func TestTheTableReadsTheRosterByName(t *testing.T) {
	arms, seeds := []string{"plain", "retain"}, []string{"1", "2", "3"}

	dir := roster(t, "go")
	out, ok, err := table("go", dir, arms, seeds, "100000")
	if err != nil || !ok || !strings.Contains(out, "all 6 rows of the roster are here (6 pass, 0 owed by name)") {
		t.Fatalf("CONTROL FAILED: a whole roster: ok=%v err=%v\n%s", ok, err, out)
	}

	for _, gone := range []string{"go.plain.1.row", "go.plain.3.row", "go.retain.3.row"} {
		dir := roster(t, "go")
		if err := os.Remove(filepath.Join(dir, gone)); err != nil {
			t.Fatal(err)
		}
		out, ok, err := table("go", dir, arms, seeds, "100000")
		if err != nil || ok || !strings.Contains(out, "RED: the run left no row ("+gone+")") || !strings.Contains(out, "RED on 1 of 6 rows") {
			t.Errorf("%s removed: ok=%v err=%v\n%s", gone, ok, err, out)
		}
	}

	for name, body := range map[string]string{
		"another run's row":  "go\tplain\t2\tpass (100000 mutants, 3 s)\n",
		"another leg's row":  "c\tplain\t1\tpass (100000 mutants, 3 s)\n",
		"an empty row":       "",
		"two rows in a file": "go\tplain\t1\tpass (1 mutants, 1 s)\ngo\tplain\t1\tpass (1 mutants, 1 s)\n",
		"no verdict":         "go\tplain\t1\tfine\n",
		"a red row":          "go\tplain\t1\tRED: exit 1: boom\n",
	} {
		dir := roster(t, "go")
		if err := os.WriteFile(filepath.Join(dir, "go.plain.1.row"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if out, ok, _ := table("go", dir, arms, seeds, "100000"); ok {
			t.Errorf("%s passed:\n%s", name, out)
		}
	}

	if _, ok, err := table("go", roster(t, "go"), nil, seeds, "100000"); ok || err == nil {
		t.Error("a roster of no arm passed")
	}
	if _, ok, err := table("go", roster(t, "go"), arms, nil, "100000"); ok || err == nil {
		t.Error("a roster of no seed passed")
	}

	owed := roster(t, "go")
	if err := os.WriteFile(filepath.Join(owed, "go.retain.2.row"), []byte("go\tretain\t2\towed: not written yet\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, ok, _ := table("go", owed, arms, seeds, "100000"); !ok || !strings.Contains(out, "(5 pass, 1 owed by name)") {
		t.Errorf("an owed row:\n%s", out)
	}
}

// TestWriteRow runs the row verb over files, as the run script does.
func TestWriteRow(t *testing.T) {
	dir := t.TempDir()
	owed, absent := filepath.Join(dir, "owed.txt"), filepath.Join(dir, "absent.txt")
	for _, p := range []string{owed, absent} {
		if err := os.WriteFile(p, []byte("# none\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	row := func(name, log string, noDriver bool, status int) string {
		t.Helper()
		if !noDriver {
			log = strings.Replace(log, "seed 1,", "seed "+strings.Split(name, ".")[2]+",", 1)
			if err := os.WriteFile(filepath.Join(dir, name+".log"), []byte(log), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		f := strings.Split(name, ".")
		if err := writeRow(f[0], f[1], f[2], dir, owed, absent, noDriver, status, 2, 100000); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(filepath.Join(dir, name+".row"))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	if got := row("go.plain.1", green, false, 0); got != "go\tplain\t1\tpass (105000 mutants, 2 s)\n" {
		t.Errorf("a green row: %q", got)
	}
	if got := row("go.plain.2", noMutants, false, 0); !strings.HasPrefix(got, "go\tplain\t2\tRED: empty wire-fuzz mutation count") {
		t.Errorf("a zero-mutant row: %q", got)
	}
	if got := row("go.plain.3", oneAbsent, false, 0); !strings.HasPrefix(got, "go\tplain\t3\tRED: incomplete wire-fuzz roster") {
		t.Errorf("an absent-seed row: %q", got)
	}
	if got := row("zz.plain.1", "", true, -1); got != "zz\tplain\t1\tRED: the leg has no wire-fuzz driver and the owed list does not name this arm\n" {
		t.Errorf("a leg with no driver: %q", got)
	}
}
