// The docs test for the Dart table leg: a pure read over the repository's
// documentation and make files. It pins the claim that the Dart port now
// carries the id-table wire (form 1) and the fixed form, that USAGE.md's
// Dart usage example is present and marked, and that every target a Dart cell
// of PORTING.md names actually exists in make/dart.mk — so the register cannot
// name a target that is not there (the `tables-dart-alloc` claim that was
// false for a month is the case).
package darttable

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// repoRoot is the checkout root, three levels above this package
// (internal/codegen/darttable -> internal/codegen -> internal -> root).
func repoRoot() string {
	root, err := filepath.Abs("../../..")
	if err != nil {
		panic(err)
	}
	return root
}

func readRepo(t *testing.T, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot(), rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(data)
}

// The stale claim the card removes: that the Dart port carries no table wire
// at all. The leg now emits the id-table wire (form 1) as `<Package>Wire.dart`
// and the fixed form as `<Base>Fixed.dart` / `<Package>Fixed.dart`, so a
// sentence that says Dart has no wire is a lie the gate refuses.
//
// "There is no `<Base>Table.dart`" is NOT in the list: it is true and
// harmless — the wire library is named for the package (`<Package>Wire.dart`),
// not for the base — so the gate looks for absences of the wire itself, not
// for the file name.
var noWireClaims = []string{
	"and not the table wire",
	"no table-wire library",
	"Dart and JavaScript carry the FIXED form and nothing else",
	"neither has form 1 at all",
	"emits no `<Base>Table.dart`",
}

func TestDartTableDocsDescribeTheWireTheLegCarries(t *testing.T) {
	t.Parallel()
	for _, file := range []string{"docs/USAGE.md", "docs/SPEC.md", "docs/SPEC-TABLES.md", "docs/PORTING.md"} {
		doc := readRepo(t, file)
		// Scope the check to paragraphs that describe Dart. Other legs (Java,
		// Elixir, JavaScript) legitimately say they carry no table wire, so the
		// stale-claim check only applies where the paragraph is about Dart.
		for _, para := range strings.Split(doc, "\n\n") {
			if !regexp.MustCompile(`\b[Dd]art\b`).MatchString(para) {
				continue
			}
			norm := strings.Join(strings.Fields(para), " ")
			for _, claim := range noWireClaims {
				if strings.Contains(norm, strings.Join(strings.Fields(claim), " ")) {
					t.Errorf("%s still says %q — the Dart leg now carries the table wire", file, claim)
				}
			}
		}
	}

	usage := readRepo(t, "docs/USAGE.md")
	if !strings.Contains(usage, "<!-- dart-table-usage -->") {
		t.Error("docs/USAGE.md lacks the opening <!-- dart-table-usage --> marker")
	}
	if !strings.Contains(usage, "<!-- /dart-table-usage -->") {
		t.Error("docs/USAGE.md lacks the closing <!-- /dart-table-usage --> marker")
	}
	if strings.Index(usage, "<!-- dart-table-usage -->") > strings.Index(usage, "<!-- /dart-table-usage -->") {
		t.Error("docs/USAGE.md's dart-table-usage markers are out of order")
	}

	// Every make target a Dart cell of PORTING.md names must exist in
	// make/dart.mk: the register cannot name a target that is not there.
	porting := readRepo(t, "docs/PORTING.md")
	dart := readRepo(t, "make/dart.mk")
	targetRe := regexp.MustCompile("`tables-dart-[a-z0-9-]+`")
	seen := map[string]bool{}
	for _, target := range targetRe.FindAllString(porting, -1) {
		name := strings.Trim(target, "`")
		if seen[name] {
			continue
		}
		seen[name] = true
		if !regexp.MustCompile("(?m)^" + regexp.QuoteMeta(name) + ":").MatchString(dart) {
			t.Errorf("PORTING.md names %s, but make/dart.mk defines no such target", name)
		}
	}
	if len(seen) == 0 {
		t.Error("no tables-dart-* target found in PORTING.md — this gate proves nothing")
	}
}
