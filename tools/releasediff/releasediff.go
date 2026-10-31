// Command releasediff is the PREVIOUS-RELEASE DIFFERENTIAL
// (docs/VERSIONING.md, "The packet wire and the protocol id"; #463): the
// WIRE bytes the previous release encodes and the bytes the new one encodes,
// for the same instances of the same units, compared under an equal protocol
// id.
//
//	releasediff [-prev vX.Y.Z] [-rev REV] [-old-bin PATH] [-new-bin PATH]
//	            [-corpus DIR] [-serialize DIR] [-work DIR] [-j N]
//
// The invariant is the page's: no encoded byte may change for the same schema
// and the same values without the protocol id changing. Generated SOURCE is
// not compared; it changes in every release by design. Both compilers, the
// newest v tag's (-prev) and REV's (default HEAD), are built from their own
// trees, and the corpus is REV's tree. Each wire is encoded the same way on
// both sides:
//
//   - THE PACKET WIRE, by the C++ reference. Each packet driver (the Makefile's
//     own: test/main.cpp over examples, test/ludicrous_main.cpp over
//     examples128, test/bench/main.cpp over the bench units) is compiled once
//     against each compiler's generated C++ and run with
//     SCHEMA_UPDATE_WIRE_GOLDENS=1 into an empty testdata/wire, so it writes
//     the bytes of every instance it pins. The SAME driver source runs on
//     both sides: REV's when it builds against both, else the previous
//     release's, else the driver is listed.
//   - THE TABLE FORMS, by the Go engine each compiler carries (`schema pack`):
//     every `instance` of testdata/conformance/tables/MANIFEST.txt that
//     carries text, packed from its JSON in the variable form and in the
//     message form.
//
// A unit whose protocol id is EQUAL under both compilers is compared, and a
// differing byte refuses, naming the unit, the instance and the first
// differing byte; so does a packet driver that passes its own checks on one
// side only, since those checks read the bytes it wrote. A unit whose id moved is listed, not compared; so is a unit
// either compiler refuses, a driver that builds on one side only, and an
// instance one side refuses to encode.
//
// It exits 1 on a difference and 0 otherwise. -old-bin and -new-bin take
// prebuilt compilers (the negative control hands it a sabotaged one), and
// -corpus reads the corpus from a directory instead of REV's tree.
//
// The decisions are pure functions (ParseManifest, FirstDifference,
// CompareTrees, Judge) unit-tested with no git and no compiler; only main
// execs git, go, the C++ compiler and the two schema compilers.
package main

import (
	"archive/tar"
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
)

// PacketDriver is one C++ reference driver of the packet wire, as the
// Makefile builds it: its units are generated into one directory, and its
// sources are compiled against it.
type PacketDriver struct {
	Name        string
	Units       [][]string // every unit must keep its id for the driver to be compared
	Sources     []string   // relative to the driver's tree
	TestInclude bool       // -I<tree>/test, as build/schema_test takes it
}

// PacketDrivers are the Makefile's packet drivers that pin wire bytes:
// build/schema_test, build/schema_test_ludicrous and build/schema_test_bench.
var PacketDrivers = []PacketDriver{
	{Name: "examples", Units: [][]string{{"examples"}}, Sources: []string{"test/main.cpp", "test/second.cpp"}, TestInclude: true},
	{Name: "examples128", Units: [][]string{{"examples128"}}, Sources: []string{"test/ludicrous_main.cpp"}},
	{Name: "bench", Units: [][]string{{"bench/corpus/Bench.schema"}, {"bench/corpus/RealWorld.schema"}}, Sources: []string{"test/bench/main.cpp"}},
}

// ManifestPath is the tables conformance registry, under the corpus root.
const ManifestPath = "testdata/conformance/tables/MANIFEST.txt"

// Instance is one manifest `instance` that carries text.
type Instance struct {
	Name, Unit, Root string
}

// Manifest is what the table leg reads out of MANIFEST.txt.
type Manifest struct {
	Units     map[string][]string // key -> schema paths
	Keys      []string            // unit keys, in manifest order
	Instances []Instance          // in manifest order, `no-text` ones left out
}

