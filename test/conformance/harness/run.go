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
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// surfaces, in the order the matrix prints them.
var surfaces = []string{"wire", "message", "report", "framing", "retain", "retain-save", "json-read", "json-write", "json-hostile",
	"cook", "cook-write", "cook-foreign", "block", "block-foreign", "block-dump", "forgery", "cook-forgery",
	"cook-reason", "block-reason"}

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
	missing int
	// named counts the cases among missing that a leg answered with the
	// row's NAMED WRITER REFUSAL (an instance's go-refusal= marker): every
	// writer refuses the value by that name (docs/SPEC-TABLES.md §2.5), so the
	// answer is the refusal and not a feature the leg lacks, and the reference
	// leg may give it.
	named    int
	failures []string
	// cases is each case's verdict, by the file name the driver writes: pass,
	// named (the row's named writer refusal), absent or fail. The proof table
	// (tools/matrix proof) reads it from the --results file, so a cell can name
	// the exact case that holds it and the exact case that is still absent.
	cases map[string]string
}

// verdict records one case's verdict.
func (r *result) verdict(name, v string) {
	if r.cases == nil {
		r.cases = map[string]string{}
	}
	r.cases[name] = v
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
			// them rather than writing a text the form refuses (§16.7)
			marker = " no-text"
		}
		fmt.Fprintf(&b, "instance %s %s %s %s%s\n", i.Name, i.Unit, i.Root, i.Wire, marker)
	}
	for _, c := range m.Connections {
		fmt.Fprintf(&b, "connection %s %s 0x%016x %s\n", c.Key, c.Unit, c.BuildVersion, c.Wire)
	}
	for _, msg := range m.Messages {
		fmt.Fprintf(&b, "message %s %s %s %s %s\n", msg.Name, msg.Connection, msg.Root, msg.FileWire, msg.MessageWire)
	}
	for _, rc := range m.Retains {
		// THE ANSWER IS WITHHELD, as it is everywhere else: a driver is handed
		// the wire and the two capacities and nothing about the counters or the
		// bytes it owes (docs/SPEC-TABLES.md §6.6). `short` and `full` travel as
		// the RULES they are, because a record's byte cost is the port's own.
		ids := "full"
		if rc.Ids >= 0 {
			ids = strconv.Itoa(rc.Ids)
		}
		capacity := "full"
		if rc.Short {
			capacity = "short"
		}
		fmt.Fprintf(&b, "retain %s %s %s %s %s %s\n", rc.Name, rc.Unit, rc.Root, rc.Wire, capacity, ids)
	}
	for _, r := range m.Reports {
		fmt.Fprintf(&b, "report %s %s %s %s\n", r.Name, r.Unit, r.Root, r.Wire)
	}
	for _, fc := range m.Framings {
		// the verdict and the value file are the answer and stay withheld; a
		// driver still has to know which rows owe the report alone
		marker := ""
		if fc.Value == "" {
			marker = " no-value"
		}
		fmt.Fprintf(&b, "framing %s %s %s %s%s\n", fc.Name, fc.Unit, fc.Root, fc.Wire, marker)
	}
	for _, h := range m.Hostiles {
		fmt.Fprintf(&b, "json-hostile %s %s %s %s\n", h.Name, h.Unit, h.Root, h.Tree)
	}
	for _, c := range m.Cooks {
		fmt.Fprintf(&b, "cook %s %s %s %s\n", c.Case, c.Unit, c.Root, repoRelative(c.File))
	}
	for _, cw := range m.CookWrites {
		// the instance is the whole line a driver needs: the two cooked files
		// the tool wrote are the ANSWER, and an answer never reaches a driver
		fmt.Fprintf(&b, "cook-write %s\n", cw.Instance)
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
	for _, r := range m.Refusals {
		// which forgeries the reason surfaces answer, and on which kind: the
		// reason itself is the expectation and stays withheld
		fmt.Fprintf(&b, "refusal %s %s\n", r.Forgery, r.Kind)
	}
	return writeFileAtomic(path, []byte(b.String()))
}

