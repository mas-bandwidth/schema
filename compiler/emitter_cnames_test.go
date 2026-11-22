package compiler

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
)

// TestCNamespaceCurrent: emitter_names_c.json is what the C target writes
// for the control unit today. A target that moves a runtime name fails here;
// SCHEMA_UPDATE_EMITTER_TABLES=1 rewrites the file.
func TestCNamespaceCurrent(t *testing.T) {
	got, err := computeCNamespace()
	if err != nil {
		t.Fatal(err)
	}
	want, err := json.MarshalIndent(got, "", " ")
	if err != nil {
		t.Fatal(err)
	}
	want = append(want, '\n')
	if bytes.Equal(want, cNamespaceJSON) {
		return
	}
	if os.Getenv("SCHEMA_UPDATE_EMITTER_TABLES") == "1" {
		if err := os.WriteFile("emitter_names_c.json", want, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	t.Fatal("emitter_names_c.json is not what the C target writes for the control unit; run SCHEMA_UPDATE_EMITTER_TABLES=1 go test ./compiler -run TestCNamespaceCurrent")
}