// ParseManifest reads the `unit` and `instance` lines of MANIFEST.txt.
func ParseManifest(text string) (*Manifest, error) {
	m := &Manifest{Units: map[string][]string{}}
	for line := range strings.SplitSeq(text, "\n") {
		f := strings.Fields(line)
		if len(f) == 0 || strings.HasPrefix(f[0], "#") {
			continue
		}
		switch f[0] {
		case "unit":
			if len(f) < 3 {
				return nil, fmt.Errorf("manifest: a unit line names no schema: %q", line)
			}
			if _, dup := m.Units[f[1]]; dup {
				return nil, fmt.Errorf("manifest: unit %s twice", f[1])
			}
			m.Units[f[1]] = f[2:]
			m.Keys = append(m.Keys, f[1])
		case "instance":
			if len(f) < 5 {
				return nil, fmt.Errorf("manifest: a short instance line: %q", line)
			}
			if slices.Contains(f[5:], "no-text") {
				continue
			}
			m.Instances = append(m.Instances, Instance{Name: f[1], Unit: f[2], Root: f[3]})
		}
	}
	for _, in := range m.Instances {
		if _, ok := m.Units[in.Unit]; !ok {
			return nil, fmt.Errorf("manifest: instance %s names unit %s, which no unit line declares", in.Name, in.Unit)
		}
	}
	if len(m.Instances) == 0 {
		return nil, errors.New("manifest: no instance carries text; the table leg would compare nothing")
	}
	return m, nil
}

// FirstDifference is the offset of the first byte at which a and b differ,
// the shorter length when one is a prefix of the other, and -1 when equal.
func FirstDifference(a, b []byte) int {
	n := min(len(a), len(b))
	for i := range n {
		if a[i] != b[i] {
			return i
		}
	}
	if len(a) != len(b) {
		return n
	}
	return -1
}

// Difference is the first place two encodings disagree.
type Difference struct {
	File   string // slash-separated, relative to the tree; empty for one file
	Offset int    // the first differing byte; -1 when the file is on one side only
	Old    int    // the byte there in the previous encoding, -1 past its end or absent
	New    int    // the byte there in the new encoding, -1 past its end or absent
	OldLen int
	NewLen int
	Files  int // how many files differ in all, this one included
}

func (d *Difference) String() string {
	name := d.File
	if name != "" {
		name += " "
	}
	if d.Offset < 0 {
		side := "the previous release only"
		if d.Old < 0 {
			side = "the new compiler only"
		}
		return fmt.Sprintf("%sis written by %s (%d file(s) differ)", name, side, d.Files)
	}
	s := fmt.Sprintf("%sfirst differs at byte %d of %d/%d: previous %s, new %s", name, d.Offset, d.OldLen, d.NewLen, byteText(d.Old), byteText(d.New))
	if d.Files > 1 {
		s += fmt.Sprintf(" (%d files differ)", d.Files)
	}
	return s
}

func byteText(b int) string {
	if b < 0 {
		return "end"
	}
	return fmt.Sprintf("0x%02x", b)
}

// CompareBytes is nil when a and b are equal, or their first difference.
func CompareBytes(a, b []byte) *Difference {
	off := FirstDifference(a, b)
	if off < 0 {
		return nil
	}
	return &Difference{Offset: off, Old: at(a, off), New: at(b, off), OldLen: len(a), NewLen: len(b), Files: 1}
}

func at(b []byte, i int) int {
	if i < len(b) {
		return int(b[i])
	}
	return -1
}

func listing(root string) (map[string]bool, error) {
	files := map[string]bool{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(rel)] = true
		return nil
	})
	return files, err
}

