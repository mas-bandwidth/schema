// `harness run` — the gate, and the matrix it prints.
//
// One process per (language, surface). The driver is handed a DERIVED manifest
// — the committed one with the materialised fixture paths folded in, and with
// every expected answer REMOVED — an output directory, and nothing else. The
// harness holds the expectations and does the comparing, so a driver cannot
// pass by reading the answer.
package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// surfaces, in the order the matrix prints them.
var surfaces = []string{"wire", "report", "json-read", "json-write", "json-hostile",
	"cook", "cook-foreign", "block", "block-foreign", "block-dump", "forgery", "cook-forgery"}

type driver struct {
	lang string
	argv []string
}

type result struct {
	pass, total int
	absent      bool // the whole SURFACE: the backend does not implement it
	// missing counts the CASES a driver answered ABSENT one at a time, by
	// writing `<case>.absent` beside where the answer would go. A backend with
	// no variable class runs the wire surface over the fixed instances and says
	// so about the rest, which is a missing FEATURE per case rather than a
	// failing test — the same distinction the surface-level absence draws, at
	// the grain the corpus now needs (test/conformance/README.md).
	missing  int
	failures []string
}

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

// materialise writes the fixtures a driver cannot be handed as committed text:
// the cooked files, which test/cookgen produces deterministically, and the
// forged images, which are patches over a base fixture.
func materialise(m *Manifest, work string) error {
	fixtures := filepath.Join(work, "fixtures")
	if err := os.RemoveAll(fixtures); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(fixtures, "forgery"), 0o755); err != nil {
		return err
	}

	// bases a forgery may be patched over: the block images by name, and the
	// cooked files by root
	base := map[string]string{}
	for _, b := range m.Blocks {
		base[b.Name] = b.File
	}

	if len(m.Cooks) > 0 {
		cookgen := filepath.Join(work, "cookgen")
		build := exec.Command("go", "build", "-o", cookgen, "./test/cookgen")
		build.Stderr = os.Stderr
		if err := build.Run(); err != nil {
			return fmt.Errorf("building cookgen: %w", err)
		}
		for i := range m.Cooks {
			c := &m.Cooks[i]
			out := filepath.Join(fixtures, c.Case+".cook")
			args := []string{"--bytes", "4096", "--root", c.Root, "--out", out}
			extra, ok := cookShape[c.Case]
			if !ok {
				return fmt.Errorf("cooking %s: test/conformance/harness names no fixture shape for it", c.Case)
			}
			args = append(args, extra...)
			cmd := exec.Command(cookgen, args...)
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
				return fmt.Errorf("cooking %s: %w", c.Case, err)
			}
			c.File = out
			base[c.Case] = out
		}
	}

	for i := range m.Forgeries {
		f := &m.Forgeries[i]
		src, ok := base[f.Base]
		if !ok {
			return fmt.Errorf("forgery %s: the manifest names no fixture %q", f.Name, f.Base)
		}
		data, err := os.ReadFile(src)
		if err != nil {
			return err
		}
		for _, p := range f.Patches {
			if p.Offset+int64(p.Width) > int64(len(data)) {
				return fmt.Errorf("forgery %s: the patch at %d+%d does not fit %s (%d bytes)",
					f.Name, p.Offset, p.Width, src, len(data))
			}
			var word [8]byte
			binary.LittleEndian.PutUint64(word[:], p.Value)
			copy(data[p.Offset:p.Offset+int64(p.Width)], word[:p.Width])
		}
		out := filepath.Join(fixtures, "forgery", f.Name+".bin")
		if err := os.WriteFile(out, data, 0o644); err != nil {
			return err
		}
		f.File = out
	}
	return nil
}

// cookShape carries the chain each CASE's fixture is generated with, which
// test/cookgen needs and the manifest does not: it is a property of the FIXTURE
// GENERATOR, not of the conformance data, and a driver never sees it.
//
// `--values` is what makes SceneValued a value gate rather than a structure
// one: without it every node is value-initialised, so the dump locks every
// offset, every deref and every visit order and almost no VALUES, because there
// are almost none in it.
var cookShape = map[string][]string{
	"Scene":       {"--ref", "head", "--chain", "ListNode", "--next", "next"},
	"SceneValued": {"--ref", "head", "--chain", "ListNode", "--next", "next", "--values"},
	"Depot":       {"--ref", "head", "--chain", "ListNode", "--next", "next"},
	"Album":       {"--ref", "head", "--chain", "ListNode", "--next", "next"},
	"TreeNode":    {"--ref", "left", "--chain", "TreeNode", "--next", "left"},
	"ListNode":    {"--ref", "next", "--chain", "ListNode", "--next", "next"},
}

