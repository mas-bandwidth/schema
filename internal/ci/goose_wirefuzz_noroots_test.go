package ci

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestGooseWireFuzzHoldsNoRootArmsByName is CI-TOOLS-7: make/goose-wirefuzz.sh
// used to drop, unsaid and uncounted, any unit and arm whose driver announced
// no roots, so a driver that stopped presenting a unit's roots read green. An
// arm that answers "the leg has a codec for none" now passes only when
// wirefuzz-noroots.tsv carries it, and a carried row whose arm runs, or whose
// unit is gone, fails. The goose compiler and the harness are stand-ins (a
// script each); the script under test and the leg build script are the real
// ones. Reads and writes only its temporary directory.
func TestGooseWireFuzzHoldsNoRootArmsByName(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	tmp := t.TempDir()
	write := func(path, body string, mode os.FileMode) string {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), mode); err != nil {
			t.Fatal(err)
		}
		return path
	}
	// the goose stand-in builds a leg that is never run: the harness stand-in
	// answers for it
	goose := write(filepath.Join(tmp, "bin", "goose"), "#!/bin/sh\nprintf '#!/bin/sh\\nexit 0\\n' > wirefuzz && chmod +x wirefuzz\n", 0o755)
	// the harness stand-in: an arm named in $FAKE_NOROOTS has no root,
	// every other arm is green
	harness := write(filepath.Join(tmp, "bin", "harness"), `#!/bin/sh
while [ $# -gt 0 ]; do
	if [ "$1" = --driver ]; then set -- $2; unit=$2; arm=$3; break; fi
	shift
done
if grep -qx "$unit $arm" "$FAKE_NOROOTS"; then
	echo "harness: the leg has a codec for none of the 3 roots the corpus names"
	exit 1
fi
echo "wire-fuzz: seed 1, 3 seeds over 3 roots, 0 divergences"
`, 0o755)
	units := filepath.Join(tmp, "units")
	write(filepath.Join(units, "u1", "U1Table.goose"), "namespace u1;\n", 0o644)
	known := write(filepath.Join(tmp, "known.tsv"), "# none\n", 0o644)

	run := func(t *testing.T, noroots, carried string) (string, bool) {
		t.Helper()
		dir := t.TempDir()
		fake := write(filepath.Join(dir, "fake-noroots"), noroots, 0o644)
		list := write(filepath.Join(dir, "noroots.tsv"), "# unit\tarm\twhy\n"+carried, 0o644)
		cmd := exec.Command("sh", "make/goose-wirefuzz.sh", goose, harness, units, filepath.Join(dir, "work"), "1", "10", "plain")
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "FAKE_NOROOTS="+fake, "SCHEMA_GOOSE_WIREFUZZ_KNOWN="+known, "SCHEMA_GOOSE_WIREFUZZ_NOROOTS="+list, "GOOSE_FUZZ_JOBS=1")
		out, err := cmd.CombinedOutput()
		return string(out), err == nil
	}
	for _, c := range []struct {
		name, noroots, carried, want string
		ok                           bool
	}{
		{"every arm runs", "", "", "2 arms ran", true},
		{"an arm with no roots, carried", "u1 fixed\n", "u1\tfixed\ttest\n", "u1 (fixed arm): no roots, carried: test", true},
		{"an arm with no roots, not carried", "u1 fixed\n", "", "u1 (fixed arm) RED: its driver announces no roots", false},
		{"a carried arm that runs", "", "u1\tfixed\ttest\n", "u1 (fixed arm) runs, but", false},
		{"a carried unit that is gone", "", "gone\tbuilder\ttest\n", "carries gone (builder arm), which did not answer no roots", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			out, ok := run(t, c.noroots, c.carried)
			if ok != c.ok || !strings.Contains(out, c.want) {
				t.Fatalf("passed=%v, want %v and output carrying %q:\n%s", ok, c.ok, c.want, out)
			}
		})
	}
}