// CompareTrees compares two trees of encodings, listing and bytes, and
// returns nil when they are the same, or the first difference in path order,
// with the number of files both sides wrote. With bothComplete false a file
// on one side only is not a difference: a driver that stopped early wrote
// less, and what it did write still compares.
func CompareTrees(oldDir, newDir string, bothComplete bool) (*Difference, int, error) {
	oldFiles, err := listing(oldDir)
	if err != nil {
		return nil, 0, err
	}
	newFiles, err := listing(newDir)
	if err != nil {
		return nil, 0, err
	}
	all := map[string]bool{}
	for f := range oldFiles {
		all[f] = true
	}
	for f := range newFiles {
		all[f] = true
	}
	names := make([]string, 0, len(all))
	for f := range all {
		names = append(names, f)
	}
	slices.Sort(names)
	var first *Difference
	count, compared := 0, 0
	for _, f := range names {
		var d *Difference
		switch {
		case !newFiles[f] || !oldFiles[f]:
			if !bothComplete {
				continue
			}
			d = &Difference{File: f, Offset: -1, Old: 0, New: -1}
			if !oldFiles[f] {
				d.Old, d.New = -1, 0
			}
		default:
			compared++
			a, err := os.ReadFile(filepath.Join(oldDir, filepath.FromSlash(f)))
			if err != nil {
				return nil, 0, err
			}
			b, err := os.ReadFile(filepath.Join(newDir, filepath.FromSlash(f)))
			if err != nil {
				return nil, 0, err
			}
			if d = CompareBytes(a, b); d == nil {
				continue
			}
			d.File = f
		}
		count++
		if first == nil {
			first = d
		}
	}
	if first != nil {
		first.Files = count
	}
	return first, compared, nil
}

// Compiler is one side of the differential.
type Compiler interface {
	// Name says which side it is, in every line the gate prints.
	Name() string
	// ID is the unit's protocol id, or an error when it refuses the unit.
	ID(unit []string) (string, error)
	// Packet builds driver d against this compiler's generated C++ from the
	// driver sources under tree, runs it, and leaves the wire files it wrote
	// under out/testdata/wire. ran is false when it did not build; err is
	// the build's or the run's failure.
	Packet(d PacketDriver, tree, out string) (ran bool, err error)
	// Pack encodes one instance's JSON text under the unit, in the variable
	// form or the message form, or returns an error when it refuses.
	Pack(unit []string, root, json string, message bool) ([]byte, error)
}

// Report is everything one run found.
type Report struct {
	Compared    []string // what was compared, and how many encodings
	Moved       []string // "unit: old -> new"
	Unreadable  []string // a unit one side refuses
	Uncompared  []string // a driver or an instance one side cannot encode
	Differences []string // the refusals
}

