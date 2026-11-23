// THE 2026-11-22 SURFACES: the rules the shared corpus could not ask before.
//
// Three surfaces and one more write-refusal writer:
//
//   - `announce` puts a unit's ANNOUNCEMENT to its writer and its reader
//     (docs/SPEC-TABLES.md §3.3): the sender's announcement bytes (FL-38: no
//     writer announces more than 4096 entries), a strict and a lenient read of
//     a peer's announcement (FL-36), and a SaveMessages with no announcement in
//     hand (FL-18).
//   - `read-into` hands ONE set of caller values and ONE report to three reads
//     in a row, a lawful one, a refused one and the lawful one again
//     (docs/SPEC-TABLES.md §6.5): every read zeroes the report it is handed
//     (FL-02), a refusal leaves every value the call was handed at its declared
//     defaults (FL-03), and the read refuses through its report and never by
//     throwing (FL-29). Its `fixed-nocap` form loads a fixed file with no
//     stated capacity and owes the file's own record count (FL-17).
//   - `json-write-into` runs the text writer in a comma-decimal process
//     locale (FL-14) into a caller buffer larger than the text, whose bytes
//     past the text must be untouched (FL-15).
//   - `write-refusal-cook` drives the write contract's rows through the cook
//     writer; its `point` rows hand it a cyclic pointer graph (FL-28).
//
// The new write-refusal kinds (`elems`, `mask`, `dupkey`, `point`) and the
// fresh-value wire `-` are in manifest.go beside the rest of the battery.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// AnnounceDir holds the bytes an `announce` row that writes owes, generated
// from the engine by `make conformance-generate`.
const AnnounceDir = "testdata/conformance/tables/announce"

// ReadIntoDir holds the answer each `read-into` row owes, generated from the
// engine by `make conformance-generate`.
const ReadIntoDir = "testdata/conformance/tables/read-into"

// AnnounceCase is one `announce` row:
//
//	announce <case> <unit> <root> <mode> <input> <verdict>
//
// Mode `write` asks the unit's announcement writer for its bytes (root and
// input `-`); the verdict is `wrote` (the bytes are owed, Golden) or
// `refused`. Modes `strict` and `lenient` read the announcement of the
// connection Input names into a fresh vocabulary of Unit, with the strict flag
// set or left at its default; the verdict is `read` or `refused`. Mode `send`
// loads Input (the root's current-form wire, or `-` for a fresh value) as Root
// and writes it with SaveMessages as a batch of one, with no announcement read
// or written; the verdict is `wrote` (the bytes are owed, Golden) or
// `refused`.
type AnnounceCase struct {
	Name    string
	Unit    string
	Root    string
	Mode    string
	Input   string
	Verdict string
}

// Golden is the file holding the bytes a writing row owes.
func (a AnnounceCase) Golden() string { return AnnounceDir + "/" + a.Name + ".bin" }

var announceModes = map[string][]string{
	"write":   {"wrote", "refused"},
	"strict":  {"read", "refused"},
	"lenient": {"read", "refused"},
	"send":    {"wrote", "refused"},
}

func parseAnnounce(f []string, where string) (AnnounceCase, error) {
	if len(f) != 7 {
		return AnnounceCase{}, fmt.Errorf("%s: announce takes case, unit, root, mode, input, verdict", where)
	}
	a := AnnounceCase{Name: f[1], Unit: f[2], Root: f[3], Mode: f[4], Input: f[5], Verdict: f[6]}
	verdicts, ok := announceModes[a.Mode]
	if !ok {
		return a, fmt.Errorf("%s: %q is not an announce mode (write, strict, lenient, send)", where, a.Mode)
	}
	if a.Verdict != verdicts[0] && a.Verdict != verdicts[1] {
		return a, fmt.Errorf("%s: %q is not a verdict of mode %s: it is %s or %s", where, a.Verdict, a.Mode, verdicts[0], verdicts[1])
	}
	switch a.Mode {
	case "write":
		if a.Root != "-" || a.Input != "-" {
			return a, fmt.Errorf("%s: an announce write takes root - and input -", where)
		}
	case "strict", "lenient":
		if a.Root != "-" || a.Input == "-" {
			return a, fmt.Errorf("%s: an announce %s read takes root - and a connection", where, a.Mode)
		}
	case "send":
		if a.Root == "-" || a.Input == "" {
			return a, fmt.Errorf("%s: an announce send takes a root and a wire (or -)", where)
		}
	}
	return a, nil
}

// derivedInput is what a driver is handed in the input column: a strict or
// lenient row's connection resolved to its announcement file.
func (a AnnounceCase) derivedInput(m *Manifest) (string, error) {
	if a.Mode == "strict" || a.Mode == "lenient" {
		c, err := m.LookupConnection(a.Input)
		if err != nil {
			return "", fmt.Errorf("%s: %w", a.Name, err)
		}
		return c.Wire, nil
	}
	return a.Input, nil
}

