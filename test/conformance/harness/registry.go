// The driver registry (test/conformance/README.md, "Registering a language"):
// DISCOVERED, not listed. A language is registered when test/conformance/<lang>/
// driver exists, and its CI row when test/conformance/<lang>/ci.json does. No
// file names every language, so a port touches nothing another port touches.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// referenceLang is the reference leg: the one `pin` takes the goldens from and
// the one that may not answer ABSENT. C++ writes the pins, every other leg
// compares, and that is the repo's standing convention.
const referenceLang = "cpp"

// defaultDriversDir is the committed registry — one directory per language,
// each holding a `driver` command.
const defaultDriversDir = "test/conformance"

// discoverDrivers reads the committed registry: every <dir>/<lang>/driver that
// is a regular file. The reference leg comes first and the rest follow by
// name, so the matrix prints in one order whatever the file system's is.
func discoverDrivers(dir string) ([]driver, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []driver
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		path := filepath.Join(dir, e.Name(), "driver")
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		out = append(out, driver{lang: e.Name(), argv: []string{filepath.ToSlash(path)}})
	}
	sort.Slice(out, func(i, j int) bool {
		if (out[i].lang == referenceLang) != (out[j].lang == referenceLang) {
			return out[i].lang == referenceLang
		}
		return out[i].lang < out[j].lang
	})
	if len(out) == 0 || out[0].lang != referenceLang {
		return nil, fmt.Errorf("%s: no reference leg — %s/%s/driver is missing", dir, dir, referenceLang)
	}
	return out, nil
}

// readDrivers reads a SUBSTITUTED registry: one `<language> <command...>` line
// per driver. It is how a negative control or the big-endian leg points the
// harness at a driver that is not the committed one, and a run under it is one
// leg of a port rather than the matrix — its first line is not the reference.
func readDrivers(path string) ([]driver, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []driver
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Fields(line)
		if len(f) < 2 {
			return nil, fmt.Errorf("%s: %q names no command", path, line)
		}
		out = append(out, driver{lang: f[0], argv: f[1:]})
	}
	return out, nil
}

// loadDrivers resolves --drivers: a directory is discovered, a file is read.
// discovered says which, because the reference rule belongs to the committed
// registry alone.
func loadDrivers(path string) (drivers []driver, discovered bool, err error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, false, err
	}
	if info.IsDir() {
		drivers, err = discoverDrivers(path)
		return drivers, true, err
	}
	drivers, err = readDrivers(path)
	return drivers, false, err
}

// matrix prints the CI matrix — `{"include": [...]}`, one row per discovered
// driver — from every test/conformance/<lang>/ci.json. A row is that file's
// keys plus "lang"; the workflow reads "targets" (the make targets that build
// the leg), "env" (variable assignments the make and run steps carry),
// "runtime" and "runtime_tag" (the sibling checkout and the workflow variable
// holding its pin), and one key per toolchain step it can install. Every row
// carries every key seen in any file, empty where a leg did not set it, so a
// step comparing matrix.<key> with the empty string reads the same for every
// leg.
//
// A driver with no ci.json is an error: a leg the harness runs locally and CI
// never sees is the gap this registry exists to close.
func matrix(dir string) ([]byte, error) {
	drivers, err := discoverDrivers(dir)
	if err != nil {
		return nil, err
	}
	keys := map[string]bool{"lang": true, "targets": true}
	var rows []map[string]string
	for _, d := range drivers {
		path := filepath.Join(dir, d.lang, "ci.json")
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("%s: the %s leg has a driver and no CI row: %w", path, d.lang, err)
		}
		row := map[string]string{}
		if err := json.Unmarshal(data, &row); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if strings.TrimSpace(row["targets"]) == "" {
			return nil, fmt.Errorf("%s: \"targets\" names no make target", path)
		}
		row["lang"] = d.lang
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