// Judge runs the differential: the packet drivers, whose sources are looked
// for under each of trees in order, and the manifest's table instances, whose
// text is under jsonDir, with work as scratch and at most jobs encodings at
// once.
func Judge(old, new Compiler, trees []string, m *Manifest, jsonDir, work string, jobs int) (*Report, error) {
	r := &Report{}
	var mu sync.Mutex
	add := func(list *[]string, s string) {
		mu.Lock()
		*list = append(*list, s)
		mu.Unlock()
	}

	// the protocol id of every unit either leg names, once
	ids := map[string]bool{} // unit -> its id is equal
	check := func(u []string) bool {
		name := strings.Join(u, " ")
		if eq, ok := ids[name]; ok {
			return eq
		}
		oid, oerr := old.ID(u)
		nid, nerr := new.ID(u)
		eq := false
		switch {
		case oerr != nil && nerr != nil:
			r.Unreadable = append(r.Unreadable, fmt.Sprintf("%s: both compilers refuse it", name))
		case oerr != nil:
			r.Unreadable = append(r.Unreadable, fmt.Sprintf("%s: %s refuses it (%s)", name, old.Name(), firstLine(oerr)))
		case nerr != nil:
			r.Unreadable = append(r.Unreadable, fmt.Sprintf("%s: %s refuses it (%s)", name, new.Name(), firstLine(nerr)))
		case oid != nid:
			r.Moved = append(r.Moved, fmt.Sprintf("%s: %s -> %s", name, oid, nid))
		default:
			eq = true
		}
		ids[name] = eq
		return eq
	}

	var drivers []PacketDriver
	for _, d := range PacketDrivers {
		all := true
		for _, u := range d.Units {
			all = check(u) && all
		}
		if all {
			drivers = append(drivers, d)
		}
	}
	type tableJob struct {
		inst Instance
		unit []string
	}
	var tableJobs []tableJob
	for _, in := range m.Instances {
		u := m.Units[in.Unit]
		if check(u) {
			tableJobs = append(tableJobs, tableJob{in, u})
		}
	}

	sem := make(chan struct{}, max(jobs, 1))
	var wg sync.WaitGroup
	var fatal error
	fail := func(err error) {
		mu.Lock()
		fatal = errors.Join(fatal, err)
		mu.Unlock()
	}

	// THE PACKET WIRE: one driver source on both sides, the first tree whose
	// copy builds against both compilers' C++
	for _, d := range drivers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			var why []string
			for ti, tree := range trees {
				if _, err := os.Stat(filepath.Join(tree, d.Sources[0])); err != nil {
					continue
				}
				base := filepath.Join(work, "packet", d.Name, strconv.Itoa(ti))
				od, nd := filepath.Join(base, "old"), filepath.Join(base, "new")
				oran, oerr := old.Packet(d, tree, od)
				nran, nerr := new.Packet(d, tree, nd)
				if !oran || !nran {
					side, err := old.Name(), oerr
					if oran {
						side, err = new.Name(), nerr
					}
					why = append(why, fmt.Sprintf("the driver from tree %d does not build against %s's C++ (%s)", ti, side, firstLine(err)))
					continue
				}
				diff, n, err := CompareTrees(filepath.Join(od, "testdata", "wire"), filepath.Join(nd, "testdata", "wire"), oerr == nil && nerr == nil)
				if err != nil {
					fail(err)
					return
				}
				// the same driver, the same instances, one id: a run that
				// passes its own checks on one side only is a difference,
				// since the driver's checks read the bytes it wrote
				if (oerr == nil) != (nerr == nil) {
					side, other, e := new.Name(), old.Name(), nerr
					if oerr != nil {
						side, other, e = old.Name(), new.Name(), oerr
					}
					add(&r.Differences, fmt.Sprintf("packet wire, unit %s: the driver completes against %s and fails its own check against %s (%s)", unitNames(d.Units), other, side, firstLine(e)))
				}
				note := ""
				if oerr != nil || nerr != nil {
					note = fmt.Sprintf("; a run stopped early, so only what both wrote compares (previous: %s, new: %s)", firstLineOr(oerr), firstLineOr(nerr))
				}
				if n == 0 {
					add(&r.Uncompared, fmt.Sprintf("packet %s: the two runs wrote no encoding in common%s", d.Name, note))
					return
				}
				add(&r.Compared, fmt.Sprintf("packet wire, driver %s (%s, tree %d): %d encodings, C++ reference%s", d.Name, strings.Join(d.Sources, " "), ti, n, note))
				if diff != nil {
					add(&r.Differences, fmt.Sprintf("packet wire, unit %s, instance %s", unitNames(d.Units), diff))
				}
				return
			}
			add(&r.Uncompared, fmt.Sprintf("packet %s: no driver source builds on both sides: %s", d.Name, strings.Join(why, "; ")))
		}()
	}

	// THE TABLE FORMS: the Go engine each compiler carries
	var tableCompared [2]int
	for _, j := range tableJobs {
		for _, message := range []bool{false, true} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				form, k := "variable form", 0
				if message {
					form, k = "message form", 1
				}
				json := filepath.Join(jsonDir, j.inst.Name+".json")
				a, oerr := old.Pack(j.unit, j.inst.Root, json, message)
				b, nerr := new.Pack(j.unit, j.inst.Root, json, message)
				switch {
				case oerr != nil && nerr != nil:
					// a form neither side writes for this instance
				case oerr != nil:
					add(&r.Uncompared, fmt.Sprintf("table %s, unit %s, instance %s: %s refuses it (%s)", form, j.inst.Unit, j.inst.Name, old.Name(), firstLine(oerr)))
				case nerr != nil:
					add(&r.Uncompared, fmt.Sprintf("table %s, unit %s, instance %s: %s refuses it (%s)", form, j.inst.Unit, j.inst.Name, new.Name(), firstLine(nerr)))
				default:
					mu.Lock()
					tableCompared[k]++
					mu.Unlock()
					if d := CompareBytes(a, b); d != nil {
						add(&r.Differences, fmt.Sprintf("table %s, unit %s, instance %s: %s", form, j.inst.Unit, j.inst.Name, d))
					}
				}
			}()
		}
	}
	wg.Wait()
	if fatal != nil {
		return nil, fatal
	}
	if tableCompared[0]+tableCompared[1] > 0 {
		r.Compared = append(r.Compared, fmt.Sprintf("table forms: %d variable-form and %d message-form encodings of %s, Go engine", tableCompared[0], tableCompared[1], ManifestPath))
	}
	for _, l := range [][]string{r.Compared, r.Uncompared, r.Differences} {
		slices.Sort(l)
	}
	return r, nil
}

