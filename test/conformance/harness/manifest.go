// The conformance manifest reader (testdata/conformance/tables/MANIFEST.txt).
//
// One line-oriented format, read here by the harness and by every language
// driver. The whole point of it is that nothing in the data names a language:
// a port implements test/conformance/README.md's contract against this file
// and the harness is the gate.
package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// Unit is a compilation unit by the key the other lines use.
type Unit struct {
	Key   string
	Paths []string
}

// Instance is one table instance in two forms that must agree: the wire bytes
// at Wire (docs/SPEC-TABLES.md §3) and the JSON text at JSON (§16).
type Instance struct {
	Name string
	Unit string
	Root string
	Wire string
	JSON string
	// NoText marks an instance the corpus carries on the WIRE alone, by the
	// text form's own rule (docs/SPEC-TABLES.md §16.7): a pointer chain nests
	// in the text as deep as it is long, and past the depth cap the writer
	// refuses it in every implementation.
	NoText bool
	// WireRefused marks an instance whose wire file is refused on the wire
	// surface in every language (e.g. shared blob identity, §2.5).
	WireRefused bool
}

// WireRefusal answers the wire refusal lang owes for this instance, or ""
// when it owes the committed bytes.
func (inst Instance) WireRefusal(lang string) string {
	if inst.WireRefused {
		return "blob_shared"
	}
	return ""
}

// ReportCase is bytes read by a type that did not write them. Value is the
// file holding the §16 text of the value the read leaves, which the row owes
// after its verdict line whenever the read accepts; a refused read has none.
type ReportCase struct {
	Name  string
	Unit  string
	Root  string
	Wire  string
	Value string
}

// FramingCase is one hand-built framing vector (docs/LAYERS/1-framing.md): the
// wire, the verdict it owes, and the file holding the §16 text of the value it
// owes when read, or "" when refused.
type FramingCase struct {
	Name    string
	Unit    string
	Root    string
	Wire    string
	Refused bool
	Value   string
}

func (fc FramingCase) Verdict() string {
	if fc.Refused {
		return "refused"
	}
	return "read"
}

// Hostile is one directory of hostile JSON (docs/SPEC-TABLES.md §16.3) that
// pack reads at Root, and the verdict that read owes: read with the value at
// Value, or refused.
type Hostile struct {
	Name    string
	Unit    string
	Root    string
	Tree    string
	Refused bool
	Value   string
}

// Verdict is the standard's verdict line for h, without a trailing newline.
func (h Hostile) Verdict() string {
	if h.Refused {
		return "refused"
	}
	return "read"
}

// Carries reports whether lang carries h's standard rule. Under the single
// read contract every port carries every rule.
func (h Hostile) Carries(lang string) bool {
	return true
}

// VerdictFor is the verdict line lang owes for h, without a trailing newline.
func (h Hostile) VerdictFor(lang string) string {
	return h.Verdict()
}

// HostileValue is where a hostile row's value golden lives: beside the tree,
// not inside it, so a directory walk of the tree never mistakes the answer for
// a second input file.
func HostileValue(tree string) string { return tree + ".value.json" }

// ReportValue is where a report row's value golden lives: in report-values/
// beside json/, which is the directory jsonDir names.
func ReportValue(jsonDir, name string) string {
	return filepath.Join(filepath.Dir(jsonDir), "report-values", name+".json")
}

// RetainValue is where a retain row's value golden lives: in retain-values/
// beside json/, which is the directory jsonDir names.
func RetainValue(jsonDir, name string) string {
	return filepath.Join(filepath.Dir(jsonDir), "retain-values", name+".json")
}

// Cook is one cooked image (docs/SPEC-TABLES.md §7) and the canonical node
// dump every reader must produce from it. File is filled in by `harness run`
// when it materialises the fixture; Dump is committed.
type Cook struct {
	Case string
	Unit string
	Root string
	Dump string
	File string
}

// CookWrite is the WRITE side of the cook (docs/SPEC-TABLES.md §7.6): an
// instance the manifest already names, and the two cooked files the tool
// produces from its construction recipe — little-endian first, big-endian
// second.
type CookWrite struct {
	Instance string
	Little   string
	Big      string
	Input    string
	Share    string
}

// Block is one block-allocated image (docs/SPEC-TABLES.md §19) and the row
// dump every reader must produce from it. Both files are committed.
type Block struct {
	Name string
	Unit string
	File string
	Dump string
}

// Patch is one little-endian word written over a base fixture to forge a
// hostile cook or block image.
type Patch struct {
	Offset int64
	Width  int
	Value  uint64
}

