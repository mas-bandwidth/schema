// cards-driver is the Go reference leg of the card conformance set
// (tables/cards/conformance/FORMAT.md). It answers the driver contract
// test/conformance/README.md states — `<driver> <manifest> list` and
// `<driver> <manifest> <surface> <outdir>` — over the nine card surfaces,
// through the generated cards module and the facade in tables/cards (Load,
// ToJSON, FromJSON, Render, Parse, Upgrade, the Set* edits, TaskHash and the
// generated view, docs/SPEC-TABLES.md §8). It is the concrete side of
// tla/CardHops.tla: every language's driver must answer the same bytes, and
// this one writes the expected answers the check compares against.
package main

import (
	"bufio"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"cards"
)

// surfaces is the nine card surfaces, in the order a leg lists and answers
// them.
var surfaces = []string{"load", "save", "render", "parse", "bounds", "upgrade", "edit", "taskhash", "view"}

// aCase is one manifest line.
type aCase struct {
	surface string
	name    string
	input   string
	edit    []string
}

func main() {
	if len(os.Args) < 3 {
		fatalf("usage: cards-driver <manifest> (list | <surface> <outdir>)")
	}
	manifest, verb := os.Args[1], os.Args[2]
	if verb == "list" {
		for _, s := range surfaces {
			fmt.Println(s)
		}
		return
	}
	if len(os.Args) < 4 {
		fatalf("usage: cards-driver <manifest> <surface> <outdir>")
	}
	surface, outdir := verb, os.Args[3]
	cases, err := readManifest(manifest)
	if err != nil {
		fatalf("%v", err)
	}
	if err := os.MkdirAll(outdir, 0o755); err != nil {
		fatalf("%v", err)
	}
	if err := answer(surface, cases, outdir); err != nil {
		fatalf("%v", err)
	}
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "cards-driver: "+format+"\n", args...)
	os.Exit(1)
}

// readManifest reads the committed manifest: one case per line as
// "<surface> <name> <input> [edit...]", skipping comments and blanks.
func readManifest(path string) ([]aCase, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []aCase
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Fields(line)
		if len(f) < 3 {
			return nil, fmt.Errorf("malformed manifest line %q", line)
		}
		out = append(out, aCase{surface: f[0], name: f[1], input: f[2], edit: f[3:]})
	}
	return out, sc.Err()
}

// answer runs one surface over every case of that surface in the manifest,
// writing one file per case into outdir.
func answer(surface string, cases []aCase, outdir string) error {
	for _, c := range cases {
		if c.surface != surface {
			continue
		}
		if err := one(surface, c, outdir); err != nil {
			return err
		}
	}
	return nil
}

// writeFile writes data to outdir/<name>.<ext>, creating the directory.
func writeFile(outdir, name, ext string, data []byte) error {
	return os.WriteFile(filepath.Join(outdir, name+"."+ext), data, 0o644)
}

// refuse writes outdir/<name>.refused holding the one-line reason class.
func refuse(outdir, name, class string) error {
	return writeFile(outdir, name, "refused", []byte(class+"\n"))
}

func one(surface string, c aCase, outdir string) error {
	switch surface {
	case "load":
		wire, err := os.ReadFile(c.input)
		if err != nil {
			return err
		}
		rec, report, err := cards.Load(wire)
		if err != nil {
			return err
		}
		json, _, err := cards.ToJSON(rec)
		if err != nil {
			return err
		}
		if err := writeFile(outdir, c.name, "json", json); err != nil {
			return err
		}
		return writeReport(outdir, c.name, report)
	case "save", "bounds":
		text, err := os.ReadFile(c.input)
		if err != nil {
			return err
		}
		rec, err := cards.FromJSON(text)
		if err != nil {
			return refuse(outdir, c.name, refusalClass(err))
		}
		return writeFile(outdir, c.name, "wire", rec.Wire())
	case "render":
		wire, err := os.ReadFile(c.input)
		if err != nil {
			return err
		}
		rec, _, err := cards.Load(wire)
		if err != nil {
			return err
		}
		text, _, err := cards.Render(rec)
		if err != nil {
			return err
		}
		return writeFile(outdir, c.name, "txt", text)
	case "parse":
		text, err := os.ReadFile(c.input)
		if err != nil {
			return err
		}
		rec, err := cards.Parse(text)
		if err != nil {
			return refuse(outdir, c.name, refusalClass(err))
		}
		return writeFile(outdir, c.name, "wire", rec.Wire())
	case "upgrade":
		wire, err := os.ReadFile(c.input)
		if err != nil {
			return err
		}
		out, report, err := cards.Upgrade(wire)
		if err != nil {
			return refuse(outdir, c.name, refusalClass(err))
		}
		if err := writeFile(outdir, c.name, "wire", out); err != nil {
			return err
		}
		return writeReport(outdir, c.name, report)
	case "edit":
		wire, err := os.ReadFile(c.input)
		if err != nil {
			return err
		}
		rec, _, err := cards.Load(wire)
		if err != nil {
			return err
		}
		if err := applyEdit(rec, c.edit); err != nil {
			return refuse(outdir, c.name, refusalClass(err))
		}
		return writeFile(outdir, c.name, "wire", rec.Wire())
	case "taskhash":
		wire, err := os.ReadFile(c.input)
		if err != nil {
			return err
		}
		rec, _, err := cards.Load(wire)
		if err != nil {
			return err
		}
		hash, err := cards.TaskHash(rec)
		if err != nil {
			return err
		}
		return writeFile(outdir, c.name, "hex", []byte(hex.EncodeToString(hash[:])))
	case "view":
		return writeFile(outdir, c.name, "txt", view())
	}
	return fmt.Errorf("unknown surface %q", surface)
}