func unitNames(units [][]string) string {
	var out []string
	for _, u := range units {
		out = append(out, strings.Join(u, " "))
	}
	return strings.Join(out, " + ")
}

func firstLine(err error) string {
	if err == nil {
		return ""
	}
	s := strings.TrimSpace(err.Error())
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 200 {
		s = s[:200] + "..."
	}
	return s
}

func firstLineOr(err error) string {
	if err == nil {
		return "complete"
	}
	return firstLine(err)
}

// TableFormsNote says why the table side of a run against a 2.x release is
// thin: the table wire's promises are made at 3.0.0.
const TableFormsNote = "the table forms are compared from 3.0.0 on; before 3.0.0 the previous release cannot read most table units, so they are listed, not compared"

// Print writes the report: what was compared, what was listed and why, and
// every difference.
func (r *Report) Print(w io.Writer, oldName, newName string) {
	_, _ = fmt.Fprintf(w, "release-differential: %s -> %s, wire bytes under an equal protocol id\n", oldName, newName)
	section := func(title string, lines []string) {
		for _, l := range lines {
			_, _ = fmt.Fprintf(w, "  %-12s %s\n", title, l)
		}
	}
	_, _ = fmt.Fprintf(w, "  %-12s %s\n", "note", TableFormsNote)
	section("compared", r.Compared)
	section("id moved", r.Moved)
	section("unreadable", r.Unreadable)
	section("uncompared", r.Uncompared)
	for _, d := range r.Differences {
		_, _ = fmt.Fprintf(w, "DIFFERENCE under an equal protocol id: %s\n", d)
	}
	if len(r.Differences) > 0 {
		_, _ = fmt.Fprintf(w, "release-differential: REFUSED, %d encoding(s) differ under an equal protocol id\n", len(r.Differences))
		return
	}
	_, _ = fmt.Fprintf(w, "release-differential: ok, every encoding compared is the same bytes (%d comparison line(s), %d unit(s) with a moved id)\n", len(r.Compared), len(r.Moved))
}

// binary is a schema compiler binary, run with the corpus root as its
// directory.
type binary struct {
	name, path, corpus, serialize, cxx, scratch string
}

func (b binary) Name() string { return b.name }

func run1(dir string, env []string, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if env != nil {
		cmd.Env = append(os.Environ(), env...)
	}
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String() + "\n" + out.String())
		// a refusal's own line, not the warnings before it
		for l := range strings.SplitSeq(msg, "\n") {
			if strings.HasPrefix(l, "schema:") || strings.Contains(l, "error") || strings.HasPrefix(l, "FAILED") {
				return "", errors.New(strings.TrimSpace(l))
			}
		}
		return "", fmt.Errorf("%v: %s", err, msg)
	}
	return out.String(), nil
}

func (b binary) ID(unit []string) (string, error) {
	out, err := run1(b.corpus, nil, b.path, append([]string{"id"}, unit...)...)
	if err != nil {
		return "", err
	}
	f := strings.Fields(out)
	for i := len(f) - 1; i >= 0; i-- {
		if strings.HasPrefix(f[i], "0x") {
			return f[i], nil
		}
	}
	return "", fmt.Errorf("`id` printed no id: %q", out)
}