// expectation is what the harness compares a driver's output against.
type expectation struct {
	name string // the file the driver writes
	want []byte
	// damaged says the case is DAMAGED INPUT (docs/SPEC-TABLES.md §4):
	// the load is discarded whole, so the answer is scored on the refusal
	// alone, the report line saying `malformed`, and nothing after it.
	damaged bool
}

// hostileExpectations is the answer each port owes the hostile battery: the
// standard's verdict from a port the row names in carried-by, and the row's
// uncarried verdict from every other port, which still answers with the
// behaviour it had before the ruling. A row with no mark is owed by every port
// as the standard states it. The exemptions come back beside the expectations
// so a run prints them rather than skipping the row in silence.
func hostileExpectations(m *Manifest, lang string) ([]expectation, []string) {
	out := make([]expectation, 0, len(m.Hostiles))
	var exemptions []string
	for _, h := range m.Hostiles {
		out = append(out, expectation{name: h.Name, want: []byte(h.VerdictFor(lang) + "\n")})
		if !h.Carries(lang) && h.Uncarried != "" {
			exemptions = append(exemptions, fmt.Sprintf("%s / json-hostile / %s: the row is carried by %s; %s keeps %s (docs/SPEC-TABLES.md §16.3)",
				lang, h.Name, strings.Join(h.CarriedBy, ","), lang, h.Uncarried))
		}
	}
	return out, exemptions
}