// writeReport writes the read report, one Name=value line per counter of the
// Go TableReport in declaration order (tables/cards/conformance/FORMAT.md).
func writeReport(outdir, name string, r cards.Report) error {
	var b strings.Builder
	fmt.Fprintf(&b, "Retained=%d\n", r.Retained)
	fmt.Fprintf(&b, "RetainLost=%d\n", r.RetainLost)
	fmt.Fprintf(&b, "Widened=%d\n", r.Widened)
	fmt.Fprintf(&b, "Unknown=%d\n", r.Unknown)
	fmt.Fprintf(&b, "KindMismatch=%d\n", r.KindMismatch)
	fmt.Fprintf(&b, "Clamped=%d\n", r.Clamped)
	fmt.Fprintf(&b, "Duplicate=%d\n", r.Duplicate)
	fmt.Fprintf(&b, "Malformed=%t\n", r.Malformed)
	return writeFile(outdir, name, "report", []byte(b.String()))
}

// view renders the generated view: every field of every table in the unit, in
// declaration order, one line each (docs/SPEC-TABLES.md §8; FORMAT.md "The
// view").
func view() []byte {
	uv := cards.UnitView()
	var b strings.Builder
	for _, t := range uv.Tables {
		for _, f := range t.Type.Fields {
			shape := "value"
			if f.List {
				shape = "list"
			} else if f.Pointer {
				shape = "blob"
			}
			bound := "-"
			if !f.List && !f.Pointer && f.ArrayBound > 0 {
				bound = strconv.Itoa(int(f.ArrayBound))
			}
			fmt.Fprintf(&b, "%s %s %s %s %s\n", t.Name, f.Name, f.TypeName, shape, bound)
		}
	}
	return []byte(b.String())
}

// applyEdit applies one edit line — "needs=a,b", "tier=heavy",
// "priority=blocker", "paths=x,y", "column=review", "holder=h" — through the
// Set* edit of the same name (tables/cards/edit.go).
func applyEdit(rec *cards.Record, edit []string) error {
	if len(edit) != 1 {
		return errors.New("cards-driver: an edit line carries exactly one edit")
	}
	key, value, ok := strings.Cut(edit[0], "=")
	if !ok {
		return fmt.Errorf("cards-driver: edit %q has no =", edit[0])
	}
	switch key {
	case "needs":
		return cards.SetNeeds(rec, splitList(value))
	case "paths":
		return cards.SetPaths(rec, splitList(value))
	case "tier":
		tier, err := parseTier(value)
		if err != nil {
			return err
		}
		return cards.SetTier(rec, tier)
	case "priority":
		pr, err := parsePriority(value)
		if err != nil {
			return err
		}
		return cards.SetPriority(rec, pr)
	case "column":
		col, err := parseColumn(value)
		if err != nil {
			return err
		}
		return cards.SetColumn(rec, col)
	case "holder":
		return cards.SetHolder(rec, value)
	}
	return fmt.Errorf("cards-driver: unknown edit %q", key)
}

func splitList(v string) []string {
	if v == "" {
		return nil
	}
	return strings.Split(v, ",")
}

func parseTier(s string) (cards.Tier, error) {
	switch s {
	case "flash":
		return cards.Tierflash, nil
	case "pro":
		return cards.Tierpro, nil
	case "heavy":
		return cards.Tierheavy, nil
	case "frontier":
		return cards.Tierfrontier, nil
	}
	return cards.TierNone, fmt.Errorf("cards-driver: unknown tier %q", s)
}

func parsePriority(s string) (cards.Priority, error) {
	switch s {
	case "normal":
		return cards.Prioritynormal, nil
	case "high":
		return cards.Priorityhigh, nil
	case "critical":
		return cards.Prioritycritical, nil
	case "blocker":
		return cards.Priorityblocker, nil
	}
	return cards.PriorityNone, fmt.Errorf("cards-driver: unknown priority %q", s)
}

func parseColumn(s string) (cards.Column, error) {
	switch s {
	case "waiting":
		return cards.Columnwaiting, nil
	case "ready":
		return cards.Columnready, nil
	case "working":
		return cards.Columnworking, nil
	case "review":
		return cards.Columnreview, nil
	case "merging":
		return cards.Columnmerging, nil
	case "landed":
		return cards.Columnlanded, nil
	case "plan":
		return cards.Columnplan, nil
	case "check":
		return cards.Columncheck, nil
	}
	return cards.Columnwaiting, fmt.Errorf("cards-driver: unknown column %q", s)
}

// refusalClass maps a facade error to one class of the closed list FORMAT.md
// fixes, never free text, so every language can spell the same refusal.
func refusalClass(err error) string {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "past the 65536-byte ceiling"):
		return "body-bound 65536"
	case strings.Contains(msg, "past the 512-entry ceiling"):
		return "list-bound " + fieldName(msg) + " 512"
	case strings.Contains(msg, "stranded header key"):
		return "stranded-key " + quotedWord(msg)
	case strings.Contains(msg, "retained field(s) lost"):
		return "retain-lost"
	case strings.Contains(msg, "record has loss events"):
		return "unknown-present"
	default:
		return "malformed"
	}
}

// fieldName reads the collection name from a checkCard ceiling error, whose
// text is "cards: <field> has N entries, past the 512-entry ceiling ...".
func fieldName(msg string) string {
	rest := strings.TrimPrefix(msg, "cards: ")
	return strings.SplitN(rest, " has ", 2)[0]
}

// quotedWord reads the first double-quoted token of an error, the stranded
// header key a parse refusal names.
func quotedWord(msg string) string {
	i := strings.IndexByte(msg, '"')
	if i < 0 {
		return ""
	}
	j := strings.IndexByte(msg[i+1:], '"')
	if j < 0 {
		return ""
	}
	return msg[i+1 : i+1+j]
}