// deriveManifest writes what a driver reads: the committed lines plus the
// materialised paths, with every expected answer withheld.
func deriveManifest(m *Manifest, path string) error {
	var b strings.Builder
	b.WriteString("# DERIVED, by `harness run` — do not edit, do not commit.\n")
	b.WriteString("# The committed manifest with the materialised fixture paths folded in and\n")
	b.WriteString("# every expected answer removed. testdata/conformance/tables/FORMAT.md states\n")
	b.WriteString("# both shapes.\n\n")
	for _, u := range m.Units {
		fmt.Fprintf(&b, "unit %s %s\n", u.Key, strings.Join(u.Paths, " "))
	}
	for _, i := range m.Instances {
		marker := ""
		if i.NoText {
			// the marker travels: a driver has to know which instances the
			// corpus carries on the wire alone, so that its TEXT surfaces skip
			// them rather than reporting a form nobody has yet (§16.2)
			marker = " no-text"
		}
		fmt.Fprintf(&b, "instance %s %s %s %s%s\n", i.Name, i.Unit, i.Root, i.Wire, marker)
	}
	for _, r := range m.Reports {
		fmt.Fprintf(&b, "report %s %s %s %s\n", r.Name, r.Unit, r.Root, r.Wire)
	}
	for _, h := range m.Hostiles {
		fmt.Fprintf(&b, "json-hostile %s %s %s %s\n", h.Name, h.Unit, h.Root, h.Tree)
	}
	for _, c := range m.Cooks {
		fmt.Fprintf(&b, "cook %s %s %s %s\n", c.Case, c.Unit, c.Root, repoRelative(c.File))
	}
	for _, bl := range m.Blocks {
		fmt.Fprintf(&b, "block %s %s %s\n", bl.Name, bl.Unit, bl.File)
	}
	for _, f := range m.Forgeries {
		// the PATCH is already applied; what a driver still needs is the file,
		// the extent its caller claims and the buffer that caller holds
		pointer := strconv.Itoa(f.Pointer)
		if f.Pointer < 0 {
			pointer = "null"
		}
		fmt.Fprintf(&b, "forgery %s %s %s %s %d %s\n",
			f.Name, f.Kind, f.Subject, repoRelative(f.File), f.Extent, pointer)
	}
	return writeFileAtomic(path, []byte(b.String()))
}

// expectation is what the harness compares a driver's output against.
type expectation struct {
	name string // the file the driver writes
	want []byte
}

