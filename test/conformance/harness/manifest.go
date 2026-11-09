// The conformance manifest reader (testdata/conformance/tables/MANIFEST.txt).
//
// One line-oriented format, read here by the harness and by every language
// driver. The whole point of it is that nothing in the data names a language:
// a port implements test/conformance/README.md's contract against this file
// and the harness is the gate.
package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
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
	// Save is the canonical write-back wire of the value the read leaves.
	// An accepted row owes it; a refused row has none.
	Save string
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
	// Save is the canonical write-back wire of the value an accepted row
	// leaves. A refused row owes no value and no wire.
	Save string
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
	// Save is the canonical write-back wire of the value an accepted row
	// leaves. A refused row owes no value and no wire.
	Save string
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

// SaveWire is where a readable report, framing or json-hostile row's canonical
// write-back wire lives: in save-wire/ beside json/. It is the bytes the value
// golden encodes to, so the read is held to the whole value in both forms.
func SaveWire(jsonDir, name string) string {
	return filepath.Join(filepath.Dir(jsonDir), "save-wire", name+".bin")
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

// AnnounceGrown is one MESSAGE row read again over its connection's
// announcement with the announcement's id table grown to Entries entries
// (docs/SPEC-TABLES.md §3.3: the announcement is a form 1 file, so §3's id
// table rules are its rules; §3, THE DISTINCTNESS CHECK IS BOUNDED WORK: a
// table of more than 4096 entries is malformed, and so is one that names an id
// twice). The added ids are named by nothing in the body, so an announcement
// the read accepts announces the vocabulary the connection's own does, and the
// row owes its message golden. Malformed is the other answer: the read refuses
// the announcement as damage, the connection carries no vocabulary, and the
// leg answers the row absent as a refusal.
type AnnounceGrown struct {
	Name    string
	Message string // the message row read again
	Entries int
	Repeat  bool // the last entry repeats the first
	Refused bool
	File    string // filled in by the harness once materialised
}

// AnnouncementMalformed is the diagnostic reason for a message row whose
// connection's announcement was refused as damage (docs/SPEC-TABLES.md §3.3).
const AnnouncementMalformed = "malformed announcement: the connection carries no vocabulary (SPEC-TABLES §3.3)"

// GrowIdTable returns the form 1 file with its id table padded to n entries,
// each added id one the table does not already carry and below the three
// reserved ids at the top of the range (splitmix64 of a counter, so the bytes
// are a pure function of the file and n). With repeat, the last entry is set
// to the first, which makes the table name one id twice.
func GrowIdTable(file []byte, n int, repeat bool) ([]byte, error) {
	if len(file) < 9 {
		return nil, fmt.Errorf("a form 1 file of %d bytes carries no id table", len(file))
	}
	k := int(binary.LittleEndian.Uint64(file[len(file)-8:]))
	if k < 1 || k > n || 8*(k+1) > len(file)-1 {
		return nil, fmt.Errorf("an id table of %d entries cannot be grown to %d in a file of %d bytes", k, n, len(file))
	}
	have := map[uint64]bool{}
	for i := range k {
		have[binary.LittleEndian.Uint64(file[len(file)-8-8*(k-i):])] = true
	}
	out := bytes.Clone(file[:len(file)-8])
	for x := uint64(1); k < n; x++ {
		z := x * 0x9E3779B97F4A7C15
		z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
		z = (z ^ (z >> 27)) * 0x94D049BB133111EB
		z ^= z >> 31
		if have[z] || z >= 0xFFFFFFFFFFFFFFFD {
			continue
		}
		have[z] = true
		out = binary.LittleEndian.AppendUint64(out, z)
		k++
	}
	if repeat {
		first := len(out) - 8*n
		copy(out[len(out)-8:], out[first:first+8])
	}
	return binary.LittleEndian.AppendUint64(out, uint64(n)), nil
}

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
	Grown       []AnnounceGrown
	Reports     []ReportCase
	Framings    []FramingCase
	Rewrites    []RewriteCase
	Hostiles    []Hostile
	Retains     []RetainCase
	Cooks       []Cook
	CookWrites  []CookWrite
	Blocks      []Block
	Forgeries   []Forgery
	// JSONWriteRefusals are the json-write rows whose answer is the writer's
	// refusal (docs/SPEC-TABLES.md §16.14, W0).
	JSONWriteRefusals []JSONWriteRefusal
	// WriteRefusals is the write contract's battery (docs/SPEC.md §5.1): one
	// value per row that breaks its declaration, or the lawful value at the
	// same boundary, handed to the writer the row's form names.
	WriteRefusals []WriteRefusal
}

