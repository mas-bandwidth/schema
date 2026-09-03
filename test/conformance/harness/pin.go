// `harness pin` — the goldens a driver writes rather than the engine.
//
// Two artefacts cannot come from the compiler's own engine, because what they
// describe is a RUNTIME's read rather than a file's content: the cook's
// canonical node dump (SPEC-TABLES.md §7.5) and the block forgery battery
// resolved to byte offsets (§19.2). Both are pinned from the REFERENCE leg —
// the first driver in the registry, which is C++, exactly as the wire goldens
// are — and every other language byte-compares them.
//
// This is deliberate and it is the repo's standing convention: C++ writes the
// pins, every other leg compares. A pin that MOVES under an unchanged schema is
// stop-the-line, never a quiet repin.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func pin(m *Manifest, driversPath, work, cookDir string) error {
	drivers, err := readDrivers(driversPath)
	if err != nil {
		return err
	}
	if len(drivers) == 0 {
		return fmt.Errorf("%s: no driver to pin from", driversPath)
	}
	reference := drivers[0]

	if err := materialise(m, work); err != nil {
		return err
	}
	derived := filepath.Join(work, "manifest.txt")
	if err := deriveManifest(m, derived); err != nil {
		return err
	}

	out := filepath.Join(work, "pin", "cook")
	if err := os.RemoveAll(out); err != nil {
		return err
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	code, stderr, err := runDriver(reference, derived, "cook", out)
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("the %s driver's cook surface exited %d\n%s", reference.lang, code, stderr)
	}
	if err := os.MkdirAll(cookDir, 0o755); err != nil {
		return err
	}
	for _, c := range m.Cooks {
		data, err := os.ReadFile(filepath.Join(out, c.Root))
		if err != nil {
			return err
		}
		if err := writeFileAtomic(c.Dump, data); err != nil {
			return err
		}
		fmt.Printf("conformance: pinned %s from the %s leg — %d bytes\n", c.Dump, reference.lang, len(data))
	}

	// and the block battery, whose offsets only a backend that can open a block
	// knows. The C++ driver prints the manifest lines; a human folds them into
	// MANIFEST.txt, because a manifest that rewrites itself is a manifest
	// nobody reviews.
	cmd := exec.Command("build/conformance-cpp", "emit-block-forgeries")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("emitting the block forgeries: %w", err)
	}
	return nil
}