func (b binary) Packet(d PacketDriver, tree, out string) (bool, error) {
	gen := filepath.Join(out, "generated")
	if err := os.MkdirAll(gen, 0o755); err != nil {
		return false, err
	}
	for _, u := range d.Units {
		if _, err := run1(b.corpus, nil, b.path, append([]string{"generate", "--lang", "cpp", "--out", gen}, u...)...); err != nil {
			return false, err
		}
	}
	exe := filepath.Join(out, "driver")
	// -O0: the bench driver takes the address of always_inline functions;
	// a warning is no wire fact, so none is an error here
	args := []string{"-std=c++17", "-w", "-O0", "-ffp-contract=off", "-I" + b.serialize, "-I" + gen}
	if d.TestInclude {
		args = append(args, "-I"+filepath.Join(tree, "test"))
	}
	for _, s := range d.Sources {
		args = append(args, filepath.Join(tree, s))
	}
	args = append(args, "-o", exe)
	if _, err := run1(out, nil, b.cxx, args...); err != nil {
		return false, err
	}
	// an empty testdata/wire with the corpus's own subdirectories, so every
	// pin the driver writes lands, and nothing it would compare is there
	src := filepath.Join(b.corpus, "testdata", "wire")
	wire := filepath.Join(out, "testdata", "wire")
	err := filepath.WalkDir(src, func(p string, e fs.DirEntry, err error) error {
		if err != nil || !e.IsDir() {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		return os.MkdirAll(filepath.Join(wire, rel), 0o755)
	})
	if err != nil {
		return false, err
	}
	_, err = run1(out, []string{"SCHEMA_UPDATE_WIRE_GOLDENS=1"}, exe)
	return true, err
}

func (b binary) Pack(unit []string, root, json string, message bool) ([]byte, error) {
	dir, err := os.MkdirTemp(b.scratch, "pack-")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	tree := filepath.Join(dir, "tree")
	if err := os.MkdirAll(tree, 0o755); err != nil {
		return nil, err
	}
	text, err := os.ReadFile(json)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(tree, root+".json"), text, 0o644); err != nil {
		return nil, err
	}
	out := filepath.Join(dir, "out.bin")
	args := []string{"pack", "--root", root, "--out", out}
	if message {
		args = append(args, "--message")
	}
	args = append(args, tree)
	args = append(args, unit...)
	if _, err := run1(b.corpus, nil, b.path, args...); err != nil {
		return nil, err
	}
	return os.ReadFile(out)
}

func git(args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return out, nil
}

var versionForm = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

// NewestRelease is the greatest vMAJOR.MINOR.PATCH among tags, in numeric
// order; anything else (promotion tags, pre-releases) is ignored.
func NewestRelease(tags []string) (string, bool) {
	var best [3]uint64
	name, found := "", false
	for _, t := range tags {
		m := versionForm.FindStringSubmatch(t)
		if m == nil {
			continue
		}
		var v [3]uint64
		for i := range v {
			v[i], _ = strconv.ParseUint(m[i+1], 10, 64)
		}
		if !found || slices.Compare(best[:], v[:]) < 0 {
			best, name, found = v, t, true
		}
	}
	return name, found
}

// extract writes rev's tree into dir, from `git archive`.
func extract(rev, dir string) error {
	cmd := exec.Command("git", "archive", "--format=tar", rev)
	cmd.Stderr = os.Stderr
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	tr := tar.NewReader(pipe)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		p := filepath.Join(dir, filepath.FromSlash(h.Name))
		if !strings.HasPrefix(p, filepath.Clean(dir)+string(os.PathSeparator)) {
			return fmt.Errorf("archive entry %q leaves %s", h.Name, dir)
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(p, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, fs.FileMode(h.Mode)&0o777)
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil {
				_ = f.Close()
				return err
			}
			if err := f.Close(); err != nil {
				return err
			}
		case tar.TypeSymlink:
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				return err
			}
			if err := os.Symlink(h.Linkname, p); err != nil {
				return err
			}
		}
	}
	return cmd.Wait()
}

