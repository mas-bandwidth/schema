// Package hostilecorpus is the SHARED HOSTILE-INPUT CORPUS
// (schema-shared-hostile-corpus): the portable directory of inputs plus one
// line-oriented manifest whose verdicts are the Go engine's (internal/tablewire,
// internal/tabletext), never a generated reader's.
//
// The corpus is committed at internal/ci/hostilecorpus/corpus: one raw file per
// input under inputs/ and manifest.tsv beside it. The manifest needs no JSON
// dependency; a port reads it with a line reader. This package carries the
// engine verdict, the generator that rebuilds the manifest, the drift check,
// and the source inventory. The Go-port runner in tools/hostilecorpus is the
// worked example every per-port card follows.
package hostilecorpus

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

const (
	Accept   = "accept"
	Refuse   = "refuse"
	NoReason = "-"

	ReasonDamaged            = "damaged"
	ReasonNewerForm          = "newer_form"
	ReasonNoVocabulary       = "no_vocabulary"
	ReasonSecondAnnouncement = "second_announcement"
	ReasonVocabularyTooLarge = "vocabulary_too_large"
	ReasonMessageFormAsFile  = "message_form_as_file"
	ReasonBatchTooLarge      = "batch_too_large"
	ReasonCountRefusal       = "count_refusal"

	// ProbeSchemaPath is the manifest's name for the one schema that is not in
	// the checkout: a variable root whose collection count can pass the int32
	// storage cap and name internal/tablewire's CountRefusal.
	ProbeSchemaPath = "schemas/probe.schema"

	// ManifestName is the manifest file inside the corpus directory.
	ManifestName = "manifest.tsv"
)

// ProbeSchema is the variable root used only by the CountRefusal case.
const ProbeSchema = "// probe.schema — a variable root whose one collection is what the engine's\n// builder-path CountRefusal can name (internal/tablewire/decodenodes.go): a\n// count past the int32 storage cap refuses the partial value before an entry\n// is read. It exists only so the hostile corpus can carry that verdict.\npackage probe\n\ntable Root\n{\n    entries map[int32]int32\n}\n"

// Case is one manifest line.
type Case struct {
	Name       string
	Carrier    string
	Schema     string
	Table      string
	Input      string
	Provenance string
	Verdict    string
	Reason     string
	Digest     string
}

// Parse reads manifest text without asking the engine anything.
func Parse(text string) ([]Case, error) {
	var out []Case
	for i, line := range strings.Split(text, "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 9 {
			return nil, fmt.Errorf("manifest line %d: %d fields, want 9", i+1, len(f))
		}
		out = append(out, Case{
			Name: f[0], Carrier: f[1], Schema: f[2], Table: f[3], Input: f[4],
			Provenance: f[5], Verdict: f[6], Reason: f[7], Digest: f[8],
		})
	}
	return out, nil
}

// Format renders a manifest deterministically, in the exact committed shape
// (header comments, then rows sorted by name).
func Format(cases []Case) string {
	var b strings.Builder
	b.WriteString("# schema shared hostile-input corpus — line-oriented manifest\n")
	b.WriteString("# name\tcarrier\tschema\ttable\tinput\tprovenance\tverdict\treason\tdigest\n")
	rows := append([]Case(nil), cases...)
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
	for _, c := range rows {
		fmt.Fprintf(&b, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			c.Name, c.Carrier, c.Schema, c.Table, c.Input, c.Provenance, c.Verdict, c.Reason, c.Digest)
	}
	return b.String()
}

// LoadManifest reads the committed manifest.
func LoadManifest(dir string) (string, []Case, error) {
	data, err := os.ReadFile(filepath.Join(dir, ManifestName))
	if err != nil {
		return "", nil, err
	}
	cases, err := Parse(string(data))
	if err != nil {
		return "", nil, err
	}
	return string(data), cases, nil
}

// WriteManifest writes the committed manifest.
func WriteManifest(dir, manifest string) error {
	return os.WriteFile(filepath.Join(dir, ManifestName), []byte(manifest), 0o644)
}

// EngineReasons is every refusal reason the engine can name, read off
// internal/tablewire's own constants. A corpus that names a reason outside this
// set, or omits one, is a corpus whose coverage a reader cannot trust.
func EngineReasons() []string {
	return []string{
		tablewire.ReasonNewerForm.String(),
		tablewire.ReasonNoVocabulary.String(),
		tablewire.ReasonSecondAnnouncement.String(),
		tablewire.ReasonVocabularyTooLarge.String(),
		tablewire.ReasonMessageFormAsFile.String(),
		tablewire.ReasonBatchTooLarge.String(),
		ReasonCountRefusal,
		ReasonDamaged,
	}
}

// Payload reads one input from the corpus directory.
func Payload(dir, input string) ([]byte, error) {
	rel := strings.TrimPrefix(input, "inputs/")
	if rel == input {
		return nil, fmt.Errorf("input %q is not under inputs/", input)
	}
	payload, err := os.ReadFile(filepath.Join(dir, "inputs", filepath.FromSlash(rel)))
	if err != nil {
		return nil, fmt.Errorf("input %q: %w", input, err)
	}
	return payload, nil
}