func expectations(m *Manifest, surface string, reports map[string]Counts, jsonDir string) ([]expectation, error) {
	var out []expectation
	switch surface {
	case "wire", "json-read":
		for _, i := range m.Instances {
			if i.NoText && surface == "json-read" {
				continue // no text to read it FROM (§16.2, schema#275)
			}
			want, err := os.ReadFile(i.Wire)
			if err != nil {
				return nil, err
			}
			out = append(out, expectation{i.Name, want})
		}
	case "json-write":
		for _, i := range m.Instances {
			if i.NoText {
				continue // the corpus owes this one a text; no leg is asked for one
			}
			want, err := os.ReadFile(i.JSON)
			if err != nil {
				return nil, err
			}
			out = append(out, expectation{i.Name + ".json", want})
		}
	case "report":
		for _, r := range m.Reports {
			c, ok := reports[r.Name]
			if !ok {
				return nil, fmt.Errorf("reports.txt names no case %q — run: make conformance-generate", r.Name)
			}
			out = append(out, expectation{r.Name, []byte(c.String() + "\n")})
		}
	case "cook":
		for _, c := range m.Cooks {
			want, err := os.ReadFile(c.Dump)
			if err != nil {
				return nil, err
			}
			out = append(out, expectation{c.Case, want})
		}
	case "json-hostile":
		for _, h := range m.Hostiles {
			out = append(out, expectation{h.Name, []byte(h.Verdict() + "\n")})
		}
	case "block":
		for _, b := range m.Blocks {
			out = append(out, expectation{b.Name, []byte("open\n")})
		}
	case "block-foreign", "cook-foreign":
		// THE FOREIGN-ORDER REFUSAL, and the expectation is a constant because
		// the driver makes the file foreign to ITSELF: it byte-swaps the magic
		// word in its own order, so every leg on every host meets a magic that
		// is not this build's and refuses BY MAGIC — which is the check §19.1
		// and §7.1 put first for exactly this. A cross-endian gate that
		// depended on the host's own order could only ever be green on one.
		if surface == "block-foreign" {
			for _, b := range m.Blocks {
				out = append(out, expectation{b.Name, []byte("refuse\n")})
			}
			break
		}
		for _, c := range m.Cooks {
			out = append(out, expectation{c.Case, []byte("refuse\n")})
		}
	case "block-dump":
		for _, b := range m.Blocks {
			want, err := os.ReadFile(b.Dump)
			if err != nil {
				return nil, err
			}
			out = append(out, expectation{b.Name, want})
		}
	case "forgery", "cook-forgery":
		// one surface per KIND: a backend can have a block reader and no cook
		// reader, and the matrix says which rather than blaming one for the
		// other. The row shape is the same either way.
		kind := "block"
		if surface == "cook-forgery" {
			kind = "cook"
		}
		for _, f := range m.Forgeries {
			if f.Kind != kind {
				continue
			}
			out = append(out, expectation{f.Name, []byte(f.Verdict + "\n")})
		}
	default:
		return nil, fmt.Errorf("%q is not a surface", surface)
	}
	return out, nil
}

func run(m *Manifest, manifestPath, jsonDir, reportsPath, driversPath, work, only string) (bool, error) {
	drivers, err := readDrivers(driversPath)
	if err != nil {
		return false, err
	}
	reports, err := readReports(reportsPath)
	if err != nil {
		return false, err
	}
	if err := materialise(m, work); err != nil {
		return false, err
	}
	derived := filepath.Join(work, "manifest.txt")
	if err := deriveManifest(m, derived); err != nil {
		return false, err
	}

	want := map[string][]expectation{}
	for _, s := range surfaces {
		e, err := expectations(m, s, reports, jsonDir)
		if err != nil {
			return false, err
		}
		want[s] = e
	}

	results := map[string]map[string]*result{}
	var langs []string
	for i, d := range drivers {
		// THE REFERENCE LEG MAY NOT ANSWER ABSENT. It is the first driver in
		// the registry and the one `conformance-pin` takes its pins from, so an
		// absence there is not a port's missing feature — it is the corpus
		// losing its own expectation, quietly, while every other leg keeps
		// comparing against nothing. Per-case absence is safe exactly because
		// this rule stands beside it.
		reference := i == 0
		if only != "" && d.lang != only {
			continue
		}
		langs = append(langs, d.lang)
		results[d.lang] = map[string]*result{}

		listed, err := listSurfaces(d, derived)
		if err != nil {
			return false, fmt.Errorf("%s: %w", d.lang, err)
		}
		for _, s := range surfaces {
			r := &result{total: len(want[s])}
			results[d.lang][s] = r
			if !listed[s] {
				r.absent = true
				continue
			}
			out := filepath.Join(work, "out", d.lang, s)
			if err := os.RemoveAll(out); err != nil {
				return false, err
			}
			if err := os.MkdirAll(out, 0o755); err != nil {
				return false, err
			}
			code, stderr, err := runDriver(d, derived, s, out)
			if err != nil {
				return false, err
			}
			if code == 2 {
				r.absent = true
				continue
			}
			if code != 0 {
				r.failures = append(r.failures, fmt.Sprintf("the driver exited %d\n%s", code, indent(stderr, "    ")))
				continue
			}
			for _, e := range want[s] {
				got, err := os.ReadFile(filepath.Join(out, e.name))
				if err != nil {
					if _, absent := os.Stat(filepath.Join(out, e.name+".absent")); absent == nil {
						// the driver SAID it cannot answer this case, which is
						// a feature it lacks and not a test it failed
						r.missing++
						continue
					}
					r.failures = append(r.failures, fmt.Sprintf("%s: the driver wrote nothing", e.name))
					continue
				}
				if !bytes.Equal(got, e.want) {
					r.failures = append(r.failures, describeDiff(e.name, e.want, got))
					continue
				}
				r.pass++
			}
			if reference && r.missing > 0 {
				r.failures = append(r.failures, fmt.Sprintf(
					"the REFERENCE leg answered ABSENT for %d case(s) — an absence here is the corpus "+
						"losing its own expectation, not a missing feature; the reference answers every case "+
						"it registers a surface for", r.missing))
			}
		}
	}

	return report(langs, results), nil
}

