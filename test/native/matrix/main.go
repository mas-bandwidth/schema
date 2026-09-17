// The native gate's CI registry (issue #547): DISCOVERED, not listed.
//
// A port's native row is test/native/<lang>/ci.json, and this command prints
// the matrix .github/workflows/ci.yml fans out over — one job per port,
// running that port's `make native-<lang>`. A row names the make targets, the
// sibling runtime the leg's analyzer needs on disk to typecheck what the
// emitter wrote, the toolchain steps CI installs, and the make overrides that
// point the leg at them.
//
// THE REGISTRY IS TIED TO THE PORT SET. The nine rows the law owes are the
// nine ports, so this command reads the conformance registry — the one file
// set that already says which languages exist — and refuses a tree where a
// port has no native row, or a native row names no port. A port that lands
// without its native gate is the gap this file exists to close.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// portsDir is the committed port registry: one directory per language, each
// holding a conformance `driver`. The native gate owes one row per port.
const portsDir = "test/conformance"

// nativeDir holds the native rows, one directory per language.
const nativeDir = "test/native"

func main() {
	out, err := matrix(nativeDir, portsDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "native matrix:", err)
		os.Exit(1)
	}
	fmt.Println(string(out))
}

// ports lists every language with a conformance driver, in name order.
func ports(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		info, err := os.Stat(filepath.Join(dir, e.Name(), "driver"))
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		out = append(out, e.Name())
	}
	slices.Sort(out)
	return out, nil
}

// rows lists every language with a native row, in name order.
func rows(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, e.Name(), "ci.json")); err != nil {
			continue
		}
		out = append(out, e.Name())
	}
	slices.Sort(out)
	return out, nil
}

// matrix prints `{"include": [...]}`, one row per port, from every
// test/native/<lang>/ci.json. A row is that file's keys plus "lang", and every
// row carries every key any row set, empty where it did not — so a workflow
// step comparing matrix.<key> with the empty string reads the same for every
// leg, which is what lets one job serve nine toolchains.
func matrix(dir, portRegistry string) ([]byte, error) {
	langs, err := ports(portRegistry)
	if err != nil {
		return nil, err
	}
	if len(langs) == 0 {
		return nil, fmt.Errorf("%s: no port has a driver — this matrix would fan out over nothing", portRegistry)
	}
	registered, err := rows(dir)
	if err != nil {
		return nil, err
	}
	for _, lang := range registered {
		if !slices.Contains(langs, lang) {
			return nil, fmt.Errorf("%s/%s/ci.json: a native row for %q, which is not a port in %s", dir, lang, lang, portRegistry)
		}
	}

	keys := map[string]bool{"lang": true, "targets": true}
	rows := make([]map[string]string, 0, len(langs))
	for _, lang := range langs {
		path := filepath.Join(dir, lang, "ci.json")
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("%s: the %s port has no native row: %w", path, lang, err)
		}
		row := map[string]string{}
		if err := json.Unmarshal(data, &row); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if strings.TrimSpace(row["targets"]) == "" {
			return nil, fmt.Errorf("%s: \"targets\" names no make target", path)
		}
		row["lang"] = lang
		for k := range row {
			keys[k] = true
		}
		rows = append(rows, row)
	}
	for _, row := range rows {
		for k := range keys {
			if _, ok := row[k]; !ok {
				row[k] = ""
			}
		}
	}
	return json.Marshal(map[string]any{"include": rows})
}
