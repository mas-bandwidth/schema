package ci

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestCppCodecRosterCarriesEveryJsonHostileRoot holds the C++ codec roster
// (test/conformance/cpp/codecs.h) to the corpus manifest: every `json-hostile`
// row of testdata/conformance/tables/MANIFEST.txt names a (unit, root) the
// roster carries. The hostile-value gate (`make tables-hostile-values`,
// test/tables/hostile_main.cpp) and the conformance driver both dispatch on
// that roster and refuse a row it does not carry, by name.
//
// The gate itself compiles every unit the roster names, plain and sanitized,
// which is past the two-minute per-PR budget, so it runs nightly (ci-full's
// base-hostile control row and certify's core-tables-3 shard). Until
// 2026-11-24 it read a hand-copied root list, and three landings added
// json-hostile rows on roots that list did not name: 40 of 316 rows red, seen
// by no per-PR tier because none runs the gate. This test is the per-PR half
// of that check, from the two source files alone, with no compiler and no run.
func TestCppCodecRosterCarriesEveryJsonHostileRoot(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	manifest, err := os.ReadFile(filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	roster, err := os.ReadFile(filepath.Join(root, "test", "conformance", "cpp", "codecs.h"))
	if err != nil {
		t.Fatal(err)
	}
	gaps, rows, err := jsonHostileRosterGaps(manifest, roster)
	if err != nil {
		t.Fatal(err)
	}
	if rows == 0 {
		t.Fatal("the manifest names no json-hostile row: the parse is wrong, and a check over nothing proves nothing")
	}
	for _, gap := range gaps {
		t.Error(gap)
	}

	// THE PLANTS. The same check over a manifest with one row on a root the
	// roster does not carry, and over a roster with one carried root taken
	// out, must name exactly what was planted: a check that cannot be made to
	// fail is not a check.
	t.Run("planted manifest row on an unrostered root", func(t *testing.T) {
		planted := append(append([]byte{}, manifest...), []byte("\njson-hostile planted-unrostered-root tabledemo PlantedNoSuchRoot testdata/conformance/tables/json-hostile/none read\n")...)
		gaps, _, err := jsonHostileRosterGaps(planted, roster)
		if err != nil {
			t.Fatal(err)
		}
		if len(gaps) != 1 || !strings.Contains(gaps[0], "planted-unrostered-root") || !strings.Contains(gaps[0], "tabledemo.PlantedNoSuchRoot") {
			t.Fatalf("the planted row was not the one gap named: %q", gaps)
		}
	})
	t.Run("planted roster without shapes.Bounds", func(t *testing.T) {
		line := regexp.MustCompile(`(?m)^[ \t]*VARCODEC\(\s*"shapes"\s*,\s*shapes\s*,\s*Bounds\s*\),[^\n]*\n`)
		if len(line.FindAll(roster, -1)) != 1 {
			t.Fatal("the roster does not carry shapes.Bounds exactly once: the plant has nothing to take out")
		}
		planted := line.ReplaceAll(roster, nil)
		gaps, _, err := jsonHostileRosterGaps(manifest, planted)
		if err != nil {
			t.Fatal(err)
		}
		want := 0
		for _, f := range jsonHostileRows(manifest) {
			if f[2] == "shapes" && f[3] == "Bounds" {
				want++
			}
		}
		if want == 0 || len(gaps) != want {
			t.Fatalf("taking shapes.Bounds out named %d gaps, want one per shapes.Bounds row (%d): %q", len(gaps), want, gaps)
		}
		for _, gap := range gaps {
			if !strings.Contains(gap, "shapes.Bounds") {
				t.Fatalf("a gap names another root: %q", gap)
			}
		}
	})
}

// jsonHostileRows answers the fields of every json-hostile row, in manifest order.
func jsonHostileRows(manifest []byte) [][]string {
	var rows [][]string
	sc := bufio.NewScanner(bytes.NewReader(manifest))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) > 0 && f[0] == "json-hostile" {
			rows = append(rows, f)
		}
	}
	return rows
}

// jsonHostileRosterGaps names every json-hostile row whose (unit, root) no
// CODEC, NOFILE_CODEC or VARCODEC row of the roster carries, and answers how
// many json-hostile rows there were. A row too short to name its unit and root
// is an error, never passed over.
func jsonHostileRosterGaps(manifest, roster []byte) ([]string, int, error) {
	registration := regexp.MustCompile(`(?m)^[ \t]*(?:NOFILE_CODEC|VARCODEC|CODEC)\(\s*"([^"]+)"\s*,\s*(?:\w+::)*\w+\s*,\s*(\w+)\s*\),`)
	carried := map[[2]string]bool{}
	for _, row := range registration.FindAllSubmatch(roster, -1) {
		carried[[2]string{string(row[1]), string(row[2])}] = true
	}
	if len(carried) == 0 {
		return nil, 0, fmt.Errorf("the roster carries no CODEC, NOFILE_CODEC or VARCODEC row: the parse is wrong")
	}
	rows := jsonHostileRows(manifest)
	var gaps []string
	for _, f := range rows {
		if len(f) < 6 {
			return nil, len(rows), fmt.Errorf("a json-hostile row names no unit and root: %q", strings.Join(f, " "))
		}
		if !carried[[2]string{f[2], f[3]}] {
			gaps = append(gaps, fmt.Sprintf("json-hostile %s names %s.%s, which the C++ codec roster (test/conformance/cpp/codecs.h) does not carry: add its CODEC or VARCODEC row", f[1], f[2], f[3]))
		}
	}
	return gaps, len(rows), nil
}