func listSurfaces(d driver, manifest string) (map[string]bool, error) {
	argv := append(append([]string{}, d.argv...), manifest, "list")
	cmd := exec.Command(argv[0], argv[1:]...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("`%s list` failed: %w\n%s", strings.Join(argv, " "), err, stderr.String())
	}
	out := map[string]bool{}
	for line := range strings.SplitSeq(stdout.String(), "\n") {
		if s := strings.TrimSpace(line); s != "" {
			out[s] = true
		}
	}
	return out, nil
}

func runDriver(d driver, manifest, surface, out string) (int, string, error) {
	argv := append(append([]string{}, d.argv...), manifest, surface, out)
	cmd := exec.Command(argv[0], argv[1:]...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Stdout = os.Stdout
	err := cmd.Run()
	if err == nil {
		return 0, stderr.String(), nil
	}
	if exit, ok := errors.AsType[*exec.ExitError](err); ok {
		return exit.ExitCode(), stderr.String(), nil
	}
	return 0, stderr.String(), fmt.Errorf("`%s`: %w", strings.Join(argv, " "), err)
}

// describeDiff says WHERE, because "the bytes differ" over a 200 KB instance is
// not a diagnosis.
func describeDiff(name string, want, got []byte) string {
	if len(want) != len(got) {
		return fmt.Sprintf("%s: %d bytes out, %d expected", name, len(got), len(want))
	}
	for i := range want {
		if want[i] != got[i] {
			return fmt.Sprintf("%s: first difference at byte %d — expected 0x%02x, got 0x%02x", name, i, want[i], got[i])
		}
	}
	return name + ": differs"
}

func report(langs []string, results map[string]map[string]*result) bool {
	ok := true
	// wide enough for "pass 111/111 +4a", the widest cell the matrix can print
	width := 18
	for _, s := range surfaces {
		if len(s)+2 > width {
			width = len(s) + 2
		}
	}
	fmt.Printf("\nTABLES CONFORMANCE — surface x language\n\n")
	fmt.Printf("%-14s", "surface")
	for _, l := range langs {
		fmt.Printf("%-*s", width, l)
	}
	fmt.Println()
	fmt.Printf("%s\n", strings.Repeat("-", 14+width*len(langs)))
	for _, s := range surfaces {
		fmt.Printf("%-14s", s)
		for _, l := range langs {
			r := results[l][s]
			switch {
			case r.absent:
				fmt.Printf("%-*s", width, "absent")
			case len(r.failures) > 0:
				fmt.Printf("%-*s", width, fmt.Sprintf("FAIL %d/%d", r.pass, r.total))
				ok = false
			case r.missing > 0:
				// what it answered, and what it said it cannot: the cell is the
				// completion tracker, so an absence stays visible rather than
				// being rounded away into a smaller total
				fmt.Printf("%-*s", width, fmt.Sprintf("pass %d/%d +%da", r.pass, r.total-r.missing, r.missing))
			default:
				fmt.Printf("%-*s", width, fmt.Sprintf("pass %d/%d", r.pass, r.total))
			}
		}
		fmt.Println()
	}
	fmt.Println()

	var lines []string
	for _, l := range langs {
		for _, s := range surfaces {
			for _, f := range results[l][s].failures {
				lines = append(lines, fmt.Sprintf("  %s / %s: %s", l, s, f))
			}
		}
	}
	sort.Strings(lines)
	if len(lines) > 0 {
		fmt.Printf("FAILURES\n%s\n\n", strings.Join(lines, "\n"))
	}
	if ok {
		fmt.Printf("tables conformance: every registered surface passes\n")
	}
	return ok
}
