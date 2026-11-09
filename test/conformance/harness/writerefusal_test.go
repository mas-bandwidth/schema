package main

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestWriteRefusalSurfaceHoldsTheGoEngine is the write contract's worked leg
// (docs/SPEC.md §5.1): every row of the four write-refusal surfaces is loaded,
// its one field is set to the row's value, and the row's writer is run. The
// Go engine refuses exactly the rows the manifest calls `refused` and writes
// exactly the lawful twins the manifest calls `wrote`, so a leg that refused
// everything or accepted everything fails on the battery itself.
func TestWriteRefusalSurfaceHoldsTheGoEngine(t *testing.T) {
	t.Parallel()
	root := conformanceRoot(t)
	m, err := ReadManifest(filepath.Join(root, defaultManifest), filepath.Join(root, defaultJSONDir))
	if err != nil {
		t.Fatal(err)
	}
	for i := range m.Units {
		for j, path := range m.Units[i].Paths {
			if !filepath.IsAbs(path) {
				m.Units[i].Paths[j] = filepath.Join(root, path)
			}
		}
	}
	if len(m.WriteRefusals) == 0 {
		t.Fatal("the manifest carries no write-refusal rows")
	}
	seen := map[string]bool{}
	for _, s := range surfaces {
		if strings.HasPrefix(s, "write-refusal") {
			seen[s] = true
		}
	}
	for _, wr := range m.WriteRefusals {
		if !seen[wr.Surface()] {
			t.Errorf("%s: surface %q is not registered", wr.Name, wr.Surface())
		}
	}
	u := newUnits(m)
	for _, wr := range m.WriteRefusals {
		wr := wr
		t.Run(wr.Surface()+"/"+wr.Name, func(t *testing.T) {
			t.Parallel()
			answer, err := writeRefusalAnswer(root, m, u, wr)
			if err != nil {
				t.Fatal(err)
			}
			if answer == wr.Verdict {
				return
			}
			// A leg whose own types cannot hold the value has nothing for its
			// writer to refuse; the manifest admits `unrepresentable` only
			// against a `refused` row.
			if answer == "unrepresentable" && wr.Verdict == "refused" {
				return
			}
			if answer == "absent" && wr.Form == "fixed" {
				t.Logf("%s: this unit emits no fixed form for %s, so the row is absent", wr.Name, wr.Root)
				return
			}
			t.Errorf("%s: the Go engine answered %q, the manifest owes %q", wr.Name, answer, wr.Verdict)
		})
	}
}

// writeRefusalAnswer loads one row's wire, applies its one mutation, and runs
// the writer the row's form names. A writer refusal is `refused`; a complete
// write is `wrote`; a value the row asks for that the instance cannot hold is
// `unrepresentable`.
func writeRefusalAnswer(root string, m *Manifest, u *units, wr WriteRefusal) (string, error) {
	unitKey := wr.Unit
	if wr.Form == "message" {
		conn, err := m.LookupConnection(wr.Unit)
		if err != nil {
			return "", err
		}
		unitKey = conn.Unit
	}
	unit, err := u.get(unitKey)
	if err != nil {
		return "", err
	}
	model := tabletext.NewModel(unit)
	def := model.Lookup(wr.Root)
	if def == nil {
		return "", fmt.Errorf("%s: unit %s declares no root %q", wr.Name, unitKey, wr.Root)
	}
	if wr.Form == "fixed" && !ir.TableFixedEmitted(unit, def) {
		return "absent", nil
	}
	wirePath := wr.Wire
	if !filepath.IsAbs(wirePath) {
		wirePath = filepath.Join(root, wirePath)
	}
	wire, err := os.ReadFile(wirePath)
	if err != nil {
		return "", fmt.Errorf("%s: %w", wr.Name, err)
	}
	inst := model.New(def)
	var report tabletext.Report
	if ok, err := tablewire.DecodeVariable(model, inst, wire, &report); !ok || err != nil {
		return "", fmt.Errorf("%s: the wire did not load: %v %+v", wr.Name, err, report)
	}
	held, err := mutateWriteRefusal(model, inst, wr.Path, wr.Kind, wr.Value)
	if err != nil {
		return "", fmt.Errorf("%s: %w", wr.Name, err)
	}
	if !held {
		return "unrepresentable", nil
	}
	switch wr.Form {
	case "":
		if _, err := tablewire.EncodeVariable(model, inst); err != nil {
			return "refused", nil
		}
		return "wrote", nil
	case "fixed":
		if _, err := tablewire.EncodeFixed(model, inst); err != nil {
			return "refused", nil
		}
		return "wrote", nil
	case "message":
		if _, err := tablewire.EncodeMessage(model, inst); err != nil {
			return "refused", nil
		}
		return "wrote", nil
	case "json":
		if _, err := model.Write(inst); err != nil {
			return "refused", nil
		}
		return "wrote", nil
	}
	return "", fmt.Errorf("%s: unknown write-refusal form %q", wr.Name, wr.Form)
}