// Forgery is one hostile cook or block image (docs/SPEC-TABLES.md §7.5, §19.2).
type Forgery struct {
	Name    string
	Kind    string // "cook" or "block"
	Subject string // root table for cook, block name for block
	Base    string // fixture name to patch over
	Pointer int    // -1 for null, >= 0 for byte offset into an aligned buffer
	Patches []Patch
	Extent  int64
	Verdict string // "refuse" or "open"
	Label   string
	File    string // materialised by `harness run`
}

// Connection is the announced vocabulary of one endpoint (docs/SPEC-TABLES.md
// §3.3): the unit that wrote the announcement, the build version it stamps on
// the connection, and the announcement wire itself.
type Connection struct {
	Key          string
	Unit         string
	BuildVersion uint64
	Wire         string
}

// Message is one resolved-form pair (docs/SPEC-TABLES.md §3.3): the file-form
// wire at FileWire and the message-form batch at MessageWire, under the
// connection at Connection, which names the unit.
type Message struct {
	Name        string
	Connection  string
	Unit        string
	Root        string
	FileWire    string
	MessageWire string
}

// VariableMessageRefusal is the diagnostic reason for a message row whose
// root is a variable table (docs/SPEC-TABLES.md §3.3).
const VariableMessageRefusal = "variable table: the message form is fixed tables only (SPEC-TABLES §3.3)"

// Refused reports whether this row expects the variable-table refusal rather
// than a message-form golden.
func (m Message) Refused() bool { return m.MessageWire == "-" }

// RetainCase is one retaining round trip (docs/SPEC-TABLES.md §6.6): a wire
// carrying fields its reader's schema does not declare, read into a retain
// store at the capacity Short and Ids name and written back out.
type RetainCase struct {
	Name  string
	Unit  string
	Root  string
	Wire  string
	Short bool
	// Ids is the id list's entry capacity: -1 for `full`, or >= 0 for a
	// fixed entry cap that isolates the id-list bound from the byte buffer.
	Ids   int
	Saves string
	Value string
}

// RetainShortSlack is the byte margin the `short` capacity rule subtracts from
// the wire's length: `len(wire) - RetainShortSlack` fits the first unknown and
// refuses the second in every port's layout.
const RetainShortSlack = 16

type Manifest struct {
	Units       []Unit
	Instances   []Instance
	Connections []Connection
	Messages    []Message
	Reports     []ReportCase
	Framings    []FramingCase
	Hostiles    []Hostile
	Retains     []RetainCase
	Cooks       []Cook
	CookWrites  []CookWrite
	Blocks      []Block
	Forgeries   []Forgery
}

// parseReadVerdict parses either a plain verdict ("read"/"ACCEPT" or
// "refused"/"REFUSED") or a legacy 7-field report line ("u,k,w,c,d,m,verdict")
// used as transport by unmigrated drivers. In legacy transport, only m==true or
// verdict=="refused" is a refusal; nonzero repair counters (k, c, d) with
// verdict=="read" remain "read" so a recovering reader fails against a strict
// refusal expectation.
func parseReadVerdict(text string) (string, error) {
	switch strings.TrimSpace(text) {
	case "read", "ACCEPT":
		return "read", nil
	case "refused", "REFUSED":
		return "refused", nil
	}
	u, k, w, c, d, m, v, err := parseSevenFieldReport(text)
	if err != nil {
		return "", err
	}
	_, _, _, _, _ = u, k, w, c, d
	if m || v == "refused" {
		return "refused", nil
	}
	return "read", nil
}

// parseManifestVerdict parses a verdict token in a manifest file, where any
// legacy counter tuple with k!=0, c!=0, d!=0, m==true, or verdict=="refused"
// is a strict refusal.
func parseManifestVerdict(text string) (refused, malformed bool, err error) {
	switch strings.TrimSpace(text) {
	case "read":
		return false, false, nil
	case "refused":
		return true, false, nil
	case "malformed":
		return true, true, nil
	}
	_, k, _, c, d, m, v, err := parseSevenFieldReport(text)
	if err != nil {
		return false, false, err
	}
	if m {
		return true, true, nil
	}
	if v == "refused" || k != 0 || c != 0 || d != 0 {
		return true, false, nil
	}
	return false, false, nil
}

