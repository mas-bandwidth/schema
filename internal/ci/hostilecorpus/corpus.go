// Package hostilecorpus is the SHARED HOSTILE-INPUT CORPUS: one directory of
// raw inputs plus one line-oriented manifest, and the generator that rebuilds
// the manifest's verdicts from the Go engine (internal/tablewire,
// internal/tabletext). No JSON dependency: every line is tab-separated and
// every payload is a raw file, so any port can read the corpus with a line
// reader and its own decoder.
package hostilecorpus

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// The manifest lives beside the inputs and is line-oriented:
//
//	name\tcarrier\tschema\ttable\tinput\tprovenance\tverdict\treason\tdigest
//
// A '#' first line and blank lines are comments. `input` is relative to the
// manifest's own directory. `verdict` is "accept" or "refuse"; `reason` is "-"
// for an accept. `digest` is the SHA-256 of the canonical decoded value for an
// accept and "-" otherwise.
const (
	ManifestName = "manifest.txt"
	Accept       = "accept"
	Refuse       = "refuse"
	NoReason     = "-"
)

// Every reason the Go engine can name. The list is the engine's own, not a
// corpus convention: see EngineReasons.
const (
	ReasonDamaged            = "damaged"
	ReasonNewerForm          = "newer_form"
	ReasonNoVocabulary       = "no_vocabulary"
	ReasonSecondAnnouncement = "second_announcement"
	ReasonVocabularyTooLarge = "vocabulary_too_large"
	ReasonMessageFormAsFile  = "message_form_as_file"
	ReasonBatchTooLarge      = "batch_too_large"
	ReasonCountRefusal       = "count_refusal"
	ReasonEngineError        = "engine_error"
)

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

// EngineReasons is every refusal reason the engine declares, read off the
// engine's own constants. A corpus that names a reason outside this set, or
// that omits one, is a corpus whose coverage a reader cannot trust.
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

// Load reads the committed manifest without asking the engine anything.
func Load(root string) ([]Case, error) {
	dir := Dir(root)
	data, err := os.ReadFile(filepath.Join(dir, ManifestName))
	if err != nil {
		return nil, err
	}
	return Parse(data)
}

