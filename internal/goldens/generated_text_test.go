package goldens

import (
	"bytes"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

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
var absolutePath = regexp.MustCompile(`(?m:^|[^A-Za-z0-9_.])(/Users/|/home/|/Volumes/|/private/|/tmp/|/var/folders/|/root/|[A-Za-z]:\\{1,2}Users\\)`)

// bannedNeedles: every match of bannedTerm is a casing of "peer", so its
// folded text holds foldKey("peer").
var bannedNeedles = [][]byte{foldKey([]byte("peer"))}

// absoluteNeedles: every match of absolutePath holds one of these bytes as
// written (absolutePath has no (?i)); the drive path's is its "Users\".
var absoluteNeedles = [][]byte{[]byte("/Users/"), []byte("/home/"), []byte("/Volumes/"), []byte("/private/"), []byte("/tmp/"), []byte("/var/folders/"), []byte("/root/"), []byte("Users\\")}

// A scanner is one rule: its expression, and needles at least one of which
// every match of the expression holds (in the folded text when folded). A
// text that holds none cannot match, and the expression does not run on it:
// run on every generated file it was two thirds of this test's time
// (TestGeneratedTextNamesNoMachine, 80 s of 113 s on amd-2, 2026-11-25). The
// prefilter only skips texts the expression cannot match, so find returns
// exactly what the expression's FindAllIndex does
// (TestAScannerFindsWhatItsExpressionFinds).
type scanner struct {
	re      *regexp.Regexp
	folded  bool
	needles [][]byte
}

// find is s.re.FindAllIndex(text, -1); folded is foldKey(text).
func (s scanner) find(text, folded []byte) [][]int {
	hay := text
	if s.folded {
		hay = folded
	}
	for _, n := range s.needles {
		if bytes.Contains(hay, n) {
			return s.re.FindAllIndex(text, -1)
		}
	}
	return nil
}

// machineScanner is the rule for one machine word: the word whole, in any
// casing (?i) matches, between non-word bytes or line ends.
func machineScanner(w string) scanner {
	return scanner{
		re:      regexp.MustCompile(`(?im)(^|[^A-Za-z0-9_])` + regexp.QuoteMeta(w) + `($|[^A-Za-z0-9_])`),
		folded:  true,
		needles: [][]byte{foldKey([]byte(w))},
	}
}

// foldKey maps each rune of b to the least rune of its simple case-folding
// orbit (unicode.SimpleFold), the equivalence (?i) matches by: 'k', 'K' and
// the Kelvin sign all become 'K', and 's', 'S' and the long s all become 'S'.
// A word (?i) matches in a text is then a substring of the folded text, as
// its folded word. A byte that is not UTF-8 is U+FFFD on both sides, as the
// regexp package reads it.
func foldKey(b []byte) []byte {
	out := make([]byte, 0, len(b))
	for len(b) > 0 {
		r, n := utf8.DecodeRune(b)
		b = b[n:]
		least := r
		for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
			least = min(least, f)
		}
		out = utf8.AppendRune(out, least)
	}
	return out
}