// WriteRefusal is one row of the `write-refusal` surface (docs/SPEC.md §5.1):
// load Wire as Root, set the field at Path by Kind to Value, and save. Verdict
// is what the save owes: `refused` for a value the declaration does not admit,
// and `wrote` for the lawful value beside it, which is what keeps a driver
// that refuses everything from passing.
//
// Path is dotted field names with `[i]` element indexes, from the root.
// Kind says what the value sets: `int` an integer (a ranged scalar or bits),
// `count` the used count of the array at Path (new elements are copies of
// element 0), `length` the used length of the text at Path (filled with `a`),
// `ordinal` an enum by its raw ordinal, `tag` a union by its raw tag, `float`
// one of nan, inf or -inf, `bytes` a narrow text's exact bytes in hex, and
// `units` a wide text's exact UTF-16 units, four hex digits each.
type WriteRefusal struct {
	// Form is the writer the row drives, and the line kind and surface it
	// travels under: "" the variable-form Save (`write-refusal`), "fixed" the
	// fixed form's FixedSave (`write-refusal-fixed`), "message" SaveMessages
	// against the connection named in Unit (`write-refusal-message`), and
	// "json" the text writer (`write-refusal-json`).
	Form    string
	Name    string
	Unit    string
	Root    string
	Wire    string
	Path    string
	Kind    string
	Value   string
	Verdict string
}

// writeRefusalKinds is the closed set a row's Kind is drawn from.
var writeRefusalKinds = map[string]bool{"int": true, "count": true, "length": true, "ordinal": true, "tag": true, "float": true, "bytes": true, "units": true}

// writeRefusalForms maps each write-refusal line kind, which is also its
// surface, to the writer it drives.
var writeRefusalForms = map[string]string{"write-refusal": "", "write-refusal-fixed": "fixed", "write-refusal-message": "message", "write-refusal-json": "json"}

// Surface is the surface, and the manifest line kind, a row travels under.
func (wr WriteRefusal) Surface() string {
	if wr.Form == "" {
		return "write-refusal"
	}
	return "write-refusal-" + wr.Form
}

