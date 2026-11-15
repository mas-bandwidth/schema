package mootable_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/fixedvector"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// EVERY DEAD BYTE IN A FIXED RECORD IS ZERO, THE WRITER'S IMAGE
// (docs/FIXED-FORM-ALGORITHM.md §3.1, §4.5, §5.3; docs/SPEC-TABLES.md §3.4,
// §13.10): an absent optional's payload, a counted array's slots past its
// count, a string's or bytes(N)'s bytes past the used length, and an inactive
// union arm (all of it under tag 0, and the span past a narrower taken arm).
// The shared vectors (fixedvector.DeadBytes) are the reference's own files: a
// control is the writer's image with every dead byte zero and reads, showing
// the same values and re-saving byte for byte; each changed file stains one
// dead region and the moo load REFUSES THE WHOLE LOAD, so no record lands.
func TestFixedDeadBytesAreZero(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted fixed-form runtime)")
	t.Parallel()
	corpus := fixedCorpus(t)
	u := unitFromPath(t, "test/tables/DeadBytes.schema")
	for _, vec := range fixedvector.DeadBytes {
		t.Run(vec.Name, func(t *testing.T) {
			t.Parallel()
			g := fixedUnit(t, u, nil)
			want, err := fixedvector.Read(corpus, vec.File)
			if err != nil {
				t.Fatal(err)
			}
			if want.Refused == vec.Reads {
				t.Fatalf("%s: the manifest says refused=%v, DeadBytes says reads=%v", vec.File, want.Refused, vec.Reads)
			}
			file := corpusFile(t, corpus, vec.File)
			got := runFixedLoad(t, g, "DeadBytesTable", vec.Root, file)
			if !vec.Reads {
				if !strings.HasPrefix(got.head, "refused ") {
					t.Fatalf("%s: %s, want the whole load refused", vec.File, got.head)
				}
				if got.saved != nil {
					t.Errorf("%s: a refused load saved %d bytes", vec.File, len(got.saved))
				}
				return
			}
			if !strings.HasPrefix(got.head, "ok 1 ") {
				t.Fatalf("%s: %s, want the control read", vec.File, got.head)
			}
			if !bytes.Equal(got.saved, file) {
				t.Errorf("%s: the control saved %d bytes, not the %d it read: %s", vec.File, len(got.saved), len(file), firstDiff(got.saved, file))
			}
		})
	}
}