func expectations(m *Manifest, surface string, reports map[string]Counts, jsonDir string) ([]expectation, error) {
	var out []expectation
	switch surface {
	case "wire", "json-read":
		for _, i := range m.Instances {
			if i.NoText && surface == "json-read" {
				continue // no text to read it FROM (§16.7)
			}
			want, err := os.ReadFile(i.Wire)
			if err != nil {
				return nil, err
			}
			out = append(out, expectation{name: i.Name, want: want})
		}
	case "message":
		// THE MESSAGE FORM (docs/SPEC-TABLES.md §3.3): read the message
		// against the connection's announced table and write it back. The
		// expectation is the message golden itself, exactly as the wire
		// surface's is the wire golden.
		// A ROW OVER A VARIABLE TABLE IS A NAMED REFUSAL (§3.3): it carries
		// no expected bytes, and scoreAnswer holds the leg to the reason.
		for _, msg := range m.Messages {
			if msg.Refused() {
				out = append(out, expectation{name: msg.Name})
				continue
			}
			want, err := os.ReadFile(msg.MessageWire)
			if err != nil {
				return nil, err
			}
			out = append(out, expectation{name: msg.Name, want: want})
		}
	case "retain":
		// RETAIN-UNKNOWN's COUNTERS (docs/SPEC-TABLES.md §6.6): the load's two
		// beside the `unknown` retention never moves, and the save's own
		// `retain_lost` behind them. The bytes are the other surface's, so each
		// of the two is one shape.
		for _, rc := range m.Retains {
			out = append(out, expectation{name: rc.Name, want: []byte(rc.Load.String() + " " + strconv.Itoa(rc.SaveLost) + "\n")})
		}
	case "retain-save":
		// AND THE BYTES THE PAIR WRITES BACK, which a counter cannot see: a
		// dropped record moves a field out of a body and every reference behind
		// it. A row that does not save is not a case here.
		for _, rc := range m.Retains {
			if rc.Saves == "" {
				continue
			}
			want, err := os.ReadFile(rc.Saves)
			if err != nil {
				return nil, err
			}
			out = append(out, expectation{name: rc.Name, want: want})
		}
	case "json-write":
		for _, i := range m.Instances {
			if i.NoText {
				continue // the form has no text for this one; no leg is asked for one
			}
			want, err := os.ReadFile(i.JSON)
			if err != nil {
				return nil, err
			}
			out = append(out, expectation{name: i.Name + ".json", want: want})
		}
	case "report":
		for _, r := range m.Reports {
			c, ok := reports[r.Name]
			if !ok {
				return nil, fmt.Errorf("reports.txt names no case %q — run: make conformance-generate", r.Name)
			}
			out = append(out, expectation{name: r.Name, want: []byte(c.String() + "\n"), damaged: c.Malformed})
		}
	case "framing":
		// THE FRAMING VECTORS (docs/LAYERS/1-framing.md): the report line the
		// row owes, then the §16 text of the value where the row owes one.
		// Both are hand-authored, so neither is regenerated from any leg.
		for _, fc := range m.Framings {
			want := []byte(fc.Verdict() + "\n")
			if fc.Value != "" {
				text, err := os.ReadFile(fc.Value)
				if err != nil {
					return nil, err
				}
				want = append(want, text...)
			}
			out = append(out, expectation{name: fc.Name, want: want, damaged: fc.Damaged})
		}
	case "cook":
		for _, c := range m.Cooks {
			want, err := os.ReadFile(c.Dump)
			if err != nil {
				return nil, err
			}
			out = append(out, expectation{name: c.Case, want: want})
		}
	case "cook-write":
		// THE TOOL IS THE REFERENCE (§7.6): a runtime writes the cook and the
		// bytes must be `schema cook`'s, in BOTH byte orders. The big-endian
		// half is not host-dependent — the order is a parameter of the write,
		// settled at cook time for the target build — so every leg answers both
		// on any host.
		for _, cw := range m.CookWrites {
			little, err := os.ReadFile(cw.Little)
			if err != nil {
				return nil, fmt.Errorf("%s: %w — run: make conformance-generate", cw.Instance, err)
			}
			big, err := os.ReadFile(cw.Big)
			if err != nil {
				return nil, fmt.Errorf("%s: %w — run: make conformance-generate", cw.Instance, err)
			}
			out = append(out, expectation{name: cw.Instance, want: little})
			out = append(out, expectation{name: cw.Instance + "-be", want: big})
		}
	case "json-hostile":
		for _, h := range m.Hostiles {
			out = append(out, expectation{name: h.Name, want: []byte(h.Verdict() + "\n")})
		}
	case "block":
		for _, b := range m.Blocks {
			out = append(out, expectation{name: b.Name, want: []byte("open\n")})
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
				out = append(out, expectation{name: b.Name, want: []byte("refuse\n")})
			}
			break
		}
		for _, c := range m.Cooks {
			out = append(out, expectation{name: c.Case, want: []byte("refuse\n")})
		}
	case "block-dump":
		for _, b := range m.Blocks {
			want, err := os.ReadFile(b.Dump)
			if err != nil {
				return nil, err
			}
			out = append(out, expectation{name: b.Name, want: want})
		}
	case "cook-reason", "block-reason":
		// THE REASON BESIDE THE NULL (docs/SPEC-TABLES.md §7, §19.2): one word
		// per forgery a `refusal` line names, the enum's own value name, so a
		// conformance row pins the first failing clause in every language
		kind := "block"
		if surface == "cook-reason" {
			kind = "cook"
		}
		for _, r := range m.Refusals {
			if r.Kind != kind {
				continue
			}
			out = append(out, expectation{name: r.Forgery, want: []byte(r.Reason + "\n")})
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
			out = append(out, expectation{name: f.Name, want: []byte(f.Verdict + "\n")})
		}
	default:
		return nil, fmt.Errorf("%q is not a surface", surface)
	}
	return out, nil
}

