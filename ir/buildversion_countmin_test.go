package ir_test

import (
	"crypto/sha256"
	"encoding/binary"
	"os"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/ir"
)

func TestBuildVersionCountedMinimumChangesShapeIdentity(t *testing.T) {
	for _, tc := range []struct{ name, source string }{
		{"direct", "package msgarrayevo\nfixed table Probe { items [..4]int32 }\n"},
		{"optional", "package msgarrayevo\nfixed table Probe { items ?[..4]int32 }\n"},
		{"nested", "package msgarrayevo\nfixed table Inner { items [..4]int32 }\nfixed table Probe { inner Inner }\n"},
		{"union-arm", "package msgarrayevo\nunion Pick { items [..4]int32 }\nfixed table Probe { pick Pick }\n"},
		{"element-range", "package msgarrayevo\nfixed table Probe { items [..4]int32 | min = 0, max = 10 }\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			old := unitFrom(t, tc.source)
			changed := unitFrom(t, strings.Replace(tc.source, "[..4]", "[2..4]", 1))
			if old.ProtocolId != changed.ProtocolId {
				t.Fatal("table-only minimum changed packet protocol id")
			}
			if ir.BuildVersion(old) == ir.BuildVersion(changed) {
				t.Fatal("distinct announced count minima share one build identity")
			}
			a, b := ir.CookProjection(old), ir.CookProjection(changed)
			if !strings.HasPrefix(a, "schema-build-version 6\n") || !strings.Contains(a, "array=bounded bound=4 countmin=0") || !strings.Contains(b, "array=bounded bound=4 countmin=2") {
				t.Fatalf("count minimum missing from prospective facts:\n%s\n%s", a, b)
			}
			if strings.Replace(a, "countmin=0", "countmin=2", 1) != b {
				t.Fatal("minimum-only edit changed unrelated projection facts")
			}
			if tc.name == "element-range" && !strings.Contains(b, "countmin=2 min=0 max=10") {
				t.Fatal("array minimum collided with scalar element min token")
			}
		})
	}
}

func TestFrozenForm5ArrayMinimumCollisionRemainsHistorical(t *testing.T) {
	facts, err := os.ReadFile("testdata/build-v5-array-min/cook-projection.txt")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(facts)
	if got := binary.BigEndian.Uint64(sum[24:]); got != 0x83c01f94234296fd {
		t.Fatalf("actual old form5 projection moved: %016x", got)
	}
	if !strings.HasPrefix(string(facts), "schema-build-version 5\n") || strings.Contains(string(facts), "countmin=") {
		t.Fatal("legacy rendering was silently rewritten")
	}
	for _, name := range []string{"zero", "two"} {
		data, err := os.ReadFile("testdata/build-v5-array-min/" + name + "/Probe.schema")
		if err != nil {
			t.Fatal(err)
		}
		current := unitFrom(t, string(data))
		if ir.BuildVersion(current) == 0x83c01f94234296fd {
			t.Fatal("prospective source reused a frozen old build identity")
		}
	}
}

// Earlier worked projections are immutable inputs, not old values recomputed
// through the prospective renderer.
func TestFrozenBuildVersionForm5WorkedExamples(t *testing.T) {
	for _, tc := range []struct {
		name string
		hash uint64
	}{
		{"worked", 0x482a6d07fc09a843},
		{"tableless", 0xfb3cc9032b967207},
	} {
		data, err := os.ReadFile("testdata/build-v5-worked/" + tc.name + ".txt")
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(data)
		if got := binary.BigEndian.Uint64(sum[24:]); got != tc.hash {
			t.Fatalf("old %s form5 identity moved: %016x", tc.name, got)
		}
	}
}
