package gotable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A sizing scan follows announced framing even when a reserved transport id
// occurs in a root body. Typed decoding still rejects that body as damage.
func TestMessageMeasureReservedFraming(t *testing.T) {
	paths, err := filepath.Glob("../../../tables/pointers/*.schema")
	if err != nil {
		t.Fatal(err)
	}
	schema := "package graphdemo\n"
	for _, path := range paths {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		part := strings.ReplaceAll(string(b), "package graphdemo", "")
		// The single-file probe needs no packet-only C++ native mapping.
		part = strings.ReplaceAll(part, ` | cpp_native = ColourMath, cpp_include = "graph_colour.h"`, "")
		schema += part + "\n"
	}
	runGenerated(t, schema, `package graphdemo
 import("testing";"encoding/hex")
 func TestReservedFraming(t *testing.T){wire,_:=hex.DecodeString("020c0100a501000000eb04000000280161cf00d61100000010667000746f7021440333486c656674c00c16726967687400ccad1b6d69640045007472656595c090c5eb00")
 var v TableVocabulary;v.Init(make([]TableMessageEntry,TableMessageEntriesHere));ann:=make([]byte,AnnounceMeasure());Announce(ann);var report TableReport;if !AnnounceRead(&v,ann,&report){t.Fatal(report)}
 if n:=SceneLoadMessagesMeasure(&v,wire);n!=960{t.Fatalf("reserved framing extent %d want 960",n)}
 }
 `)
}