func parseSevenFieldReport(text string) (u, k, w, c, d int, m bool, verdict string, err error) {
	parts := strings.Split(strings.TrimSpace(text), ",")
	if len(parts) != 7 {
		return 0, 0, 0, 0, 0, false, "", fmt.Errorf("a report verdict is `read` or `refused`, not %q", text)
	}
	nums := []*int{&u, &k, &w, &c, &d}
	for i, p := range nums {
		val, convErr := strconv.Atoi(parts[i])
		if convErr != nil {
			return 0, 0, 0, 0, 0, false, "", fmt.Errorf("%q is not a counter: %w", parts[i], convErr)
		}
		*p = val
	}
	switch parts[5] {
	case "true":
		m = true
	case "false":
		m = false
	default:
		return 0, 0, 0, 0, 0, false, "", fmt.Errorf("%q is not true or false", parts[5])
	}
	switch parts[6] {
	case "read", "refused":
		verdict = parts[6]
	default:
		return 0, 0, 0, 0, 0, false, "", fmt.Errorf("%q is not a verdict: a read is `read` or `refused` (docs/SPEC-TABLES.md §3)", parts[6])
	}
	return u, k, w, c, d, m, verdict, nil
}

// ReadManifest parses path and fills each Instance's JSON path under jsonDir.
func ReadManifest(path, jsonDir string) (*Manifest, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	m := &Manifest{}
	lineNo := 0
	scan := bufio.NewScanner(file)
	for scan.Scan() {
		lineNo++
		line := strings.TrimSpace(scan.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Fields(line)
		where := fmt.Sprintf("%s:%d", path, lineNo)
		switch f[0] {
		case "unit":
			if len(f) < 3 {
				return nil, fmt.Errorf("%s: unit takes a key and at least one schema path", where)
			}
			m.Units = append(m.Units, Unit{Key: f[1], Paths: f[2:]})
		case "instance":
			if len(f) < 5 || len(f) > 7 {
				return nil, fmt.Errorf("%s: instance takes name, unit, root, wire path, and optional markers", where)
			}
			inst := Instance{
				Name: f[1], Unit: f[2], Root: f[3], Wire: f[4],
				JSON: filepath.Join(jsonDir, f[1]+".json"),
			}
			hasRefusedBy := false
			for _, marker := range f[5:] {
				switch {
				case marker == "no-text" && !inst.NoText:
					inst.NoText = true
					inst.JSON = ""
				case marker == "wire-refused" && !inst.WireRefused:
					inst.WireRefused = true
				case strings.HasPrefix(marker, "go-refusal=") && !inst.WireRefused:
					reason := strings.TrimPrefix(marker, "go-refusal=")
					if reason != "blob_shared" {
						return nil, fmt.Errorf("%s: unsupported Go wire refusal %q", where, reason)
					}
					inst.WireRefused = true
				case strings.HasPrefix(marker, "refused-by=") && !hasRefusedBy:
					ports := strings.Split(strings.TrimPrefix(marker, "refused-by="), ",")
					if slices.Contains(ports, "") {
						return nil, fmt.Errorf("%s: refused-by names an empty port", where)
					}
					hasRefusedBy = true
				default:
					return nil, fmt.Errorf("%s: invalid instance marker %q", where, marker)
				}
			}
			if hasRefusedBy && !inst.WireRefused {
				return nil, fmt.Errorf("%s: refused-by requires wire-refused", where)
			}
			m.Instances = append(m.Instances, inst)
		case "connection":
			if len(f) != 5 {
				return nil, fmt.Errorf("%s: connection takes key, unit, build version, wire", where)
			}
			bv, err := strconv.ParseUint(f[3], 0, 64)
			if err != nil {
				return nil, fmt.Errorf("%s: %q is not a build version: %w", where, f[3], err)
			}
			m.Connections = append(m.Connections, Connection{
				Key: f[1], Unit: f[2], BuildVersion: bv, Wire: f[4],
			})
		case "message":
			if len(f) != 6 {
				return nil, fmt.Errorf("%s: message takes name, connection, root, file wire, message wire", where)
			}
			conn, err := m.LookupConnection(f[2])
			if err != nil {
				return nil, fmt.Errorf("%s: %w", where, err)
			}
			m.Messages = append(m.Messages, Message{
				Name: f[1], Connection: f[2], Unit: conn.Unit,
				Root: f[3], FileWire: f[4], MessageWire: f[5],
			})
		case "retain":
			// retain <case> <unit> <root> <wire> <capacity> <ids> <save|->
			if len(f) != 8 && len(f) != 10 {
				return nil, fmt.Errorf("%s: retain takes case, unit, root, wire, capacity, ids, save wire or -", where)
			}
			var short bool
			switch f[5] {
			case "short":
				short = true
			case "full":
				short = false
			default:
				return nil, fmt.Errorf("%s: %q is not `short` or `full`", where, f[5])
			}
			ids := -1
			if f[6] != "full" {
				n, err := strconv.Atoi(f[6])
				if err != nil || n < 0 {
					return nil, fmt.Errorf("%s: %q is not `full` or a non-negative id count", where, f[6])
				}
				ids = n
			}
			saveField := f[7]
			if len(f) == 10 {
				if parts := strings.Split(f[7], ","); len(parts) != 3 {
					return nil, fmt.Errorf("%s: invalid legacy retain counts %q", where, f[7])
				}
				if n, err := strconv.Atoi(f[9]); err != nil || n < 0 {
					return nil, fmt.Errorf("%s: %q is not a save-lost count", where, f[9])
				}
				saveField = f[8]
			}
			saves := ""
			if saveField != "-" {
				saves = saveField
			}
			m.Retains = append(m.Retains, RetainCase{
				Name: f[1], Unit: f[2], Root: f[3], Wire: f[4],
				Short: short, Ids: ids, Saves: saves,
				Value: RetainValue(jsonDir, f[1]),
			})
		case "report":
			if len(f) != 5 {
				return nil, fmt.Errorf("%s: report takes case, unit, root, wire", where)
			}
			m.Reports = append(m.Reports, ReportCase{
				Name: f[1], Unit: f[2], Root: f[3], Wire: f[4],
				Value: ReportValue(jsonDir, f[1]),
			})
		case "framing":
			// framing <case> <unit> <root> <wire> <read|refused> <value|->
			if len(f) != 7 {
				return nil, fmt.Errorf("%s: framing takes case, unit, root, wire, verdict, value file or -", where)
			}
			fc := FramingCase{Name: f[1], Unit: f[2], Root: f[3], Wire: f[4]}
			switch f[5] {
			case "refused", "malformed":
				fc.Refused = true
			case "read":
				fc.Refused = false
			default:
				refused, malformed, err := parseManifestVerdict(f[5])
				if err != nil {
					return nil, fmt.Errorf("%s: %w", where, err)
				}
				if malformed {
					return nil, fmt.Errorf("%s: a damaged read owes the verdict `refused`, not counters", where)
				}
				fc.Refused = refused
			}
			if f[6] != "-" {
				if fc.Refused {
					return nil, fmt.Errorf("%s: a refused or damaged read has no value to owe", where)
				}
				fc.Value = f[6]
			}
			m.Framings = append(m.Framings, fc)
		case "json-hostile":
			if len(f) != 6 && len(f) != 8 {
				return nil, fmt.Errorf("%s: json-hostile takes case, unit, root, tree, verdict", where)
			}
			h := Hostile{Name: f[1], Unit: f[2], Root: f[3], Tree: f[4]}
			refused, _, err := parseManifestVerdict(f[5])
			if err != nil {
				return nil, fmt.Errorf("%s: %w", where, err)
			}
			h.Refused = refused
			if !h.Refused {
				h.Value = HostileValue(f[4])
			}
			if len(f) == 8 {
				carried, ok := strings.CutPrefix(f[6], "carried-by=")
				if !ok || carried == "" || slices.Contains(strings.Split(carried, ","), "") {
					return nil, fmt.Errorf("%s: %q is not carried-by=<ports>", where, f[6])
				}
				uncarried, ok := strings.CutPrefix(f[7], "uncarried=")
				if !ok || uncarried == "" {
					return nil, fmt.Errorf("%s: %q is not uncarried=<verdict>", where, f[7])
				}
				if _, _, err := parseManifestVerdict(uncarried); err != nil {
					return nil, fmt.Errorf("%s: uncarried: %w", where, err)
				}
			}
			m.Hostiles = append(m.Hostiles, h)
		case "cook":
			if len(f) != 5 {
				return nil, fmt.Errorf("%s: cook takes case, unit, root, dump", where)
			}
			m.Cooks = append(m.Cooks, Cook{Case: f[1], Unit: f[2], Root: f[3], Dump: f[4]})
		case "cook-write":
			if len(f) < 4 || len(f) > 6 {
				return nil, fmt.Errorf("%s: cook-write takes an instance and its two cooked files, little then big", where)
			}
			cw := CookWrite{Instance: f[1], Little: f[2], Big: f[3]}
			for _, option := range f[4:] {
				switch {
				case strings.HasPrefix(option, "input=") && cw.Input == "":
					cw.Input = strings.TrimPrefix(option, "input=")
				case strings.HasPrefix(option, "share=") && cw.Share == "":
					cw.Share = strings.TrimPrefix(option, "share=")
				default:
					return nil, fmt.Errorf("%s: invalid cook construction option %q", where, option)
				}
			}
			if cw.Share != "" {
				source, alias, ok := strings.Cut(cw.Share, ":")
				if !ok || source == "" || alias == "" || source == alias || strings.Contains(alias, ":") || cw.Input == "" {
					return nil, fmt.Errorf("%s: share requires input= and two distinct blob fields", where)
				}
			}
			m.CookWrites = append(m.CookWrites, cw)
		case "block":
			if len(f) != 5 {
				return nil, fmt.Errorf("%s: block takes name, unit, file, dump", where)
			}
			m.Blocks = append(m.Blocks, Block{Name: f[1], Unit: f[2], File: f[3], Dump: f[4]})
		case "forgery":
			if len(f) < 12 {
				return nil, fmt.Errorf("%s: forgery takes name, kind, subject, base, pointer, offset, width, value, extent, verdict, label", where)
			}
			pointer := 0
			if f[5] == "null" {
				pointer = -1
			} else {
				n, err := strconv.Atoi(f[5])
				if err != nil || n < 0 {
					return nil, fmt.Errorf("%s: %q is not a lead, and is not null", where, f[5])
				}
				pointer = n
			}
			patches, err := parsePatches(f[6], f[7], f[8])
			if err != nil {
				return nil, fmt.Errorf("%s: %w", where, err)
			}
			extent, err := strconv.ParseInt(f[9], 0, 64)
			if err != nil {
				return nil, fmt.Errorf("%s: %q is not an extent", where, f[9])
			}
			if f[10] != "refuse" && f[10] != "open" {
				return nil, fmt.Errorf("%s: %q is not a verdict", where, f[10])
			}
			m.Forgeries = append(m.Forgeries, Forgery{
				Name: f[1], Kind: f[2], Subject: f[3], Base: f[4],
				Pointer: pointer, Patches: patches, Extent: extent,
				Verdict: f[10], Label: strings.Join(f[11:], " "),
			})
		default:
			return nil, fmt.Errorf("%s: %q is not a manifest line kind", where, f[0])
		}
	}
	if err := scan.Err(); err != nil {
		return nil, err
	}
	for _, cw := range m.CookWrites {
		if _, err := m.LookupInstance(cw.Instance); err != nil {
			return nil, fmt.Errorf("%s: cook-write %s: %w", path, cw.Instance, err)
		}
	}
	return m, nil
}

// parsePatches zips three comma-separated columns of equal length into a
// slice of patches.
func parsePatches(offsets, widths, values string) ([]Patch, error) {
	os := strings.Split(offsets, ",")
	ws := strings.Split(widths, ",")
	vs := strings.Split(values, ",")
	if len(os) != len(ws) || len(os) != len(vs) {
		return nil, fmt.Errorf("patch columns have %d offsets, %d widths, %d values", len(os), len(ws), len(vs))
	}
	var out []Patch
	for i := range os {
		off, err := strconv.ParseInt(os[i], 0, 64)
		if err != nil {
			return nil, fmt.Errorf("%q is not an offset", os[i])
		}
		w, err := strconv.Atoi(ws[i])
		if err != nil {
			return nil, fmt.Errorf("%q is not a patch width", ws[i])
		}
		v, err := strconv.ParseUint(vs[i], 0, 64)
		if err != nil {
			return nil, fmt.Errorf("%q is not a patch value", vs[i])
		}
		if off < 0 {
			continue
		}
		if w < 1 || w > 8 {
			return nil, fmt.Errorf("%q is not a patch width", ws[i])
		}
		out = append(out, Patch{Offset: off, Width: w, Value: v})
	}
	return out, nil
}

func (m *Manifest) LookupUnit(key string) (Unit, error) {
	for _, u := range m.Units {
		if u.Key == key {
			return u, nil
		}
	}
	return Unit{}, fmt.Errorf("manifest names no unit %q", key)
}

func (m *Manifest) LookupInstance(name string) (Instance, error) {
	for _, i := range m.Instances {
		if i.Name == name {
			return i, nil
		}
	}
	return Instance{}, fmt.Errorf("manifest names no instance %q", name)
}

func (m *Manifest) LookupConnection(key string) (Connection, error) {
	for _, c := range m.Connections {
		if c.Key == key {
			return c, nil
		}
	}
	return Connection{}, fmt.Errorf("manifest names no connection %q", key)
}

func (m *Manifest) UnitPaths(key string) ([]string, error) {
	u, err := m.LookupUnit(key)
	if err != nil {
		return nil, err
	}
	return u.Paths, nil
}
