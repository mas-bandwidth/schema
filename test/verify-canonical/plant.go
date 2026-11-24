package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// THE PLANTED ORACLE, for the negative controls of every leg
// (make/widen-18-gate-controls.mk verify-canonical-negative-control-<lang>;
// 2026-11-24). The emitter sabotage proves the gate red for JavaScript only;
// here the ORACLE is made wrong instead, the same way for every leg. Under
// SCHEMA_GATE_PLANT=canonical the canonical wire every leg's writes are held
// to has its last byte inverted, in memory and in a copy of the corpus the
// legs read, so a leg whose gate really compares its canonical bytes goes red
// by name, and one that compared its output with itself would stay green.
func plantCanonical(corpusDir, dir string, wire []byte) (string, []byte, error) {
	if len(wire) == 0 {
		return "", nil, fmt.Errorf("SCHEMA_GATE_PLANT=canonical: the canonical wire is empty: nothing to plant")
	}
	planted := append([]byte(nil), wire...)
	planted[len(planted)-1] ^= 0xff
	if err := os.RemoveAll(dir); err != nil {
		return "", nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", nil, err
	}
	entries, err := os.ReadDir(corpusDir)
	if err != nil {
		return "", nil, err
	}
	for _, e := range entries {
		if !e.Type().IsRegular() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(corpusDir, e.Name()))
		if err != nil {
			return "", nil, err
		}
		if e.Name() == "canonical.bin" {
			b = planted
		}
		if err := os.WriteFile(filepath.Join(dir, e.Name()), b, 0o644); err != nil {
			return "", nil, err
		}
	}
	return dir, planted, nil
}
