package ci

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/testinputs"
)

// TestGooseWireFuzzArmsComeFromTheCorpus is CI-TOOLS-7: make/goose-wirefuzz.sh
// once dropped, unsaid and uncounted, any unit and arm whose driver announced
// no roots, and later passed such an arm when a hand list
// (wirefuzz-noroots.tsv) carried it by name. Now the arms of each unit are read
// from the harness's own roster (wire-fuzz --list-roots): an arm runs for every
// unit the corpus names a root of its class for, an arm that runs and whose
// driver announces no root is red, and no list holds an arm back. The goose
// compiler and the harness are stand-ins (a script each); the script under
// test and the leg build script are the real ones. Reads and writes only its
// temporary directory.
func TestGooseWireFuzzArmsComeFromTheCorpus(t *testing.T) {
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
	// the harness stand-in: --list-roots prints the roster file of the
	// profile its flags name, an arm named in $FAKE_NOROOTS has no root, and
	// every other arm is green
	harness := write(filepath.Join(tmp, "bin", "harness"), `#!/bin/sh
profile=plain list=0
for a in "$@"; do
	case "$a" in
	--list-roots) list=1 ;;
	--builder) [ "$profile" = plain ] && profile=builder ;;
	--retain) profile=retain ;;
	esac
done
if [ $list -eq 1 ]; then cat "$FAKE_ROSTER.$profile"; exit 0; fi
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
	write(filepath.Join(units, "u2", "U2Table.goose"), "namespace u2;\n", 0o644)

	const (
		bothPlain    = "u1\tA\tfixed\tfile\nu1\tB\tvariable\tfile\nu2\tC\tfixed\tfile\n"
		bothBuilder  = "u1\tB\tvariable\tfile\n"
		noFixedPlain = "u1\tB\tvariable\tfile\n"
		onlyU1Plain  = "u1\tA\tfixed\tfile\nu1\tB\tvariable\tfile\n"
	)
	run := func(t *testing.T, plain, builder, noroots string) (string, bool) {
		t.Helper()
		dir := t.TempDir()
		roster := filepath.Join(dir, "roster")
		write(roster+".plain", plain, 0o644)
		write(roster+".builder", builder, 0o644)
		write(roster+".retain", builder, 0o644)
		fake := write(filepath.Join(dir, "fake-noroots"), noroots, 0o644)
		testinputs.Files(t, "make/goose-wirefuzz.sh", "make/goose-wirefuzz-build.sh")
		cmd := exec.Command("sh", "make/goose-wirefuzz.sh", goose, harness, units, filepath.Join(dir, "work"), "1", "10", "plain")
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "FAKE_ROSTER="+roster, "FAKE_NOROOTS="+fake, "GOOSE_FUZZ_JOBS=1")
		out, err := cmd.CombinedOutput()
		return string(out), err == nil
	}
	for _, c := range []struct {
		name, plain, builder, noroots, want, not string
		ok                                       bool
	}{
		{"every arm the corpus names runs", bothPlain, bothBuilder, "", "3 arms ran, all green", "", true},
		{"a unit with no root of an arm's class has no such arm", bothPlain, bothBuilder, "", "u2 (fixed arm)", "u2 (builder arm)", true},
		{"an arm the corpus names whose driver announces no root is red", bothPlain, bothBuilder, "u1 fixed\n", "u1 (fixed arm) RED", "", false},
		{"a unit the corpus names no root of says so", onlyU1Plain, bothBuilder, "", "u2: the corpus names no root of this unit", "u2 (", true},
		{"an arm the corpus names no root for at all is refused", noFixedPlain, bothBuilder, "", "the corpus names no root for the fixed arm", "arms ran", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			out, ok := run(t, c.plain, c.builder, c.noroots)
			if ok != c.ok || !strings.Contains(out, c.want) || (c.not != "" && strings.Contains(out, c.not)) {
				t.Fatalf("passed=%v, want %v, output carrying %q and not %q:\n%s", ok, c.ok, c.want, c.not, out)
			}
		})
	}

	// the hand lists that held arms back are retired and stay gone
	for _, gone := range []string{"test/conformance/goose/wirefuzz-noroots.tsv", "test/conformance/goose/wirefuzz-known.tsv"} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(gone))); err == nil {
			t.Errorf("%s is back: a list that holds wire-fuzz arms back by name is retired", gone)
		}
	}
	script, err := os.ReadFile(filepath.Join(root, "make", "goose-wirefuzz.sh"))
	if err != nil {
		t.Fatal(err)
	}
	for _, gone := range []string{"wirefuzz-noroots", "wirefuzz-known", "SCHEMA_GOOSE_WIREFUZZ_NOROOTS", "SCHEMA_GOOSE_WIREFUZZ_KNOWN"} {
		if strings.Contains(string(script), gone) {
			t.Errorf("make/goose-wirefuzz.sh reads %s again: a list that holds wire-fuzz arms back by name is retired", gone)
		}
	}
}