// LoadModel loads one schema. The probe schema is written to the temporary
// directory; every other schema is the checkout's own.
func LoadModel(root, tmp, schema string) (*tabletext.Model, error) {
	path := filepath.Join(root, filepath.FromSlash(schema))
	if schema == ProbeSchemaPath {
		path = filepath.Join(tmp, "probe.schema")
		if err := os.WriteFile(path, []byte(ProbeSchema), 0o644); err != nil {
			return nil, err
		}
	}
	paths, err := compiler.GatherPaths([]string{path})
	if err != nil {
		return nil, err
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		return nil, err
	}
	return tabletext.NewModel(u), nil
}

// Generate rebuilds every verdict from the engine, using the supplied metadata
// only for each case's identity.
func Generate(root, tmp, corpusDir string, meta []Case) ([]Case, error) {
	models := map[string]*tabletext.Model{}
	out := make([]Case, 0, len(meta))
	for _, c := range meta {
		payload, err := Payload(corpusDir, c.Input)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", c.Name, err)
		}
		m, ok := models[c.Schema]
		if !ok {
			m, err = LoadModel(root, tmp, c.Schema)
			if err != nil {
				return nil, fmt.Errorf("%s: load schema %s: %w", c.Name, c.Schema, err)
			}
			models[c.Schema] = m
		}
		verdict, reason, digest, err := EngineVerdict(m, c, payload)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", c.Name, err)
		}
		c.Verdict, c.Reason, c.Digest = verdict, reason, digest
		out = append(out, c)
	}
	return out, nil
}

// Verify regenerates the manifest from the engine and fails on any difference
// from the committed bytes. This is the drift gate.
func Verify(root, tmp, corpusDir, manifest string) error {
	meta, err := Parse(manifest)
	if err != nil {
		return err
	}
	got, err := Generate(root, tmp, corpusDir, meta)
	if err != nil {
		return err
	}
	want := Format(got)
	if want == manifest {
		return nil
	}
	for _, d := range Diff(manifest, want) {
		fmt.Fprintln(os.Stderr, d)
	}
	return errors.New("hostile corpus drift: the committed manifest differs from the engine's verdicts (regenerate with make schema-shared-hostile-corpus-generate)")
}

// Diff reports the differing manifest lines.
func Diff(have, want string) []string {
	h, w := strings.Split(have, "\n"), strings.Split(want, "\n")
	var out []string
	for i := 0; i < len(h) || i < len(w); i++ {
		var a, b string
		if i < len(h) {
			a = h[i]
		}
		if i < len(w) {
			b = w[i]
		}
		if a != b {
			out = append(out, fmt.Sprintf("manifest line %d:\n  committed: %s\n  engine:    %s", i+1, a, b))
		}
	}
	return out
}

// EngineVerdict runs one case through the Go engine.
func EngineVerdict(m *tabletext.Model, c Case, payload []byte) (verdict, reason, digest string, err error) {
	st := m.Lookup(c.Table)
	if st == nil {
		return "", "", "", fmt.Errorf("table %q is not in %s", c.Table, c.Schema)
	}
	fail := func(r string) (string, string, string, error) {
		return Refuse, r, NoReason, nil
	}
	accept := func(inst *tabletext.Instance) (string, string, string, error) {
		text, err := m.Write(inst)
		if err != nil {
			return "", "", "", fmt.Errorf("digest: %w", err)
		}
		sum := sha256.Sum256(text)
		return Accept, NoReason, hex.EncodeToString(sum[:]), nil
	}
	switch c.Carrier {
	case "wire":
		inst := m.New(st)
		var r tabletext.Report
		ok, err := tablewire.Decode(m, inst, payload, &r)
		if err != nil {
			rreason, rerr := RefusalReason(err)
			if rerr != nil {
				return "", "", "", rerr
			}
			return fail(rreason)
		}
		if !ok {
			return fail(ReasonDamaged)
		}
		return accept(inst)
	case "text":
		inst := m.New(st)
		var r tabletext.Report
		if !m.Read(inst, payload, &r) {
			return fail(ReasonDamaged)
		}
		return accept(inst)
	case "announce":
		v := &tablewire.Vocabulary{}
		var r tabletext.Report
		if err := v.AnnounceRead(payload, &r); err != nil {
			rreason, rerr := RefusalReason(err)
			if rerr != nil {
				return "", "", "", rerr
			}
			return fail(rreason)
		}
		if r.Malformed {
			return fail(ReasonDamaged)
		}
		return Accept, NoReason, NoReason, nil
	case "announce_second":
		v := &tablewire.Vocabulary{}
		var r tabletext.Report
		if err := v.AnnounceRead(payload, &r); err != nil {
			return "", "", "", fmt.Errorf("first announcement: %w", err)
		}
		if err := v.AnnounceRead(payload, &r); err == nil {
			return "", "", "", errors.New("second announcement was accepted")
		} else {
			rreason, rerr := RefusalReason(err)
			if rerr != nil {
				return "", "", "", rerr
			}
			return fail(rreason)
		}
	case "announce_bytes":
		v := &tablewire.Vocabulary{MaxBytes: 8}
		var r tabletext.Report
		if err := v.AnnounceRead(payload, &r); err == nil {
			return "", "", "", errors.New("announcement over the byte bound was accepted")
		} else {
			rreason, rerr := RefusalReason(err)
			if rerr != nil {
				return "", "", "", rerr
			}
			return fail(rreason)
		}
	case "announce_entries":
		v := &tablewire.Vocabulary{MaxEntries: 1}
		var r tabletext.Report
		if err := v.AnnounceRead(payload, &r); err == nil {
			return "", "", "", errors.New("announcement over the entry bound was accepted")
		} else {
			rreason, rerr := RefusalReason(err)
			if rerr != nil {
				return "", "", "", rerr
			}
			return fail(rreason)
		}
	case "message", "message_novocab":
		var v *tablewire.Vocabulary
		if c.Carrier == "message" {
			v = &tablewire.Vocabulary{}
			var ar tabletext.Report
			if err := v.AnnounceRead(tablewire.Announce(m.Unit), &ar); err != nil {
				return "", "", "", fmt.Errorf("announce: %w", err)
			}
		}
		inst := m.New(st)
		var r tabletext.Report
		n, ok, err := tablewire.DecodeMessages(m, []*tabletext.Instance{inst}, payload, v, &r)
		if err != nil {
			rreason, rerr := RefusalReason(err)
			if rerr != nil {
				return "", "", "", rerr
			}
			return fail(rreason)
		}
		if !ok || n != 1 {
			return fail(ReasonDamaged)
		}
		return accept(inst)
	default:
		return "", "", "", fmt.Errorf("unknown carrier %q", c.Carrier)
	}
}