// skipSet reads --skip: the legs this run does not exercise, comma separated,
// which the Makefile fills from SCHEMA_SKIP_LEGS (issue #599). A skipped leg is
// PRINTED and left out, never passed over in silence, and the reference leg
// cannot be one: the corpus would lose its own expectation while every other
// leg kept comparing against nothing.
func skipSet(list string) (map[string]bool, error) {
	out := map[string]bool{}
	for s := range strings.SplitSeq(list, ",") {
		if s = strings.TrimSpace(s); s != "" {
			out[s] = true
		}
	}
	if out[referenceLang] {
		return nil, fmt.Errorf("--skip names the reference leg %s, which every other leg compares against", referenceLang)
	}
	return out, nil
}

func run(w io.Writer, m *Manifest, manifestPath, jsonDir, reportsPath, driversPath, work, only, skip string) (bool, error) {
	return runResults(w, m, manifestPath, jsonDir, reportsPath, driversPath, work, only, skip, "")
}

// runResults is run, and when resultsPath is set it also writes every case'"'"'s
// verdict there (writeResults), the input tools/matrix proof reads.
func runResults(w io.Writer, m *Manifest, manifestPath, jsonDir, reportsPath, driversPath, work, only, skip, resultsPath string) (bool, error) {
	drivers, discovered, err := loadDrivers(driversPath)
	if err != nil {
		return false, err
	}
	skipped, err := skipSet(skip)
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

	wireRefusals := map[string]Instance{}
	for _, instance := range m.Instances {
		if instance.GoWireRefusal != "" {
			wireRefusals[instance.Name] = instance
		}
	}
	results := map[string]map[string]*result{}
	var langs []string
	for i, d := range drivers {
		// THE REFERENCE LEG MAY NOT ANSWER ABSENT, AT EITHER GRAIN. It is the
		// first driver in the COMMITTED registry — the discovered one, where
		// the reference sorts first — and the one `conformance-pin` takes its
		// pins from, so an absence there is not a port's missing feature: it is
		// the corpus losing its own expectation, quietly, while every other leg
		// keeps comparing against nothing. That holds whether the absence is a
		// whole SURFACE (left out of `list`, or exit code 2) or a single CASE
		// (`<case>.absent`); per-case absence is safe exactly because this rule
		// stands beside it. A SUBSTITUTED registry is one leg of a port rather
		// than the matrix, so the rule does not reach it.
		reference := i == 0 && discovered
		if only != "" && d.lang != only {
			continue
		}
		if skipped[d.lang] {
			// on the same rule as the matrix below: a stdout that cannot be
			// written to is not a verdict this gate can improve on
			_, _ = io.WriteString(w, "conformance SKIPS the "+d.lang+" leg: --skip names it (SCHEMA_SKIP_LEGS)\n")
			continue
		}
		langs = append(langs, d.lang)
		results[d.lang] = map[string]*result{}

		listed, err := listSurfaces(d, derived)
		if err != nil {
			return false, fmt.Errorf("%s: %w", d.lang, err)
		}
		for _, s := range surfaces {
			legWant := want[s]
			var exemptions []string
			if s == "json-hostile" {
				legWant, exemptions = hostileExpectations(m, d.lang)
			}
			r := &result{total: len(legWant)}
			results[d.lang][s] = r
			if !listed[s] {
				r.absent = true
				for _, e := range legWant {
					r.verdict(e.name, "absent")
				}
				if reference {
					r.failures = append(r.failures, absentReference(s, "`list` does not name it"))
				}
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
				for _, e := range legWant {
					r.verdict(e.name, "absent")
				}
				if reference {
					r.failures = append(r.failures, absentReference(s, "the driver exited 2"))
				}
				continue
			}
			if code != 0 {
				for _, e := range legWant {
					r.verdict(e.name, "fail")
				}
				r.failures = append(r.failures, fmt.Sprintf("the driver exited %d\n%s", code, indent(stderr, "    ")))
				continue
			}
			for _, line := range exemptions {
				_, _ = io.WriteString(w, "conformance EXEMPTS "+line+"\n")
			}
			for _, e := range legWant {
				pass, named, missing := r.pass, r.named, r.missing
				scoreAnswer(d.lang, s, out, e, wireRefusals[e.name], r)
				switch {
				case r.pass > pass:
					r.verdict(e.name, "pass")
				case r.named > named:
					r.verdict(e.name, "named")
				case r.missing > missing:
					r.verdict(e.name, "absent")
				default:
					r.verdict(e.name, "fail")
				}
			}
			if reference && r.missing > r.named {
				r.failures = append(r.failures, fmt.Sprintf(
					"the REFERENCE leg answered ABSENT for %d case(s) — an absence here is the corpus "+
						"losing its own expectation, not a missing feature; the reference answers every case "+
						"it registers a surface for", r.missing-r.named))
			}
		}
	}

	if resultsPath != "" {
		if err := writeResults(resultsPath, langs, results); err != nil {
			return false, err
		}
	}
	return report(w, langs, results), nil
}

