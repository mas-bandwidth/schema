// THE WRITE-REFUSAL SURFACES (docs/SPEC.md §5.1).
//
// Each row loads one instance, sets ONE field to the value the row names, and
// writes it through the form's writer: the variable-form Save, the fixed form's
// FixedSave, the message form's SaveMessages against the connection's unit, or
// the text writer. The answer is what that writer did: `refused` when it
// returned its refusal, `wrote` when the lawful twin or the text writer's
// zero-byte spelling completed, and `unrepresentable` when the value cannot be
// held in this leg's storage at all.
package main

import (
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"math/big"
	"os"
	"reflect"
	"regexp"
	"strconv"
	"strings"
)

var (
	writeRefusalGoSegment          = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)(?:\[([0-9]+)\])?$`)
	errWriteRefusalUnrepresentable = fmt.Errorf("unrepresentable")
)

func surfaceWriteRefusal(surface string, lines []line, out string) error {
	for _, f := range lines {
		if f[0] != surface {
			continue
		}
		name, unit, root := f[1], f[2], f[3]
		wirePath, path, kind, text := f[4], f[5], f[6], f[7]
		actualUnit := unit
		if surface == "write-refusal-message" {
			actualUnit = connectionUnit(lines, unit)
			if actualUnit == "" {
				return fmt.Errorf("%s: connection %q names no unit", name, unit)
			}
		}
		c := findCodec(actualUnit, root)
		if c == nil {
			if err := spillAbsent(out, name); err != nil {
				return err
			}
			continue
		}
		// THE ROOT'S OWN FILE-FORM READ: a declared fixed root's current-form
		// instance is form 3, read by c.load. variableLoad is the form-1 read
		// the framing surface keeps to itself (fixed.go).
		read := c.load
		wire, err := os.ReadFile(wirePath)
		if err != nil {
			return err
		}
		value := c.fresh()
		var rep report
		if !read(value, wire, &rep) {
			return fmt.Errorf("%s: load refused %s", name, wirePath)
		}
		if err := mutateWriteRefusalGo(value, path, kind, text); err != nil {
			if errors.Is(err, errWriteRefusalUnrepresentable) {
				if err := spill(out, name, []byte("unrepresentable\n")); err != nil {
					return err
				}
				continue
			}
			return fmt.Errorf("%s: %w", name, err)
		}
		answer := "wrote"
		switch surface {
		case "write-refusal":
			// THE ROOT'S OWN FORM'S WRITER, the one the row's verdict names: a
			// fixed root is written by the fixed form (c.measure/c.save), as
			// the reference leg writes it. variableMeasure/variableSave are
			// the form-1 twins the framing surface keeps to itself (fixed.go).
			if n := c.measure(value); n < 0 || c.save(value, make([]byte, n)) != n {
				answer = "refused"
			}
		case "write-refusal-fixed":
			if n := c.measure(value); n < 0 || c.save(value, make([]byte, n)) != n {
				answer = "refused"
			}
		case "write-refusal-message":
			mc := findMessageCodec(actualUnit, root)
			if mc == nil || mc.saveOne == nil {
				if err := spillAbsent(out, name); err != nil {
					return err
				}
				continue
			}
			if mc.saveOne(value) < 0 {
				answer = "refused"
			}
		case "write-refusal-json":
			if n := c.toJsonMeasure(value); n < 0 {
				answer = "refused"
			} else if c.toJson(value, make([]byte, n)) != n {
				answer = "refused"
			}
		default:
			return fmt.Errorf("%s: unknown write-refusal surface %q", name, surface)
		}
		if err := spill(out, name, []byte(answer+"\n")); err != nil {
			return err
		}
	}
	return nil
}

func connectionUnit(lines []line, connection string) string {
	for _, f := range lines {
		if f[0] == "connection" && f[1] == connection {
			return f[2]
		}
	}
	return ""
}

// mutateWriteRefusalGo sets the one field a row names in the generated value.
func mutateWriteRefusalGo(value any, path, kind, text string) error {
	if mr, ok := value.(mutableRootProvider); ok {
		value = mr.mutableRoot()
	}
	segments := strings.Split(path, ".")
	cur := reflect.ValueOf(value).Elem()
	for _, segment := range segments[:len(segments)-1] {
		name, index, indexed, err := writeRefusalGoSegmentOf(segment)
		if err != nil {
			return err
		}
		fv, ok := writeRefusalGoField(cur, name)
		if !ok {
			return fmt.Errorf("%s: no field %q in %s", path, name, cur.Type())
		}
		if indexed {
			fv, err = writeRefusalGoIndex(fv, index, path)
			if err != nil {
				return err
			}
		}
		cur = fv
	}
	last := segments[len(segments)-1]
	name, index, indexed, err := writeRefusalGoSegmentOf(last)
	if err != nil {
		return err
	}
	fv, ok := writeRefusalGoField(cur, name)
	if !ok {
		return fmt.Errorf("%s: no field %q in %s", path, name, cur.Type())
	}
	if indexed {
		fv, err = writeRefusalGoIndex(fv, index, path)
		if err != nil {
			return err
		}
	}
	return writeRefusalGoSet(cur, fv, name, kind, text)
}

func writeRefusalGoIndex(fv reflect.Value, index int, path string) (reflect.Value, error) {
	if fv.Kind() == reflect.Array || fv.Kind() == reflect.Slice {
		return fv.Index(index), nil
	}
	if fv.CanAddr() {
		if at := fv.Addr().MethodByName("At"); at.IsValid() {
			out := at.Call([]reflect.Value{reflect.ValueOf(int32(index))})
			if len(out) == 1 && !out[0].IsNil() {
				return out[0].Elem(), nil
			}
			return reflect.Value{}, fmt.Errorf("%s: index %d out of bounds in %s", path, index, fv.Type())
		}
	}
	return reflect.Value{}, fmt.Errorf("%s: %s is not indexable", path, fv.Type())
}

func writeRefusalGoSet(owner, fv reflect.Value, name, kind, text string) error {
	switch kind {
	case "count":
		n, err := writeRefusalGoToken(name, text)
		if err != nil {
			return err
		}
		companion, ok := writeRefusalGoField(owner, name+"Count")
		if !ok {
			return fmt.Errorf("%s: no count beside %s", name, owner.Type())
		}
		return writeRefusalGoStore(companion, n)
	case "length":
		n, err := writeRefusalGoToken(name, text)
		if err != nil {
			return err
		}
		// THE LENGTH IS STORED FIRST, checked against its own declared kind,
		// so a length the companion cannot hold moves nothing. Past storage
		// only the length moves: the fill stops at the array's end, and the
		// writer must refuse before it reads past it.
		if fv.Kind() == reflect.Struct {
			if val, ok := writeRefusalGoField(fv, "Value"); ok {
				if comp, ok2 := writeRefusalGoField(fv, "ValueLength"); ok2 {
					if err := writeRefusalGoStore(comp, n); err != nil {
						return err
					}
					return writeRefusalGoFill(val, n, 'a')
				}
			}
		}
		companion, ok := writeRefusalGoField(owner, name+"Length")
		if !ok {
			return fmt.Errorf("%s: no length beside %s", name, owner.Type())
		}
		if err := writeRefusalGoStore(companion, n); err != nil {
			return err
		}
		return writeRefusalGoFill(fv, n, 'a')
	case "bytes":
		b, err := hex.DecodeString(text)
		if err != nil {
			return err
		}
		units := make([]uint16, len(b))
		for i := range units {
			units[i] = uint16(b[i])
		}
		if fv.Kind() == reflect.Struct {
			if val, ok := writeRefusalGoField(fv, "Value"); ok {
				if comp, ok2 := writeRefusalGoField(fv, "ValueLength"); ok2 {
					if err := writeRefusalGoUnits(val, units); err != nil {
						return err
					}
					return writeRefusalGoStore(comp, big.NewInt(int64(len(units))))
				}
			}
		}
		if err := writeRefusalGoUnits(fv, units); err != nil {
			return err
		}
		companion, ok := writeRefusalGoField(owner, name+"Length")
		if !ok {
			return fmt.Errorf("%s: no length beside %s", name, owner.Type())
		}
		return writeRefusalGoStore(companion, big.NewInt(int64(len(units))))
	case "units":
		b, err := hex.DecodeString(text)
		if err != nil || len(b)%2 != 0 {
			return fmt.Errorf("%s: units are four hex digits each", name)
		}
		units := make([]uint16, len(b)/2)
		for i := range units {
			units[i] = uint16(b[2*i])<<8 | uint16(b[2*i+1])
		}
		if err := writeRefusalGoUnits(fv, units); err != nil {
			return err
		}
		companion, ok := writeRefusalGoField(owner, name+"Length")
		if !ok {
			return fmt.Errorf("%s: no length beside %s", name, owner.Type())
		}
		return writeRefusalGoStore(companion, big.NewInt(int64(len(units))))
	case "int", "ordinal":
		// §5.1 row 11 on a bare int, and an enum's ordinal: the declared
		// storage is the field's own Go kind, and writeRefusalGoStore holds
		// the token to it.
		n, err := writeRefusalGoToken(name, text)
		if err != nil {
			return err
		}
		return writeRefusalGoStore(fv, n)
	case "tag":
		tag, err := writeRefusalGoToken(name, text)
		if err != nil {
			return err
		}
		typ, ok := writeRefusalGoField(fv, "Type")
		if !ok {
			return fmt.Errorf("%s: %s carries no union tag", name, fv.Type())
		}
		// the tag is stored before the payload is cleared, so a tag the
		// storage cannot hold leaves the value as it was read
		if err := writeRefusalGoStore(typ, tag); err != nil {
			return err
		}
		if payload, ok := writeRefusalGoField(fv, "Payload"); ok {
			payload.Set(reflect.Zero(payload.Type()))
		}
		return nil
	case "float", "real":
		// float names a non-finite value; real a finite decimal, `-0` included
		v, err := writeRefusalGoFloat(text)
		if kind == "real" {
			v, err = strconv.ParseFloat(text, 64)
		}
		if err != nil {
			return err
		}
		if fv.Kind() == reflect.Struct && fv.NumField() == 1 && fv.Type().Field(0).Name == "Value" {
			fv = fv.Field(0)
		}
		switch fv.Kind() {
		case reflect.Float32, reflect.Float64:
			// a finite decimal past a float32's range would be stored as an
			// infinity, which is a different value: the storage cannot hold it
			if fv.OverflowFloat(v) {
				return errWriteRefusalUnrepresentable
			}
			fv.SetFloat(v)
			if pres, ok := writeRefusalGoField(owner, name+"Present"); ok && pres.Kind() == reflect.Bool {
				pres.SetBool(true)
			}
		default:
			return fmt.Errorf("%s: %s is not a float", name, fv.Type())
		}
		return nil
	}
	return fmt.Errorf("%s: unknown kind %q", name, kind)
}

// writeRefusalGoToken reads a row's decimal integer at any width, so the token
// itself is never narrowed before it meets the field's declared kind.
func writeRefusalGoToken(name, text string) (*big.Int, error) {
	n, ok := new(big.Int).SetString(text, 10)
	if !ok {
		return nil, fmt.Errorf("%s: %q is not an integer", name, text)
	}
	return n, nil
}

// writeRefusalGoStore is THE ONE CHECKED CONVERSION every integer this driver
// assigns goes through: a bare integer, an enum's ordinal, a union's tag, a
// count, a length, and each unit of a string or a byte array. It is keyed on
// the destination's own reflect kind, which is the generated field's declared
// storage. A value that kind cannot hold is `unrepresentable` and nothing is
// stored: reflect's SetInt and SetUint keep the low bits and say nothing, so
// 256 in a one-byte ordinal would land as 0, a lawful member, and the row
// would answer `wrote` for a value that was never written.
func writeRefusalGoStore(fv reflect.Value, n *big.Int) error {
	switch fv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if !n.IsInt64() || fv.OverflowInt(n.Int64()) {
			return errWriteRefusalUnrepresentable
		}
		fv.SetInt(n.Int64())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if !n.IsUint64() || fv.OverflowUint(n.Uint64()) {
			return errWriteRefusalUnrepresentable
		}
		fv.SetUint(n.Uint64())
	default:
		return fmt.Errorf("%s is not integer storage", fv.Type())
	}
	return nil
}

// writeRefusalGoFill writes unit into the first n elements the storage has.
func writeRefusalGoFill(fv reflect.Value, n *big.Int, unit byte) error {
	u := big.NewInt(int64(unit))
	for i := 0; i < fv.Len() && big.NewInt(int64(i)).Cmp(n) < 0; i++ {
		if err := writeRefusalGoStore(fv.Index(i), u); err != nil {
			return err
		}
	}
	return nil
}

// writeRefusalGoUnits writes a string's units, or a byte array's bytes, into
// the elements the storage has.
func writeRefusalGoUnits(fv reflect.Value, units []uint16) error {
	for i := 0; i < len(units) && i < fv.Len(); i++ {
		if err := writeRefusalGoStore(fv.Index(i), big.NewInt(int64(units[i]))); err != nil {
			return err
		}
	}
	return nil
}

func writeRefusalGoFloat(text string) (float64, error) {
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

func writeRefusalGoSegmentOf(segment string) (string, int, bool, error) {
	match := writeRefusalGoSegment.FindStringSubmatch(segment)
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

func writeRefusalGoField(value reflect.Value, name string) (reflect.Value, bool) {
	typ := value.Type()
	want := writeRefusalGoName(name)
	wantFold := writeRefusalGoFold(name)
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.PkgPath != "" {
			continue
		}
		if field.Name == want || writeRefusalGoFold(field.Name) == wantFold {
			return value.Field(i), true
		}
	}
	if value.CanAddr() {
		ptr := value.Addr()
		ptyp := ptr.Type()
		for i := 0; i < ptyp.NumMethod(); i++ {
			m := ptyp.Method(i)
			if m.PkgPath != "" || m.Type.NumIn() != 1 || m.Type.NumOut() != 1 || m.Type.Out(0).Kind() != reflect.Pointer {
				continue
			}
			if m.Name == want || writeRefusalGoFold(m.Name) == wantFold {
				out := ptr.Method(i).Call(nil)
				if !out[0].IsNil() {
					return out[0].Elem(), true
				}
			}
		}
	}
	return reflect.Value{}, false
}

func writeRefusalGoName(name string) string {
	parts := strings.Split(name, "_")
	for i := range parts {
		if parts[i] == "" {
			continue
		}
		parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
	}
	return strings.Join(parts, "")
}

func writeRefusalGoFold(name string) string {
	return strings.ToLower(strings.ReplaceAll(name, "_", ""))
}
