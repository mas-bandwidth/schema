package main

// RELAXED 2026-11-14 (maintainer: get main green): the relaxed CASES. A line
// of test/conformance/relaxed.txt names one leg's case on one surface that
// fails today and the slug of its entry in docs/relaxed.md:
//
//	<lang> <surface> <case> relax/<slug>
//
// A listed case that fails is still "fail" in the --results file (so the
// proof names it) and in the run's log, but it does not fail the leg. A listed
// case that passes fails the leg until its line is removed, so a fixed case
// cannot hide in the list. Each relaxation is undone by deleting its lines.

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"strings"
)

const relaxedCasesPath = "test/conformance/relaxed.txt"

var relaxedSlug = regexp.MustCompile(`^relax/[a-z0-9][a-z0-9-]*$`)

func relaxedKey(lang, surface, name string) string {
	return lang + " " + surface + " " + name
}

// readRelaxedCases reads the list; a missing file is an empty list.
func readRelaxedCases(path string) (map[string]string, error) {
	out := map[string]string{}
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	n := 0
	for sc.Scan() {
		n++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 4 || !relaxedSlug.MatchString(fields[3]) {
			return nil, fmt.Errorf("%s:%d: want `<lang> <surface> <case> relax/<slug>`, got %q", path, n, line)
		}
		key := relaxedKey(fields[0], fields[1], fields[2])
		if _, dup := out[key]; dup {
			return nil, fmt.Errorf("%s:%d: %s is listed twice", path, n, key)
		}
		out[key] = fields[3]
	}
	return out, sc.Err()
}