// scoreAnswer scores one case written by a leg. The blob-shared marker
// changes that one wire expectation from bytes to a named writer refusal, for
// Go and for every port refused-by= names (inst.WireRefusal); inst is the zero
// Instance for a case that carries no marker.
func scoreAnswer(lang, surface, out string, e expectation, inst Instance, r *result) {
	answer := filepath.Join(out, e.name)
	if surface == "message" && e.want == nil {
		// THE NAMED REFUSAL ROW (docs/SPEC-TABLES.md §3.3): a message row over
		// a variable table, which no leg carries, and every leg names why.
		if _, err := os.Stat(answer); err == nil {
			r.failures = append(r.failures, fmt.Sprintf("%s: a variable table carries no message form, and the leg wrote bytes", e.name))
			return
		}
		reason, err := os.ReadFile(answer + ".absent")
		if err != nil {
			r.failures = append(r.failures, fmt.Sprintf("%s: expected named absence %q, driver wrote nothing", e.name, VariableMessageRefusal))
			return
		}
		if string(reason) != VariableMessageRefusal {
			r.failures = append(r.failures, fmt.Sprintf("%s: absent reason %q, want %q", e.name, string(reason), VariableMessageRefusal))
			return
		}
		r.missing++
		r.named++
		return
	}
	if goWireRefusal := inst.WireRefusal(lang); surface == "wire" && goWireRefusal != "" {
		if _, err := os.Stat(answer); err == nil {
			r.failures = append(r.failures, fmt.Sprintf("%s: expected named writer refusal %q, got bytes", e.name, goWireRefusal))
			return
		} else if !errors.Is(err, os.ErrNotExist) {
			r.failures = append(r.failures, fmt.Sprintf("%s: checking wire answer: %v", e.name, err))
			return
		}
		marker := answer + ".absent"
		reason, err := os.ReadFile(marker)
		if err != nil {
			r.failures = append(r.failures, fmt.Sprintf("%s: expected named absence %q, driver wrote nothing", e.name, goWireRefusal))
			return
		}
		if string(reason) != goWireRefusal {
			r.failures = append(r.failures, fmt.Sprintf("%s: absent reason %q, want %q", e.name, string(reason), goWireRefusal))
			return
		}
		r.missing++
		r.named++
		return
	}
	// ANOTHER LEG MAY GIVE THE SAME NAMED REFUSAL, and must give it by that
	// name: §2.5 makes every writer refuse the value. A leg that still writes
	// bytes is held to them below, as before.
	if surface == "wire" && inst.GoWireRefusal != "" {
		if _, err := os.Stat(answer); errors.Is(err, os.ErrNotExist) {
			if reason, err := os.ReadFile(answer + ".absent"); err == nil && string(reason) == inst.GoWireRefusal {
				r.missing++
				r.named++
				return
			}
		}
	}

	got, err := os.ReadFile(answer)
	if err != nil {
		if _, absent := os.Stat(answer + ".absent"); absent == nil {
			// An ordinary per-case absence is a missing feature for this leg.
			r.missing++
			return
		}
		r.failures = append(r.failures, fmt.Sprintf("%s: the driver wrote nothing", e.name))
		return
	}
	if e.damaged {
		if !answersDamage(got) {
			r.failures = append(r.failures, fmt.Sprintf("%s: damaged input owes the refusal, a report line saying malformed; got %q", e.name, firstLine(got)))
			return
		}
		r.pass++
		return
	}
	if !bytes.Equal(got, e.want) {
		r.failures = append(r.failures, describeDiff(e.name, e.want, got))
		return
	}
	r.pass++
}

