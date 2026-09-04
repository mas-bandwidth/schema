// The registry's own gate. `go test ./...` carries it, so a port that lands a
// conformance driver without a native row goes red on the diff that lands it
// rather than on whoever reads the matrix next.
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// repoRoot is two directories up from this package.
const repoRoot = "../../.."

func TestEveryPortHasANativeRow(t *testing.T) {
	t.Chdir(repoRoot)
	langs, err := ports(portsDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(langs) < 9 {
		t.Fatalf("%s: %d ports discovered, and the law owes a native row for nine", portsDir, len(langs))
	}
	for _, lang := range langs {
		if _, err := os.Stat(filepath.Join(nativeDir, lang, "ci.json")); err != nil {
			t.Errorf("%s has a conformance driver and no native row at %s/%s/ci.json (issue #547)", lang, nativeDir, lang)
		}
	}
}

func TestMatrixCarriesEveryPortAndNamesItsTarget(t *testing.T) {
	t.Chdir(repoRoot)
	out, err := matrix(nativeDir, portsDir)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Include []map[string]string `json:"include"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	langs, err := ports(portsDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Include) != len(langs) {
		t.Fatalf("matrix has %d rows for %d ports", len(got.Include), len(langs))
	}
	for _, row := range got.Include {
		if want := "native-" + row["lang"]; row["targets"] != want {
			t.Errorf("%s: targets is %q, and the leg CI runs is %q", row["lang"], row["targets"], want)
		}
	}
}

// Every row carries every key, so a workflow step conditioned on one reads the
// same for a leg that set it and a leg that did not.
func TestEveryRowCarriesEveryKey(t *testing.T) {
	t.Chdir(repoRoot)
	out, err := matrix(nativeDir, portsDir)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Include []map[string]string `json:"include"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	keys := map[string]bool{}
	for _, row := range got.Include {
		for k := range row {
			keys[k] = true
		}
	}
	for _, row := range got.Include {
		for k := range keys {
			if _, ok := row[k]; !ok {
				t.Errorf("%s: no %q key", row["lang"], k)
			}
		}
	}
}
