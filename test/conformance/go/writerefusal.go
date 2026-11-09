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
	"fmt"
	"math"
	"math/big"
	"os"
	"reflect"
	"regexp"
	"strconv"
	"strings"
)

var writeRefusalGoSegment = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)(?:\[([0-9]+)\])?$`)

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
		read := c.load
		if c.variableLoad != nil {
			read = c.variableLoad
		}
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
			return fmt.Errorf("%s: %w", name, err)
		}
		answer := "wrote"
		switch surface {
		case "write-refusal":
			measure := c.measure
			if c.variableMeasure != nil {
				measure = c.variableMeasure
			}
			save := c.save
			if c.variableSave != nil {
				save = c.variableSave
			}
			if n := measure(value); n < 0 || save(value, make([]byte, n)) != n {
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
			fv = fv.Index(index)
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
		fv = fv.Index(index)
	}
	return writeRefusalGoSet(cur, fv, name, kind, text)
}

func writeRefusalGoSet(owner, fv reflect.Value, name, kind, text string) error {
	switch kind {
	case "count":
		n, err := strconv.Atoi(text)
		if err != nil {
			return err
		}
		companion, ok := writeRefusalGoField(owner, name+"Count")
		if !ok {
			return fmt.Errorf("%s: no count beside %s", name, owner.Type())
		}
		companion.SetInt(int64(n))
		return nil
	case "length":
		n, err := strconv.Atoi(text)
		if err != nil {
			return err
		}
		writeRefusalGoFill(fv, n, 'a')
		companion, ok := writeRefusalGoField(owner, name+"Length")
		if !ok {
			return fmt.Errorf("%s: no length beside %s", name, owner.Type())
		}
		companion.SetInt(int64(n))
		return nil
	case "bytes":
		b, err := hex.DecodeString(text)
		if err != nil {
			return err
		}
		writeRefusalGoBytes(fv, b)
		companion, ok := writeRefusalGoField(owner, name+"Length")
		if !ok {
			return fmt.Errorf("%s: no length beside %s", name, owner.Type())
		}
		companion.SetInt(int64(len(b)))
		return nil
	case "units":
		b, err := hex.DecodeString(text)
		if err != nil || len(b)%2 != 0 {
			return fmt.Errorf("%s: units are four hex digits each", name)
		}
		units := make([]uint16, len(b)/2)
		for i := range units {
			units[i] = uint16(b[2*i])<<8 | uint16(b[2*i+1])
		}
		writeRefusalGoUnits(fv, units)
		companion, ok := writeRefusalGoField(owner, name+"Length")
		if !ok {
			return fmt.Errorf("%s: no length beside %s", name, owner.Type())
		}
		companion.SetInt(int64(len(units)))
		return nil
	case "int":
		n, ok := new(big.Int).SetString(text, 10)
		if !ok {
			return fmt.Errorf("%s: %q is not an integer", name, text)
		}
		switch fv.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			fv.SetInt(n.Int64())
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			fv.SetUint(n.Uint64())
		default:
			return fmt.Errorf("%s: %s is not an integer", name, fv.Type())
		}
		return nil
	case "ordinal":
		n, err := strconv.ParseUint(text, 10, 64)
		if err != nil {
			return err
		}
		fv.SetUint(n)
		return nil
	case "tag":
		tag, err := strconv.ParseUint(text, 10, 64)
		if err != nil {
			return err
		}
		typ, ok := writeRefusalGoField(fv, "Type")
		if !ok {
			return fmt.Errorf("%s: %s carries no union tag", name, fv.Type())
		}
		typ.SetUint(tag)
		if payload, ok := writeRefusalGoField(fv, "Payload"); ok {
			payload.Set(reflect.Zero(payload.Type()))
		}
		return nil
	case "float":
		v, err := writeRefusalGoFloat(text)
		if err != nil {
			return err
		}
		switch fv.Kind() {
		case reflect.Float32, reflect.Float64:
			fv.SetFloat(v)
		default:
			return fmt.Errorf("%s: %s is not a float", name, fv.Type())
		}
		return nil
	}
	return fmt.Errorf("%s: unknown kind %q", name, kind)
}

func writeRefusalGoFill(fv reflect.Value, n int, unit byte) {
	for i := 0; i < n && i < fv.Len(); i++ {
		switch fv.Index(i).Kind() {
		case reflect.Uint8:
			fv.Index(i).SetUint(uint64(unit))
		case reflect.Uint16:
			fv.Index(i).SetUint(uint64(unit))
		}
	}
}

func writeRefusalGoBytes(fv reflect.Value, b []byte) {
	for i := 0; i < len(b) && i < fv.Len(); i++ {
		fv.Index(i).SetUint(uint64(b[i]))
	}
}

func writeRefusalGoUnits(fv reflect.Value, units []uint16) {
	for i := 0; i < len(units) && i < fv.Len(); i++ {
		fv.Index(i).SetUint(uint64(units[i]))
	}
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