// want is the answer the row owes.
func (a AnnounceCase) want() ([]byte, error) {
	if a.Verdict == "wrote" {
		b, err := os.ReadFile(a.Golden())
		if err != nil {
			return nil, fmt.Errorf("%s: the engine's announcement golden: %w — run: make conformance-generate", a.Name, err)
		}
		return b, nil
	}
	return []byte(a.Verdict + "\n"), nil
}

// ReadInto is one `read-into` row:
//
//	read-into <case> <unit> <root> <form> <lawful> <damaged> <count>
//
// Form is `variable` (the variable-form Load), `fixed` (the fixed form's
// batch load), `message` (LoadMessages, Unit a connection key whose
// announcement is read first), `json` (the text read), or `fixed-nocap` (the
// fixed load with no stated capacity; Damaged is `-`). Count is how many
// values the call is handed: 1 for `variable` and `json`, the lawful file's
// records or bodies for `fixed` and `message`; for `fixed-nocap` it is the
// answer, the record count the load returns, and the driver is not told it.
type ReadInto struct {
	Name    string
	Unit    string
	Root    string
	Form    string
	Lawful  string
	Damaged string
	Count   int
}

// Golden is the answer the row owes.
func (r ReadInto) Golden() string { return ReadIntoDir + "/" + r.Name + ".txt" }

var readIntoForms = map[string]bool{"variable": true, "fixed": true, "message": true, "json": true, "fixed-nocap": true}

func parseReadInto(f []string, where string) (ReadInto, error) {
	if len(f) != 8 {
		return ReadInto{}, fmt.Errorf("%s: read-into takes case, unit, root, form, lawful, damaged, count", where)
	}
	r := ReadInto{Name: f[1], Unit: f[2], Root: f[3], Form: f[4], Lawful: f[5], Damaged: f[6]}
	if !readIntoForms[r.Form] {
		return r, fmt.Errorf("%s: %q is not a read-into form (variable, fixed, message, json, fixed-nocap)", where, r.Form)
	}
	n, err := strconv.Atoi(f[7])
	if err != nil || n < 1 {
		return r, fmt.Errorf("%s: %q is not a count of values", where, f[7])
	}
	r.Count = n
	if (r.Form == "fixed-nocap") != (r.Damaged == "-") {
		return r, fmt.Errorf("%s: a fixed-nocap row, and only it, takes damaged -", where)
	}
	if (r.Form == "variable" || r.Form == "json") && n != 1 {
		return r, fmt.Errorf("%s: a %s read is handed one value", where, r.Form)
	}
	return r, nil
}

// derivedLine is the row as a driver sees it: the count withheld where it is
// the answer, and a message row's connection resolved to its unit and its
// announcement file.
func (r ReadInto) derivedLine(m *Manifest) (string, error) {
	unit, announcement := r.Unit, "-"
	if r.Form == "message" {
		c, err := m.LookupConnection(r.Unit)
		if err != nil {
			return "", fmt.Errorf("%s: %w", r.Name, err)
		}
		unit, announcement = c.Unit, c.Wire
	}
	count := strconv.Itoa(r.Count)
	if r.Form == "fixed-nocap" {
		count = "-"
	}
	return fmt.Sprintf("read-into %s %s %s %s %s %s %s %s\n", r.Name, unit, r.Root, r.Form, r.Lawful, r.Damaged, count, announcement), nil
}

// JSONWriteInto is one `json-write-into` row: the instance's text written in
// a comma-decimal process locale into a caller buffer larger than the text.
//
//	json-write-into <case> <instance>
type JSONWriteInto struct {
	Name     string
	Instance string
}

// CommaLocaleEnv names the comma-decimal locale the harness hands every
// driver on `json-write-into`; LOCPATH beside it when the harness had to
// compile the locale itself.
const CommaLocaleEnv = "SCHEMA_COMMA_LOCALE"

// commaLocale returns the environment a json-write-into driver runs under: a
// process locale whose decimal separator is a comma, which the driver sets
// before it writes. A host that can neither find nor build one is an error,
// never a skip: a locale rule checked under the C locale checks nothing.
func commaLocale(work string) ([]string, error) {
	const name = "de_DE.UTF-8"
	if runtime.GOOS == "darwin" {
		if _, err := os.Stat("/usr/share/locale/" + name); err != nil {
			return nil, fmt.Errorf("json-write-into: this macOS host has no %s locale: %w", name, err)
		}
		return []string{CommaLocaleEnv + "=" + name}, nil
	}
	out, err := exec.Command("locale", "-a").Output()
	if err != nil {
		return nil, fmt.Errorf("json-write-into: listing the host's locales: %w", err)
	}
	for _, l := range strings.Fields(string(out)) {
		if l == "de_DE.utf8" || l == name {
			return []string{CommaLocaleEnv + "=" + name}, nil
		}
	}
	dir := filepath.Join(work, "locale")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	cmd := exec.Command("localedef", "-i", "de_DE", "-f", "UTF-8", filepath.Join(dir, name))
	if msg, err := cmd.CombinedOutput(); err != nil {
		// localedef exits 1 for warnings and still writes the locale; the
		// locale existing afterwards is the test
		if _, serr := os.Stat(filepath.Join(dir, name, "LC_NUMERIC")); serr != nil {
			return nil, fmt.Errorf("json-write-into: the host has no %s and localedef could not build one: %v\n%s", name, err, msg)
		}
	}
	return []string{CommaLocaleEnv + "=" + name, "LOCPATH=" + dir}, nil
}