// build compiles ./cmd/schema of the tree at src into out.
func build(src, out string) error {
	cmd := exec.Command("go", "build", "-o", out, "./cmd/schema")
	cmd.Dir = src
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func main() {
	prev := flag.String("prev", "", "the previous release, a v tag (default: the newest v tag)")
	rev := flag.String("rev", "HEAD", "the commit whose compiler and corpus are the new side")
	oldBin := flag.String("old-bin", "", "a prebuilt previous compiler (default: build -prev's ./cmd/schema)")
	newBin := flag.String("new-bin", "", "a prebuilt new compiler (default: build -rev's ./cmd/schema)")
	corpus := flag.String("corpus", "", "read the corpus from this directory (default: -rev's tree)")
	serialize := flag.String("serialize", "../serialize", "the serialize C++ runtime the packet drivers include")
	work := flag.String("work", "build/release-differential", "the scratch directory, emptied first")
	jobs := flag.Int("j", runtime.NumCPU(), "encodings at once")
	flag.Parse()
	if err := run(*prev, *rev, *oldBin, *newBin, *corpus, *serialize, *work, *jobs); err != nil {
		if !errors.Is(err, errRefused) {
			fmt.Fprintf(os.Stderr, "release-differential: %v\n", err)
		}
		os.Exit(1)
	}
}

// errRefused is the verdict's own exit: the report has already said why.
var errRefused = errors.New("refused")

func run(prev, rev, oldBin, newBin, corpus, serialize, work string, jobs int) error {
	work, err := filepath.Abs(work)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(work); err != nil {
		return err
	}
	if err := os.MkdirAll(work, 0o755); err != nil {
		return err
	}
	if prev == "" {
		tags, err := git("tag", "--list", "v*")
		if err != nil {
			return err
		}
		var ok bool
		if prev, ok = NewestRelease(strings.Fields(string(tags))); !ok {
			return errors.New("no v tag: there is no previous release to compare against")
		}
	}
	sha, err := git("rev-parse", "--verify", rev+"^{commit}")
	if err != nil {
		return err
	}
	newName := fmt.Sprintf("%s (%s)", rev, strings.TrimSpace(string(sha))[:12])

	// the extracted trees are removed at the end: a second repository under
	// build/ is a second repository to every walk of this one
	if corpus == "" || newBin == "" {
		src := filepath.Join(work, "new-src")
		if err := extract(rev, src); err != nil {
			return fmt.Errorf("extracting %s: %w", rev, err)
		}
		defer func() { _ = os.RemoveAll(src) }()
		if corpus == "" {
			corpus = src
		}
		if newBin == "" {
			newBin = filepath.Join(work, "schema-new")
			if err := build(src, newBin); err != nil {
				return fmt.Errorf("building %s's compiler: %w", rev, err)
			}
		}
	}
	if corpus, err = filepath.Abs(corpus); err != nil {
		return err
	}
	trees := []string{corpus}
	if oldBin == "" {
		src := filepath.Join(work, "old-src")
		if err := extract(prev, src); err != nil {
			return fmt.Errorf("extracting %s: %w", prev, err)
		}
		defer func() { _ = os.RemoveAll(src) }()
		oldBin = filepath.Join(work, "schema-old")
		if err := build(src, oldBin); err != nil {
			return fmt.Errorf("building %s's compiler: %w", prev, err)
		}
		trees = append(trees, src) // the previous release's own drivers
	}
	if oldBin, err = filepath.Abs(oldBin); err != nil {
		return err
	}
	if newBin, err = filepath.Abs(newBin); err != nil {
		return err
	}
	if serialize, err = filepath.Abs(serialize); err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(serialize, "serialize.h")); err != nil {
		return fmt.Errorf("the packet drivers need the serialize C++ runtime (-serialize): %w", err)
	}
	text, err := os.ReadFile(filepath.Join(corpus, ManifestPath))
	if err != nil {
		return err
	}
	m, err := ParseManifest(string(text))
	if err != nil {
		return err
	}
	cxx := os.Getenv("CXX")
	if cxx == "" {
		cxx = "c++"
	}
	scratch := filepath.Join(work, "scratch")
	if err := os.MkdirAll(scratch, 0o755); err != nil {
		return err
	}
	old := binary{name: prev, path: oldBin, corpus: corpus, serialize: serialize, cxx: cxx, scratch: scratch}
	nw := binary{name: newName, path: newBin, corpus: corpus, serialize: serialize, cxx: cxx, scratch: scratch}
	r, err := Judge(old, nw, trees, m, filepath.Join(corpus, "testdata", "conformance", "tables", "json"), work, jobs)
	if err != nil {
		return err
	}
	r.Print(os.Stdout, old.Name(), nw.Name())
	if len(r.Differences) > 0 {
		return errRefused
	}
	return nil
}