// answersDamage reports whether a leg's answer opens with a report line that
// says `malformed`. DAMAGE DISCARDS THE WHOLE LOAD (docs/SPEC-TABLES.md §4):
// the counters and any value after the line are undefined and are not read.
func answersDamage(got []byte) bool {
	c, err := ParseCounts(firstLine(got))
	return err == nil && c.Malformed
}

func firstLine(b []byte) string {
	if i := bytes.IndexByte(b, '\n'); i >= 0 {
		return string(b[:i])
	}
	return string(b)
}

// absentReference is what the REFERENCE leg's absence reads as in the failure
// list: the surface, how it went absent, and why that is red where the same
// answer from a port is an ordinary missing feature.
func absentReference(surface, how string) string {
	return fmt.Sprintf(
		"the REFERENCE leg is ABSENT on the whole %s surface (%s) — an absence here is the corpus "+
			"losing its own expectation, not a missing feature; every other leg would keep comparing "+
			"against a surface nothing pins", surface, how)
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

// report prints the matrix and returns the verdict. THE VERDICT IS THE FAILURE
// LIST and nothing else, so that a cell printing something other than FAIL — an
// ABSENT one — cannot lose a failure recorded against it and the success footer
// cannot print beside one.
func report(w io.Writer, langs []string, results map[string]map[string]*result) bool {
	var b strings.Builder
	// wide enough for "pass 111/111 +4a", the widest cell the matrix can print
	width := 18
	for _, s := range surfaces {
		if len(s)+2 > width {
			width = len(s) + 2
		}
	}
	b.WriteString("\nTABLES CONFORMANCE — surface x language\n\n")
	fmt.Fprintf(&b, "%-14s", "surface")
	for _, l := range langs {
		fmt.Fprintf(&b, "%-*s", width, l)
	}
	fmt.Fprintf(&b, "\n%s\n", strings.Repeat("-", 14+width*len(langs)))
	for _, s := range surfaces {
		fmt.Fprintf(&b, "%-14s", s)
		for _, l := range langs {
			r := results[l][s]
			var cell string
			switch {
			case len(r.failures) > 0 && r.absent:
				// the REFERENCE leg's absence: what it did, and what that is.
				// "FAIL 0/13" would claim it ran the surface and answered
				// nothing, and it never ran the surface at all.
				cell = "FAIL absent"
			case len(r.failures) > 0:
				cell = fmt.Sprintf("FAIL %d/%d", r.pass, r.total)
			case r.absent:
				cell = "absent"
			case r.missing > 0:
				// what it answered, and what it said it cannot: the cell is the
				// completion tracker, so an absence stays visible rather than
				// being rounded away into a smaller total
				cell = fmt.Sprintf("pass %d/%d +%da", r.pass, r.total-r.missing, r.missing)
			default:
				cell = fmt.Sprintf("pass %d/%d", r.pass, r.total)
			}
			fmt.Fprintf(&b, "%-*s", width, cell)
		}
		b.WriteString("\n")
	}
	b.WriteString("\n")

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
		fmt.Fprintf(&b, "FAILURES\n%s\n\n", strings.Join(lines, "\n"))
	} else {
		b.WriteString("tables conformance: every registered surface passes\n")
	}
	// the matrix goes out in one write; a stdout that cannot be written to is
	// not a verdict this gate can improve on
	_, _ = io.WriteString(w, b.String())
	return len(lines) == 0
}