var commaLocaleOnce = sync.OnceValues(func() ([]string, error) {
	dir, err := os.MkdirTemp("", "conformance-locale")
	if err != nil {
		return nil, err
	}
	return commaLocale(dir)
})

// generate1122 writes the answers the 2026-11-22 rows owe, from the engine:
// the announcement bytes and the message bytes a writing `announce` row owes,
// and the answer of every `read-into` row. A row whose own verdict the engine
// can check is checked, so the data never asks a leg for an answer the
// reference does not give.
func generate1122(u *units, m *Manifest) error {
	if err := os.MkdirAll(AnnounceDir, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(ReadIntoDir, 0o755); err != nil {
		return err
	}
	for _, a := range m.Announces {
		unit, err := u.get(a.Unit)
		if err != nil {
			return err
		}
		switch a.Mode {
		case "write":
			entries := len(tablewire.MessageVocabulary(unit))
			if over := entries > ir.TableVocabularyMaxEntries; over != (a.Verdict == "refused") {
				return fmt.Errorf("announce %s: the unit announces %d entries (cap %d) and the row says %s", a.Name, entries, ir.TableVocabularyMaxEntries, a.Verdict)
			}
			if a.Verdict == "wrote" {
				if err := writeFileAtomic(a.Golden(), tablewire.Announce(unit)); err != nil {
					return err
				}
			}
		case "send":
			if a.Verdict != "wrote" {
				continue
			}
			model := tabletext.NewModel(unit)
			root := unit.Tables[a.Root]
			if root == nil {
				return fmt.Errorf("announce %s: root %q names no table", a.Name, a.Root)
			}
			value := model.New(root)
			if a.Input != "-" {
				wire, err := os.ReadFile(a.Input)
				if err != nil {
					return fmt.Errorf("announce %s: %w", a.Name, err)
				}
				var rep tabletext.Report
				if ok, err := strictDecode(model, value, wire, &rep); !ok {
					return fmt.Errorf("announce %s: the engine refuses the row's wire: %v", a.Name, err)
				}
			}
			saved, err := tablewire.EncodeMessages(model, []*tabletext.Instance{value})
			if err != nil {
				return fmt.Errorf("announce %s: EncodeMessages: %w", a.Name, err)
			}
			if err := writeFileAtomic(a.Golden(), saved); err != nil {
				return err
			}
		}
		fmt.Printf("conformance: %-22s announce %s %s\n", a.Name, a.Mode, a.Verdict)
	}
	for _, r := range m.ReadIntos {
		unitKey := r.Unit
		if r.Form == "message" {
			c, err := m.LookupConnection(r.Unit)
			if err != nil {
				return fmt.Errorf("read-into %s: %w", r.Name, err)
			}
			unitKey = c.Unit
		}
		unit, err := u.get(unitKey)
		if err != nil {
			return err
		}
		model := tabletext.NewModel(unit)
		root := unit.Tables[r.Root]
		if root == nil {
			return fmt.Errorf("read-into %s: root %q names no table", r.Name, r.Root)
		}
		var answer []byte
		if r.Form == "fixed-nocap" {
			answer = fmt.Appendf(nil, "read\n%d\n", r.Count)
		} else {
			if r.Form == "variable" {
				// the engine reads the variable form; the row's two verdicts
				// are checked here, the other forms by the reference leg
				for _, c := range []struct {
					path  string
					reads bool
				}{{r.Lawful, true}, {r.Damaged, false}} {
					wire, err := os.ReadFile(c.path)
					if err != nil {
						return fmt.Errorf("read-into %s: %w", r.Name, err)
					}
					var rep tabletext.Report
					if ok, _ := strictDecode(model, model.New(root), wire, &rep); ok != c.reads {
						return fmt.Errorf("read-into %s: the engine's read of %s is %v, the row needs %v", r.Name, c.path, ok, c.reads)
					}
				}
			}
			defaults, err := model.Write(model.New(root))
			if err != nil {
				return fmt.Errorf("read-into %s: the root's default text: %w", r.Name, err)
			}
			// the document ends with one line feed (§16.14 W2), as ToJson
			// writes it
			defaults = oneText(defaults)
			answer = []byte("read\nrefused\n")
			for range r.Count {
				answer = append(answer, defaults...)
			}
			answer = append(answer, "read\n"...)
		}
		if err := writeFileAtomic(r.Golden(), answer); err != nil {
			return err
		}
		fmt.Printf("conformance: %-22s read-into %s\n", r.Name, r.Form)
	}
	return nil
}
