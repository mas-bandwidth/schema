package inputfamilies

import (
	"slices"
	"strings"
	"testing"
)

func repoTree(t *testing.T) Tree {
	t.Helper()
	tr, err := Load("../..")
	if err != nil {
		t.Fatal(err)
	}
	if len(tr.Files) == 0 || len(tr.Targets) == 0 || len(tr.GoFiles) == 0 {
		t.Fatalf("the tree loaded %d files, %d targets, %d Go files: nothing to judge", len(tr.Files), len(tr.Targets), len(tr.GoFiles))
	}
	return tr
}

// Every family has inputs on disk and a reader, and every driver of a family
// has the target that runs it.
func TestEveryFamilyIsReadAndEveryDriverRuns(t *testing.T) {
	t.Parallel()
	for _, b := range Check(repoTree(t)) {
		t.Error(b)
	}
}

// THE PLANTED CHANGE: one changed input of each family selects a row for
// every reader leg, and every leg with a driver for it is among them.
func TestAChangeToEachFamilySelectsEveryReaderLeg(t *testing.T) {
	t.Parallel()
	tr := repoTree(t)
	for _, f := range Families {
		var planted string
		for _, p := range tr.Files {
			if f.Matches(p) {
				planted = p
				break
			}
		}
		if planted == "" {
			t.Errorf("%s: no input on disk to plant a change in", f.Name)
			continue
		}
		s := Select(tr, []string{planted}, false, false)
		var got []string
		for _, r := range s.Rows {
			if r.Family == f.Name {
				got = append(got, r.Leg)
			}
		}
		want := f.Readers(tr)
		if len(want) == 0 || !slices.Equal(got, want) {
			t.Errorf("%s: a change to %s selected legs %v, want every reader %v", f.Name, planted, got, want)
		}
		for _, l := range f.DriverLegs(tr) {
			if !slices.Contains(got, l) {
				t.Errorf("%s: a change to %s did not select %s, which has a driver for it", f.Name, planted, l)
			}
		}
	}
}

// f2e1c41: the two .bits pins it added selected no leg. They select every
// packet-defaults reader in the pull-request tier, and the Go package that
// reads the constant-default pins.
func TestTheF2e1c41PinsSelectEveryPacketDefaultsReader(t *testing.T) {
	t.Parallel()
	tr := repoTree(t)
	s := Select(tr, []string{
		"testdata/wire/packet-defaults/hexadecimal-addition.bits",
		"testdata/wire/packet-defaults/unsigned-subtraction.bits",
	}, false, true)
	var legs []string
	for _, r := range s.Rows {
		if r.Family != "packet-defaults" || r.Target != "packet-defaults-"+r.Leg {
			t.Errorf("unexpected row %+v", r)
		}
		legs = append(legs, r.Leg)
	}
	for _, l := range []string{"cpp", "c", "go", "rust", "js", "cs", "unity", "java", "dart", "elixir", "lua", "luau", "moo", "squirrel", "goose", "python", "ruby"} {
		if !slices.Contains(legs, l) {
			t.Errorf("the f2e1c41 pins did not select %s", l)
		}
	}
	if !slices.Contains(s.Packages, "./test/conformance/packet") {
		t.Errorf("the f2e1c41 pins did not select ./test/conformance/packet: %v", s.Packages)
	}
}

// A push to main selects every pull-request-tier family with no change at
// all, and the pull-request tier leaves out a family the owner moved off it.
func TestPushToMainSelectsEveryFamily(t *testing.T) {
	t.Parallel()
	tr := repoTree(t)
	s := Select(tr, nil, true, true)
	fams := map[string]bool{}
	for _, r := range s.Rows {
		fams[r.Family] = true
	}
	for _, f := range Families {
		if f.PR != fams[f.Name] {
			t.Errorf("%s: PR=%v but a push to main selected rows=%v", f.Name, f.PR, fams[f.Name])
		}
	}
}