// RefusalReason maps an engine error to its named reason. An error that is none
// of the engine's refusal types is a failure, not a synthetic reason: collapsing
// it into a made-up engine_error would hide an engine change behind a green gate.
func RefusalReason(err error) (string, error) {
	var mr *tablewire.MessageRefusal
	if errors.As(err, &mr) {
		return mr.Reason.String(), nil
	}
	var fr *tablewire.FormRefusal
	if errors.As(err, &fr) {
		return ReasonNewerForm, nil
	}
	var cr *tablewire.CountRefusal
	if errors.As(err, &cr) {
		return ReasonCountRefusal, nil
	}
	return "", fmt.Errorf("engine returned an unnamed refusal: %w", err)
}

// SourceDirs is the closed set of seed/vector trees the corpus claims to carry
// whole: every file under each is part of the corpus, and none may be absent
// from the manifest.
var SourceDirs = []string{
	"internal/tabletext/testdata/fuzz/FuzzReadText/",
	"testdata/conformance/tables/json-hostile/",
}

// ExcludedSourceDirs are tracked fuzz seeds that are NOT table-wire inputs:
// they exercise the compiler (format idempotence, generated code compiling, the
// two-file pipeline), so their verdict is a compiler build result, not a
// tablewire/tabletext verdict. They are named here so the exclusion is exact
// and auditable rather than a silent gap; see docs/HOSTILE-CORPUS.md.
var ExcludedSourceDirs = []string{
	"internal/fuzz/testdata/fuzz/",
}

// Uncovered returns every leaf file under the given source trees that no case's
// provenance cites. Each dir is walked recursively, so vector files nested in
// case subdirectories are required to be cited too.
func Uncovered(root string, dirs []string, cases []Case) []string {
	caught := map[string]bool{}
	for _, c := range cases {
		for _, prefix := range dirs {
			if i := strings.Index(c.Provenance, prefix); i >= 0 {
				if rest := strings.Fields(c.Provenance[i+len(prefix):]); len(rest) > 0 {
					caught[prefix+rest[0]] = true
				}
			}
		}
	}
	var uncovered []string
	for _, prefix := range dirs {
		dir := filepath.Join(root, filepath.FromSlash(prefix))
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			src := filepath.ToSlash(rel)
			if !caught[src] {
				uncovered = append(uncovered, src)
			}
			return nil
		})
		if err != nil {
			uncovered = append(uncovered, fmt.Sprintf("walk %s: %v", prefix, err))
		}
	}
	return uncovered
}

// ExcludedFiles returns every leaf file under the excluded source trees. A test
// requires each to exist and to be absent from the manifest, so the exclusion
// stays a documented decision rather than a silent omission.
func ExcludedFiles(root string) ([]string, error) {
	var out []string
	for _, prefix := range ExcludedSourceDirs {
		dir := filepath.Join(root, filepath.FromSlash(prefix))
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			out = append(out, filepath.ToSlash(rel))
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Strings(out)
	return out, nil
}
