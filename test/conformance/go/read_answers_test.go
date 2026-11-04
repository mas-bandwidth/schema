package main

import (
	"blobdemo"
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// A failed reader may leave caller storage populated. The protocol must never
// publish that storage, and successful evolution diagnostics must not refuse it.
func TestReadAnswersCarryOnlyAcceptedValues(t *testing.T) {
	old := codecTable
	t.Cleanup(func() { codecTable = old })
	const canonical = "{\"n\":7}\n"
	for _, accepted := range []bool{false, true} {
		for _, surface := range []struct {
			name string
			run  func([]line, string) error
		}{{"report", surfaceReport}, {"framing", surfaceFraming}, {"json-hostile", surfaceJsonHostile}} {
			t.Run(surface.name+map[bool]string{false: "/refused", true: "/read"}[accepted], func(t *testing.T) {
				dir := t.TempDir()
				input := filepath.Join(dir, "Root.json")
				if err := os.WriteFile(input, []byte(canonical), 0o600); err != nil {
					t.Fatal(err)
				}
				load := func(_ any, _ []byte, rep *report) bool {
					*rep = report{unknown: 3, widened: 2}
					return accepted
				}
				codecTable = []codec{{unit: "probe", root: "Root", fresh: func() any { return new(int) }, load: load, fromJson: load,
					toJsonMeasure: func(any) int64 {
						if !accepted {
							t.Fatal("refused read reached the value writer")
						}
						return int64(len(canonical))
					}, toJson: func(_ any, dst []byte) int64 { return int64(copy(dst, canonical)) }}}
				if surface.name == "json-hostile" {
					input = dir
				}
				// The historical no-value marker must not hide an accepted value.
				if err := surface.run([]line{{surface.name, "answer", "probe", "Root", input, "no-value"}}, dir); err != nil {
					t.Fatal(err)
				}
				got, err := os.ReadFile(filepath.Join(dir, "answer"))
				if err != nil {
					t.Fatal(err)
				}
				want := "refused\n"
				if accepted {
					want = "read\n" + canonical
				}
				if !bytes.Equal(got, []byte(want)) {
					t.Fatalf("answer %q, want %q", got, want)
				}
			})
		}
	}
}

func TestMessageFuzzWritesTheCompleteAcceptedBatch(t *testing.T) {
	for _, tc := range []struct{ accepted, writes bool }{{true, true}, {false, true}, {true, false}} {
		accepted := tc.accepted
		c := messageRow[byte, report, byte]("probe", "Root", func(v *byte) { *v = 0 }, func() *byte { return new(byte) },
			func() int64 { return 1 }, func(b []byte) int64 { b[0] = 1; return 1 }, func(*byte, []byte, *report) bool { return true },
			func(v []byte, _ *byte, _ []byte, _ *report) (int64, bool) { v[0], v[1] = 11, 22; return 2, accepted },
			func(v []*byte, _ *report) int64 {
				if !accepted {
					t.Fatal("refused message read reached the writer")
				}
				return int64(len(v))
			}, func(v []*byte, dst []byte, _ *report) int64 {
				if !tc.writes {
					return -1
				}
				for i, p := range v {
					dst[i] = *p
				}
				return int64(len(v))
			}, func(r *report) report { return *r })
		got, _, ok := c.run(nil, []byte{1}, true)
		if ok != accepted {
			t.Fatalf("read %v, want %v", ok, accepted)
		}
		if accepted && tc.writes && !bytes.Equal(got, []byte{11, 22}) || (!accepted || !tc.writes) && len(got) != 0 {
			t.Fatalf("accepted=%v writes=%v: batch %v", accepted, tc.writes, got)
		}
	}
}

func TestCookAliasRecipeConstructsTheRequestedBlobIdentity(t *testing.T) {
	input := filepath.Join(t.TempDir(), "input.json")
	if err := os.WriteFile(input, []byte(`{"name":"recipe","thumb":"AQID","alias":"BAUG"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	c := findCodec("blobdemo", "Catalog")
	for _, tc := range []struct {
		source, alias string
		want          []byte
	}{{"thumb", "alias", []byte{1, 2, 3}}, {"alias", "thumb", []byte{4, 5, 6}}} {
		value, err := cookInput(c, line{"instance", "probe", "blobdemo", "Catalog", "unused"}, line{"cook-write", "probe", input, tc.source, tc.alias})
		if err != nil {
			t.Fatal(err)
		}
		root := value.(*regionValue[blobdemo.Catalog]).root
		thumb := blobdemo.TableBytesAt(&root.Thumb).Data
		alias := blobdemo.TableBytesAt(&root.Alias).Data
		if !bytes.Equal(thumb, tc.want) || !bytes.Equal(alias, tc.want) || &thumb[0] != &alias[0] {
			t.Fatalf("recipe %s -> %s did not preserve one requested blob", tc.source, tc.alias)
		}
		if c.measure(value) >= 0 || c.toJsonMeasure(value) >= 0 {
			t.Fatal("shared blob unexpectedly has a wire or JSON spelling")
		}
		size := c.cookMeasure(value)
		if size <= 0 || !c.cook(value, make([]byte, size), false) {
			t.Fatal("Cook refused the in-memory shared blob")
		}
	}
}