// The derivation on a tree built by hand: a driver without a target is a
// reader that never runs, an input nothing names is reported unread, and a
// Go file naming the input directory selects its package.
func TestDerivationOnAHandTree(t *testing.T) {
	t.Parallel()
	saved := Families
	t.Cleanup(func() { Families = saved })
	Families = []Family{{Name: "pins", PR: true, Inputs: []string{"testdata/pins/"}, Target: "pins-{leg}", Drivers: `^test/pins/([a-z]+)/`}}
	tr := Tree{
		Files:   []string{"testdata/pins/a.bin", "test/pins/lua/main.lua", "test/pins/moo/main.moo"},
		Targets: map[string]bool{"pins-lua": true},
		GoFiles: map[string]string{"internal/x/x_test.go": `const dir = "testdata/pins"`, "internal/y/y.go": "package y"},
	}
	bad := strings.Join(Check(tr), "\n")
	if !strings.Contains(bad, "leg moo has a driver but no pins-moo target") {
		t.Errorf("Check did not refuse the moo driver with no target:\n%s", bad)
	}
	s := Select(tr, []string{"testdata/pins/b.bin", "testdata/orphan/z.bin"}, false, true)
	if len(s.Rows) != 1 || s.Rows[0] != (Row{Leg: "lua", Family: "pins", Target: "pins-lua"}) {
		t.Errorf("rows = %+v", s.Rows)
	}
	if !slices.Equal(s.Packages, []string{"./internal/x"}) {
		t.Errorf("packages = %v", s.Packages)
	}
	if !slices.Equal(s.Unread, []string{"testdata/orphan/z.bin"}) {
		t.Errorf("unread = %v", s.Unread)
	}
}

func TestAccountFilesForLegResolvesPerLegSchemasAndExclusions(t *testing.T) {
	t.Parallel()
	tr := repoTree(t)

	// packet-defaults and packet-end account for *.bin only (not *.bits sidecars).
	for _, fam := range []string{"packet-defaults", "packet-end"} {
		files := AccountFilesForLeg(tr, fam, "python")
		if len(files) == 0 {
			t.Fatalf("%s: no accounted files", fam)
		}
		for _, f := range files {
			if strings.HasSuffix(f, ".bits") {
				t.Errorf("%s: unexpected .bits sidecar in accounted files: %s", fam, f)
			}
		}
	}

	// Per-leg schema families resolve each leg to its own schema file.
	wantSchemas := []struct {
		fam, leg, schema string
	}{
		{"packet-finite", "go", "test/packet-finite/Finite.schema"},
		{"packet-finite", "java", "test/java/Finite.schema"},
		{"packet-finite", "dart", "test/packet-finite/dart/Finite.schema"},
		{"packet-finite", "squirrel", "test/packet-finite/squirrel/Finite.schema"},
		{"packet-void", "cpp", "test/packet-void/Void.schema"},
		{"packet-void", "python", "test/packet-void/python/Python.schema"},
		{"packet-void", "ruby", "test/packet-void/ruby/Ruby.schema"},
		{"packet-write-text", "rust", "test/packet-write-text/Text.schema"},
		{"packet-write-text", "cpp", "test/packet-write-text/cpp/TextPacketCases.schema"},
		{"packet-write-text", "c", "test/packet-write-text/c/Text.schema"},
		{"packet-write-text", "dart", "test/packet-write-text/dart/PacketText.schema"},
		{"packet-write-text", "java", "test/packet-write-text/java/PacketText.schema"},
		{"packet-write-text", "js", "test/packet-write-text/js/Text.schema"},
	}
	for _, tc := range wantSchemas {
		got := AccountFilesForLeg(tr, tc.fam, tc.leg)
		if !slices.Contains(got, tc.schema) {
			t.Errorf("%s %s: expected %s in %v", tc.fam, tc.leg, tc.schema, got)
		}
	}

	// conformance-corpus excludes the two C++ wire-fuzz-only .bin files not in MANIFEST.txt.
	cc := AccountFilesForLeg(tr, "conformance-corpus", "go")
	for _, excl := range []string{
		"testdata/conformance/tables/wire/array_root_body_short.bin",
		"testdata/conformance/tables/wire/graph_widened_default.bin",
		"testdata/conformance/tables/wire/widenbound_msg.bin",
	} {
		if slices.Contains(cc, excl) {
			t.Errorf("conformance-corpus: excluded wire-fuzz file still present: %s", excl)
		}
	}
	if !slices.Contains(cc, "testdata/conformance/tables/MANIFEST.txt") || len(cc) < 100 {
		t.Errorf("conformance-corpus: unexpected accounted count %d", len(cc))
	}
}