// JSONWriteRefusal is one value the text writer must REFUSE (docs/SPEC-TABLES.md
// §16.14, W0): the wire at Wire, loaded at Root, then — when Damage names one —
// a top-level text field's storage set, in code, to what no wire or text can
// deliver: `narrow:<field>` holds the bytes 61 FF 62 (ill-formed UTF-8), and
// `wide:<field>` the units 0061 D800 0062 (an unpaired surrogate). `none`
// writes the value as loaded. ToJson must return -1 and the driver answers
// `refused\n` as <case>.json; a text is a failure.
type JSONWriteRefusal struct {
	Name   string
	Unit   string
	Root   string
	Wire   string
	Damage string
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
func ReadManifest(path, jsonDir string) (m *Manifest, err error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() {
		if cerr := file.Close(); cerr != nil && err == nil {
			m, err = nil, cerr
		}
	}()

	m = &Manifest{}
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
			if len(f) < 5 {
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
					if slices.Contains(ports, "") || slices.Contains(ports, "go") {
						return nil, fmt.Errorf("%s: refused-by names an empty port or go (go-refusal= already holds go)", where)
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
		case "json-write-refusal":
			if len(f) != 6 {
				return nil, fmt.Errorf("%s: json-write-refusal takes case, unit, root, wire and damage", where)
			}
			damage := f[5]
			if damage != "none" && !strings.HasPrefix(damage, "narrow:") && !strings.HasPrefix(damage, "wide:") {
				return nil, fmt.Errorf("%s: %q is not a damage: none, narrow:<field> or wide:<field>", where, damage)
			}
			m.JSONWriteRefusals = append(m.JSONWriteRefusals, JSONWriteRefusal{Name: f[1], Unit: f[2], Root: f[3], Wire: f[4], Damage: damage})
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
		case "announce-grown":
			// announce-grown <name> <message> <entries> <distinct|repeat> <message|malformed|read|refused>
			if len(f) != 6 {
				return nil, fmt.Errorf("%s: announce-grown takes name, message row, entries, distinct or repeat, and the answer", where)
			}
			g := AnnounceGrown{Name: f[1], Message: f[2]}
			n, nerr := strconv.Atoi(f[3])
			if nerr != nil || n < 1 {
				return nil, fmt.Errorf("%s: %q is not an entry count", where, f[3])
			}
			g.Entries = n
			switch f[4] {
			case "distinct":
			case "repeat":
				g.Repeat = true
			default:
				return nil, fmt.Errorf("%s: %q is not distinct or repeat", where, f[4])
			}
			switch f[5] {
			case "message", "read":
			case "malformed", "refused":
				g.Refused = true
			default:
				return nil, fmt.Errorf("%s: %q is not an answer; it is message or malformed", where, f[5])
			}
			m.Grown = append(m.Grown, g)
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
				Save:  SaveWire(jsonDir, f[1]),
			})
		case "rewrite":
			// rewrite <case> <unit> <root> <wire>: an input every leg reads and
			// writes back, held to the canonical bytes (docs/SPEC-TABLES.md §22)
			if len(f) != 5 {
				return nil, fmt.Errorf("%s: rewrite takes case, unit, root, wire", where)
			}
			for _, rw := range m.Rewrites {
				if rw.Name == f[1] {
					return nil, fmt.Errorf("%s: rewrite %s is named twice", where, f[1])
				}
			}
			m.Rewrites = append(m.Rewrites, RewriteCase{Name: f[1], Unit: f[2], Root: f[3], Wire: f[4], Canonical: RewriteCanonical(jsonDir, f[1])})
		case "framing":
			// framing <case> <unit> <root> <wire> <read|refused> <value|->
			if len(f) != 7 {
				return nil, fmt.Errorf("%s: framing takes case, unit, root, wire, verdict, value file or -", where)
			}
			fc := FramingCase{Name: f[1], Unit: f[2], Root: f[3], Wire: f[4], Save: SaveWire(jsonDir, f[1])}
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
			h := Hostile{Name: f[1], Unit: f[2], Root: f[3], Tree: f[4], Save: SaveWire(jsonDir, f[1])}
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
		case "write-refusal", "write-refusal-fixed", "write-refusal-message", "write-refusal-json":
			// write-refusal[-form] <case> <unit> <root> <wire> <path> <kind> <value> <verdict>
			// (a message row's <unit> is a connection key)
			// The DERIVED manifest carries the line without its verdict.
			if len(f) != 9 && len(f) != 8 {
				return nil, fmt.Errorf("%s: write-refusal takes case, unit, root, wire, path, kind, value, verdict", where)
			}
			if !writeRefusalKinds[f[6]] {
				return nil, fmt.Errorf("%s: %q is not a write-refusal kind", where, f[6])
			}
			wr := WriteRefusal{Form: writeRefusalForms[f[0]], Name: f[1], Unit: f[2], Root: f[3], Wire: f[4], Path: f[5], Kind: f[6], Value: f[7]}
			if len(f) == 9 {
				if f[8] != "refused" && f[8] != "wrote" {
					return nil, fmt.Errorf("%s: %q is not a write verdict: a save is `refused` or `wrote`", where, f[8])
				}
				wr.Verdict = f[8]
			}
			m.WriteRefusals = append(m.WriteRefusals, wr)
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
	for _, g := range m.Grown {
		msg, err := m.LookupMessage(g.Message)
		if err != nil {
			return nil, fmt.Errorf("%s: announce-grown %s: %w", path, g.Name, err)
		}
		if msg.Refused() {
			return nil, fmt.Errorf("%s: announce-grown %s: %s is a named refusal row and has no message form", path, g.Name, g.Message)
		}
		if _, err := m.LookupConnection(msg.Connection); err != nil {
			return nil, fmt.Errorf("%s: announce-grown %s: %w", path, g.Name, err)
		}
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

func (m *Manifest) LookupMessage(name string) (Message, error) {
	for _, msg := range m.Messages {
		if msg.Name == name {
			return msg, nil
		}
	}
	return Message{}, fmt.Errorf("the manifest names no message %q", name)
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
