// `harness pin` — the goldens a driver writes rather than the engine.
//
// Two artefacts cannot come from the compiler's own engine, because what they
// describe is a RUNTIME's read rather than a file's content: the cook's
// canonical node dump (docs/SPEC-TABLES.md §7.5) and the block forgery battery
// resolved to byte offsets (§19.2). Both are pinned from the REFERENCE leg —
// the first driver in the registry, which is C++ by the registry's own order,
// exactly as the wire goldens are — and every other language byte-compares them.
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
	drivers, _, err := loadDrivers(driversPath)
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

	// the two DUMPS, each written by the reference leg's own walk: the cook's
	// node dump (§7.5) and the block's row dump (§19.2). One loop, because a
	// dump is a dump — the driver writes a file per case and the pin is that
	// file.
	pins := []struct {
		surface string
		cases   map[string]string // the file the driver writes -> the golden it becomes
	}{
		{"cook", map[string]string{}},
		{"block-dump", map[string]string{}},
	}
	for _, c := range m.Cooks {
		pins[0].cases[c.Case] = c.Dump
	}
	for _, b := range m.Blocks {
		pins[1].cases[b.Name] = b.Dump
	}
	if err := os.MkdirAll(cookDir, 0o755); err != nil {
		return err
	}
	for _, p := range pins {
		out := filepath.Join(work, "pin", p.surface)
		if err := os.RemoveAll(out); err != nil {
			return err
		}
		if err := os.MkdirAll(out, 0o755); err != nil {
			return err
		}
		code, stderr, err := runDriver(reference, derived, p.surface, out)
		if err != nil {
			return err
		}
		if code != 0 {
			return fmt.Errorf("the %s driver's %s surface exited %d\n%s", reference.lang, p.surface, code, stderr)
		}
		for name, golden := range p.cases {
			data, err := os.ReadFile(filepath.Join(out, name))
			if err != nil {
				return err
			}
			if err := writeFileAtomic(golden, data); err != nil {
				return err
			}
			fmt.Printf("conformance: pinned %s from the %s leg — %d bytes\n", golden, reference.lang, len(data))
		}
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

	// and the COOK battery, the same way and for the same reason: its 111 rows
	// name this build's own numbers — the region's alignment, the two part
	// lengths, the build version, the root's sizeof — and only a backend that
	// reads a cooked header knows them. One emit per (root, fixture) pair the
	// battery covers: <subject> names the root to open with and <base> names the
	// fixture the patch lands on, and one root can have more than one fixture.
	seen := map[string]bool{}
	for _, f := range m.Forgeries {
		if f.Kind != "cook" || seen[f.Subject+"/"+f.Base] {
			continue
		}
		seen[f.Subject+"/"+f.Base] = true
		fixture := ""
		for _, c := range m.Cooks {
			if c.Case == f.Base {
				fixture = c.File
			}
		}
		if fixture == "" {
			return fmt.Errorf("the cook battery is based on %q, which no cook line materialises", f.Base)
		}
		fmt.Println()
		cmd := exec.Command("build/schema_test_cook", "emit-forgeries", f.Subject, fixture)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("emitting the cook forgeries for %s: %w", f.Subject, err)
		}
	}
	return nil
}