func TestAcceptedBoundsSelectOnlyTheirGoReaders(t *testing.T) {
	t.Parallel()
	tree := repoTree(t)
	for _, tc := range []struct {
		family   string
		neighbor string
		inputs   []string
	}{
		{
			family:   "packet-cfloat-endpoints",
			neighbor: "packet-write-cfloat",
			inputs: []string{
				"test/packet-cfloat-endpoints/PacketRatio.schema",
				"test/packet-cfloat-endpoints/ratio-endpoints.txt",
			},
		},
		{
			family:   "table-write-bounds",
			neighbor: "conformance-corpus",
			inputs: []string{
				"testdata/conformance/table-write-bounds/ACCEPTED-WRITE-BOUNDS.txt",
				"testdata/conformance/table-write-bounds/TableWriteBounds.schema",
				"testdata/conformance/table-write-bounds/torture_var_wref.bin",
			},
		},
	} {
		t.Run(tc.family, func(t *testing.T) {
			if got := AccountFilesForLeg(tree, tc.family, "go"); !slices.Equal(got, tc.inputs) {
				t.Errorf("accounted files = %v, want %v", got, tc.inputs)
			}
			for _, input := range tc.inputs {
				want := []Row{{Leg: "go", Family: tc.family, Target: tc.family + "-go"}}
				if got := Select(tree, []string{input}, false, true).Rows; !slices.Equal(got, want) {
					t.Errorf("%s selected %v, want %v", input, got, want)
				}
				if slices.Contains(AccountFilesForLeg(tree, tc.neighbor, "cpp"), input) {
					t.Errorf("%s is accounted by neighboring family %s", input, tc.neighbor)
				}
			}
		})
	}
}

// A DOCUMENT selects the packages that read it, however the read spells the
// path: the whole path as one literal, filepath.Join's directory and name
// ("docs", "CI-TIERS.md"), or a glob over its directory (tools/roadmap's
// doneevidence over docs/roadmap-evidence). Each row is a diff of that one
// document and a package that must run (the reader's misses on #4934).
func TestADocumentSelectsTheGoPackagesThatReadIt(t *testing.T) {
	t.Parallel()
	tr := repoTree(t)
	// through Select once (the fast lane's own call), then the rows against
	// one scan of the tree
	if got := Select(tr, []string{"docs/PORTING.md"}, false, true).Packages; !slices.Contains(got, "./tools/matrix") {
		t.Errorf("Select: a docs/PORTING.md-only diff selects %v, not ./tools/matrix", got)
	}
	literals := literalsByPackage(tr)
	for _, c := range []struct{ doc, pkg string }{
		{"docs/PORTING.md", "./tools/matrix"},
		{"docs/CI-TIERS.md", "./internal/ci"},
		{"docs/BASE-REDS.md", "./internal/ci"},
		{"docs/VERSIONING.md", "./internal/ci"},
		{"docs/CLI.md", "./cmd/schema"},
		{"docs/PORT-LOBSTER.md", "./internal/codegen/lobster"},
		{"docs/PORT-PYTHON.md", "./internal/ci"},
		{"docs/PORT-RUBY.md", "./internal/ci"},
		{"docs/roadmap-evidence/fixed-c.sexp", "./tools/roadmap"},
		{"docs/SPEC-TABLES.md", "./internal/ci"},
		{"docs/SPEC-TABLES.md", "./test/conformance/harness"},
	} {
		if got := docReaders(literals, c.doc); !slices.Contains(got, c.pkg) {
			t.Errorf("a %s-only diff selects %v, not %s", c.doc, got, c.pkg)
		}
	}
	// a hand tree: each read spelling selects its package; a comment that
	// cites the document, even in Join's own spelling, selects nothing
	hand := Tree{Targets: map[string]bool{}, GoFiles: map[string]string{
		"tools/whole/w.go":   "package whole\nconst doc = \"docs/X.md\"\n",
		"tools/join/j.go":    "package join\nvar dir = \"docs\"\n",
		"tools/join/j2.go":   "package join\nvar name = \"X.md\"\n",
		"tools/glob/g.go":    "package glob\nvar a, b, c = \"docs\", \"sub\", \"*.sexp\"\n",
		"internal/cite/c.go": "package cite\n// the rule in docs/X.md; see filepath.Join(root, \"docs\", \"X.md\")\n// and \"docs\", \"sub\", \"*.sexp\"\nvar s = \"docs\"\n",
		"internal/star/s.go": "package star\nvar a, b, c = \"docs\", \"sub\", \"*\"\n",
	}}
	for doc, want := range map[string][]string{
		"docs/X.md":       {"./tools/join", "./tools/whole"},
		"docs/sub/y.sexp": {"./tools/glob"},
		"docs/sub/y.md":   nil,
	} {
		if got := Select(hand, []string{doc}, false, true).Packages; !slices.Equal(got, want) {
			t.Errorf("hand tree: a %s-only diff selects %v, want %v", doc, got, want)
		}
	}
}