// Parse reads manifest bytes.
func Parse(data []byte) ([]Case, error) {
	var out []Case
	for i, line := range strings.Split(string(data), "\n") {
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

// Format renders a manifest deterministically.
func Format(cases []Case) []byte {
	var b strings.Builder
	b.WriteString("# schema shared hostile-input corpus — line-oriented manifest\n")
	b.WriteString("# name\tcarrier\tschema\ttable\tinput\tprovenance\tverdict\treason\tdigest\n")
	rows := append([]Case(nil), cases...)
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
	for _, c := range rows {
		fmt.Fprintf(&b, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			c.Name, c.Carrier, c.Schema, c.Table, c.Input, c.Provenance, c.Verdict, c.Reason, c.Digest)
	}
	return []byte(b.String())
}

// Dir is the corpus directory inside a repository root.
func Dir(root string) string { return filepath.Join(root, "internal", "ci", "hostilecorpus") }

// Generate rebuilds every verdict from the engine, using the committed
// manifest only for each case's identity (name, carrier, schema, table, input
// and provenance).
func Generate(root string) ([]Case, error) {
	meta, err := Load(root)
	if err != nil {
		return nil, err
	}
	return GenerateCases(root, meta)
}

// GenerateCases is Generate over supplied metadata.
func GenerateCases(root string, meta []Case) ([]Case, error) {
	models := map[string]*tabletext.Model{}
	out := make([]Case, 0, len(meta))
	for _, c := range meta {
		payload, err := os.ReadFile(filepath.Join(Dir(root), filepath.FromSlash(c.Input)))
		if err != nil {
			return nil, fmt.Errorf("%s: read input: %w", c.Name, err)
		}
		m, ok := models[c.Schema]
		if !ok {
			m, err = loadModel(root, c.Schema)
			if err != nil {
				return nil, fmt.Errorf("%s: load schema %s: %w", c.Name, c.Schema, err)
			}
			models[c.Schema] = m
		}
		verdict, reason, digest, err := engineVerdict(m, c, payload)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", c.Name, err)
		}
		c.Verdict, c.Reason, c.Digest = verdict, reason, digest
		out = append(out, c)
	}
	return out, nil
}

func loadModel(root, schema string) (*tabletext.Model, error) {
	c := compiler.New()
	paths, err := compiler.GatherPaths([]string{filepath.Join(root, filepath.FromSlash(schema))})
	if err != nil {
		return nil, err
	}
	u, err := c.Load(paths)
	if err != nil {
		return nil, err
	}
	return tabletext.NewModel(u), nil
}

// Verify regenerates the manifest from the engine and fails on any difference
// from the committed bytes. This is the drift gate.
func Verify(root string) error {
	committed, err := os.ReadFile(filepath.Join(Dir(root), ManifestName))
	if err != nil {
		return err
	}
	return VerifyBytes(root, committed)
}

// VerifyBytes is Verify over manifest bytes; the inputs still resolve against
// the corpus directory. It is what the planted-bad-pattern control uses.
func VerifyBytes(root string, manifest []byte) error {
	meta, err := Parse(manifest)
	if err != nil {
		return err
	}
	got, err := GenerateCases(root, meta)
	if err != nil {
		return err
	}
	want := Format(got)
	if !bytes.Equal(want, manifest) {
		for _, d := range diffManifests(manifest, want) {
			fmt.Fprintln(os.Stderr, d)
		}
		return fmt.Errorf("hostile corpus drift: the committed manifest differs from the engine's verdicts (regenerate with go run ./tools/hostilecorpus)")
	}
	return nil
}

func diffManifests(have, want []byte) []string {
	h, w := strings.Split(string(have), "\n"), strings.Split(string(want), "\n")
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

func engineVerdict(m *tabletext.Model, c Case, payload []byte) (verdict, reason, digest string, err error) {
	st := m.Lookup(c.Table)
	if st == nil {
		return "", "", "", fmt.Errorf("table %q is not in %s", c.Table, c.Schema)
	}
	fail := func(r string) (string, string, string, error) { return Refuse, r, NoReason, nil }
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
			return fail(refusalReason(err))
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
			return fail(refusalReason(err))
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
		err := v.AnnounceRead(payload, &r)
		if err == nil {
			return "", "", "", errors.New("second announcement was accepted")
		}
		return fail(refusalReason(err))
	case "announce_bytes":
		v := &tablewire.Vocabulary{MaxBytes: 8}
		var r tabletext.Report
		if err := v.AnnounceRead(payload, &r); err == nil {
			return "", "", "", errors.New("announcement over the byte bound was accepted")
		} else {
			return fail(refusalReason(err))
		}
	case "announce_entries":
		v := &tablewire.Vocabulary{MaxEntries: 1}
		var r tabletext.Report
		if err := v.AnnounceRead(payload, &r); err == nil {
			return "", "", "", errors.New("announcement over the entry bound was accepted")
		} else {
			return fail(refusalReason(err))
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
			return fail(refusalReason(err))
		}
		if !ok || n != 1 {
			return fail(ReasonDamaged)
		}
		return accept(inst)
	default:
		return "", "", "", fmt.Errorf("unknown carrier %q", c.Carrier)
	}
}

func refusalReason(err error) string {
	var mr *tablewire.MessageRefusal
	if errors.As(err, &mr) {
		return mr.Reason.String()
	}
	var fr *tablewire.FormRefusal
	if errors.As(err, &fr) {
		return ReasonNewerForm
	}
	var cr *tablewire.CountRefusal
	if errors.As(err, &cr) {
		return ReasonCountRefusal
	}
	return ReasonEngineError
}