// textUnits is every testdata schema unit: the two corpora and every unit
// under tables/ that holds schema files.
func textUnits(t *testing.T) []string {
	t.Helper()
	units := []string{corpusDir, corpus128Dir}
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
	if len(units) == 2 {
		t.Fatal("no unit under tables/ holds a schema file: the table units moved, and every test over textUnits would read the two packet corpora alone")
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
	// a GitHub-hosted runner's account is the word "runner", which the emitted
	// runtimes use as an ordinary word (the fixed-form plan runner); its home
	// directory, below, still names that machine
	if u, err := user.Current(); err == nil && u.Username != "" &&
		(os.Getenv("RUNNER_ENVIRONMENT") != "github-hosted" || u.Username != "runner") {
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
	var machine []scanner
	for _, w := range machineWords(t) {
		if len(w) < 3 {
			continue
		}
		machine = append(machine, machineScanner(w))
	}
	banned := scanner{re: bannedTerm, folded: true, needles: bannedNeedles}
	absolute := scanner{re: absolutePath, needles: absoluteNeedles}
	units := textUnits(t)
	allTargets := schema.Targets()
	if len(allTargets) == 0 {
		t.Fatal("schema.Targets() is empty: the loop below would check nothing")
	}
	for _, target := range allTargets {
		// each target is its own subtest, run side by side: the work is
		// generating every unit and scanning its text, and no target's
		// output depends on another's
		t.Run(target, func(t *testing.T) {
			t.Parallel()
			emitted := 0
			scanned := 0
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
				scanned += len(names)
				for _, name := range names {
					text := files[name]
					folded := foldKey(text)
					where := target + " " + dir + " " + name
					report := func(m scanner, what string) {
						for _, at := range m.find(text, folded) {
							start := bytes.LastIndexByte(text[:at[0]], '\n') + 1
							end := bytes.IndexByte(text[at[0]:], '\n')
							if end < 0 {
								end = len(text) - at[0]
							}
							line := bytes.Count(text[:at[0]], []byte("\n")) + 1
							t.Errorf("%s:%d %s: %s", where, line, what, strings.TrimSpace(string(text[start:at[0]+end])))
						}
					}
					report(banned, "uses the banned term")
					report(absolute, "carries an absolute path")
					for _, m := range machine {
						report(m, "names the machine that ran the compiler")
					}
				}
			}
			if emitted == 0 {
				t.Errorf("target %s emitted no unit, so nothing of its output was checked", target)
			}
			if scanned == 0 {
				t.Errorf("target %s emitted no file across every unit, so nothing of its output was scanned", target)
			}
		})
	}
}

// TestAScannerFindsWhatItsExpressionFinds holds the prefilter to its
// expression: on every sample, each rule's find returns exactly the
// expression's own FindAllIndex, so skipping the expression on a text the
// needles rule out never hides a match. The samples are the casings (?i)
// matches beyond ASCII (the Kelvin sign for k, the long s for s), the banned
// term in each spelling, every absolute-path shape, and texts that hold none.
// Planted: foldKey as bytes.ToLower misses the long s and the Kelvin sign
// (red); absoluteNeedles without "Users\\" misses the drive path (red).
func TestAScannerFindsWhatItsExpressionFinds(t *testing.T) {
	t.Parallel()
	rules := map[string]scanner{
		"banned":   {re: bannedTerm, folded: true, needles: bannedNeedles},
		"absolute": {re: absolutePath, needles: absoluteNeedles},
		"host":     machineScanner("build-sis"),
		"user":     machineScanner("kelvin"),
		"path":     machineScanner("/srv/work/schema"),
	}
	samples := []string{
		"", "nothing to see", "peer", "PEER", "Peer", "pEeR", "a peer here", "xpeerx", "BuildPeerVersion", "TypeError",
		"open /Users/x/a", "\"/home/ci\"", "/Volumes/nova", "/private/var", "x=/tmp/y", "/var/folders/q", "/root/.cache",
		"C:\\Users\\me", "c:\\\\Users\\\\me", "D:Users\\x", "/usr/local", "usersx\\",
		"build-sis", "BUILD-SIS", "Build-Sis", "build-\u017fi\u017f", "x build-sis.", "xbuild-sis", "build-sisy",
		"kelvin", "KELVIN", "\u212aelvin", "a \u212aELVIN b", "kelvins",
		"/srv/work/schema", "/SRV/WORK/\u017fchema/sub", "at /srv/work/schema\n", "\xff/srv/work/schema\xfe",
		"line one\nPEER on two\n/home/x on three\nkelvin four",
	}
	for name, rule := range rules {
		hits := 0
		for _, s := range samples {
			text := []byte(s)
			want := rule.re.FindAllIndex(text, -1)
			got := rule.find(text, foldKey(text))
			hits += len(want)
			if fmt.Sprint(got) != fmt.Sprint(want) {
				t.Errorf("%s on %q: the prefiltered scan found %v, the expression %v", name, s, got, want)
			}
		}
		if hits == 0 {
			t.Errorf("%s matched no sample: the samples do not exercise it", name)
		}
	}
}
