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
