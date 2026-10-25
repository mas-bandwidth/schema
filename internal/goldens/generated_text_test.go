package goldens

import (
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
)

// bannedTerm is the word generated text never uses for the other side of a
// connection: it says client, server, writer, reader or endpoint instead. The
// three case-exact spellings catch it inside an identifier (BuildVersion...),
// and the word-start rule catches any other casing of it as a word; neither
// fires inside an unrelated identifier such as JavaScript's TypeError.
var bannedTerm = regexp.MustCompile(`peer|PEER|Peer|(?i:\bpeer)`)

// absolutePath is the shape of a machine-local absolute path: a home, volume
// or temporary directory, or a Windows drive path.
var absolutePath = regexp.MustCompile(`(^|[^A-Za-z0-9_.])(/Users/|/home/|/Volumes/|/private/|/tmp/|/var/folders/|/root/|[A-Za-z]:\\\\Users\\\\)`)

// textUnits is every testdata schema unit: the three corpora and every unit
// under tables/ that holds schema files.
func textUnits(t *testing.T) []string {
	t.Helper()
	units := []string{corpusDir, corpus128Dir, corpusWideDir}
	entries, err := os.ReadDir("../../tables")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		dir := filepath.Join("../../tables", e.Name())
		if found, _ := filepath.Glob(filepath.Join(dir, "*.schema")); e.IsDir() && len(found) > 0 {
			units = append(units, dir)
		}
	}
	return units
}

// machineWords is what names the machine running this test: its host name
// (whole and first label), its user name, and the absolute paths of the
// checkout, the home directory and the temporary directory.
func machineWords(t *testing.T) []string {
	t.Helper()
	var words []string
	if host, err := os.Hostname(); err == nil && host != "" {
		words = append(words, host)
		if i := strings.IndexByte(host, '.'); i > 0 {
			words = append(words, host[:i])
		}
	}
	if u, err := user.Current(); err == nil && u.Username != "" {
		words = append(words, u.Username)
	}
	if root, err := filepath.Abs("../.."); err == nil {
		words = append(words, root)
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		words = append(words, home)
	}
	words = append(words, os.TempDir())
	return words
}

// TestGeneratedTextNamesNoMachine generates every testdata schema unit for
// every target and holds the output to two rules: it never uses the banned
// term, and it never carries the name, user or an absolute path of the
// machine that ran the compiler. A unit a target refuses is skipped; every
// target must emit for at least one unit.
func TestGeneratedTextNamesNoMachine(t *testing.T) {
	var machine []*regexp.Regexp
	for _, w := range machineWords(t) {
		if len(w) < 3 {
			continue
		}
		machine = append(machine, regexp.MustCompile(`(?i)(^|[^A-Za-z0-9_])`+regexp.QuoteMeta(w)+`($|[^A-Za-z0-9_])`))
	}
	units := textUnits(t)
	for _, target := range schema.Targets() {
		emitted := 0
		for _, dir := range units {
			paths, err := compiler.GatherPaths([]string{dir})
			if err != nil {
				t.Fatal(err)
			}
			u, err := schema.Load(paths)
			if err != nil {
				t.Fatalf("%s does not compile: %v", dir, err)
			}
			files, err := schema.Generate(u, target, nil)
			if err != nil {
				continue
			}
			emitted++
			names := make([]string, 0, len(files))
			for name := range files {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				for i, line := range strings.Split(string(files[name]), "\n") {
					where := target + " " + dir + " " + name
					if bannedTerm.MatchString(line) {
						t.Errorf("%s:%d uses the banned term: %s", where, i+1, strings.TrimSpace(line))
					}
					if absolutePath.MatchString(line) {
						t.Errorf("%s:%d carries an absolute path: %s", where, i+1, strings.TrimSpace(line))
					}
					for _, m := range machine {
						if m.MatchString(line) {
							t.Errorf("%s:%d names the machine that ran the compiler: %s", where, i+1, strings.TrimSpace(line))
						}
					}
				}
			}
		}
		if emitted == 0 {
			t.Errorf("target %s emitted no unit, so nothing of its output was checked", target)
		}
	}
}