var writeRefusalSegment = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)(?:\[([0-9]+)\])?$`)

// mutateWriteRefusal sets the one field a row names. It answers false only
// when the value does not fit the instance model at all.
func mutateWriteRefusal(model *tabletext.Model, inst *tabletext.Instance, path, kind, text string) (bool, error) {
	fv, cell, f, err := writeRefusalTarget(inst, path)
	if err != nil {
		return false, err
	}
	switch kind {
	case "count":
		n, err := strconv.Atoi(text)
		if err != nil {
			return false, err
		}
		if fv.Def.Array != ir.ArrayCounted && fv.Def.Array != ir.ArrayFixed {
			return false, fmt.Errorf("%s: count on a field that is not an array", path)
		}
		fv.Count = n
		return true, nil
	case "length":
		n, err := strconv.Atoi(text)
		if err != nil {
			return false, err
		}
		if n < 0 {
			fv.Count = n
			return true, nil
		}
		switch f.Type.Kind {
		case ir.TString, ir.TBytes:
			cell.Str = bytes.Repeat([]byte{'a'}, n)
		case ir.TWString:
			units := make([]uint16, n)
			for i := range units {
				units[i] = 'a'
			}
			cell.Units = units
		default:
			return false, fmt.Errorf("%s: length on a %v field", path, f.Type.Kind)
		}
		fv.Count = n
		return true, nil
	case "bytes":
		b, err := hex.DecodeString(text)
		if err != nil {
			return false, fmt.Errorf("%s: %w", path, err)
		}
		cell.Str = b
		fv.Count = len(b)
		return true, nil
	case "units":
		b, err := hex.DecodeString(text)
		if err != nil || len(b)%2 != 0 {
			return false, fmt.Errorf("%s: units are four hex digits each", path)
		}
		units := make([]uint16, len(b)/2)
		for i := range units {
			units[i] = uint16(b[2*i])<<8 | uint16(b[2*i+1])
		}
		cell.Units = units
		fv.Count = len(units)
		return true, nil
	case "int":
		return setRefusalInt(cell, f, text), nil
	case "ordinal":
		n, err := strconv.ParseUint(text, 10, 64)
		if err != nil {
			return false, err
		}
		cell.U = n
		cell.I = int64(n)
		return true, nil
	case "tag":
		n, err := strconv.ParseUint(text, 10, 64)
		if err != nil {
			return false, err
		}
		cell.U = n
		if un := tabletext.UnionOf(f); un != nil && n > 0 && int(n) <= len(un.Variants) {
			arm := un.Variants[n-1]
			switch {
			case arm.Void():
			case arm.Body():
				cell.Tab = model.New(tabletext.StructOf(arm.F))
				cell.Arm = nil
			default:
				cell.Arm = model.NewArm(arm)
				cell.Tab = nil
			}
		}
		return true, nil
	case "float":
		fv, err := refusalFloat(text)
		if err != nil {
			return false, err
		}
		cell.F = fv
		return true, nil
	}
	return false, fmt.Errorf("%s: unknown kind %q", path, kind)
}

func setRefusalInt(cell *tabletext.Cell, f *ir.Field, text string) bool {
	n, ok := new(big.Int).SetString(text, 10)
	if !ok {
		return false
	}
	cell.Wide = new(big.Int).Set(n)
	if n.IsInt64() {
		cell.I = n.Int64()
		cell.U = uint64(n.Int64())
	} else if n.Sign() >= 0 && n.BitLen() <= 64 {
		cell.U = n.Uint64()
		cell.I = int64(cell.U)
	}
	return true
}

func refusalFloat(text string) (float64, error) {
	switch text {
	case "nan":
		return math.NaN(), nil
	case "inf":
		return math.Inf(1), nil
	case "-inf":
		return math.Inf(-1), nil
	}
	return strconv.ParseFloat(text, 64)
}

// writeRefusalTarget walks a dotted path to the field or array element the row
// names, and returns its field storage beside the declaration the value takes.
func writeRefusalTarget(inst *tabletext.Instance, path string) (*tabletext.Field, *tabletext.Cell, *ir.Field, error) {
	segments := strings.Split(path, ".")
	cur := inst
	for _, segment := range segments[:len(segments)-1] {
		name, index, indexed, err := writeRefusalSegmentOf(segment)
		if err != nil {
			return nil, nil, nil, err
		}
		fv := writeRefusalField(cur, name)
		if fv == nil {
			return nil, nil, nil, fmt.Errorf("%s: no field %q in %s", path, name, cur.Def.Name)
		}
		if !indexed {
			if fv.Cell.Tab == nil {
				return nil, nil, nil, fmt.Errorf("%s: %q is not a nested value", path, name)
			}
			cur = fv.Cell.Tab
			continue
		}
		if index >= len(fv.Elems) {
			return nil, nil, nil, fmt.Errorf("%s: %s has no element %d", path, name, index)
		}
		if fv.Elems[index].Tab == nil {
			return nil, nil, nil, fmt.Errorf("%s: %s[%d] is not a nested value", path, name, index)
		}
		cur = fv.Elems[index].Tab
	}
	last := segments[len(segments)-1]
	name, index, indexed, err := writeRefusalSegmentOf(last)
	if err != nil {
		return nil, nil, nil, err
	}
	fv := writeRefusalField(cur, name)
	if fv == nil {
		return nil, nil, nil, fmt.Errorf("%s: no field %q in %s", path, name, cur.Def.Name)
	}
	if !indexed {
		return fv, &fv.Cell, fv.Def, nil
	}
	if index >= len(fv.Elems) {
		return nil, nil, nil, fmt.Errorf("%s: %s has no element %d", path, name, index)
	}
	element := *fv.Def
	element.Array = ir.ArrayNone
	element.KeyEnum = ""
	element.KeyEnumRef = nil
	element.Type.Optional = false
	element.Required = false
	return fv, &fv.Elems[index], &element, nil
}

func writeRefusalSegmentOf(segment string) (string, int, bool, error) {
	match := writeRefusalSegment.FindStringSubmatch(segment)
	if match == nil {
		return "", 0, false, fmt.Errorf("%q is not a field path segment", segment)
	}
	if match[2] == "" {
		return match[1], 0, false, nil
	}
	index, err := strconv.Atoi(match[2])
	if err != nil {
		return "", 0, false, err
	}
	return match[1], index, true, nil
}

func writeRefusalField(inst *tabletext.Instance, name string) *tabletext.Field {
	for i := range inst.Fields {
		if inst.Fields[i].Def.Name == name {
			return &inst.Fields[i]
		}
	}
	return nil
}
