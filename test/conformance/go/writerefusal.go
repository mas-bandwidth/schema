package main

import (
	"errors"
	"fmt"
	"math"
	"os"
	"strconv"

	"backenddemo"
	"tabledemo"
	widedemo "widedemo"
)

// ---------------------------------------------------------------------------
// the write-refusal surface (testdata/conformance/tables/FORMAT.md)
// ---------------------------------------------------------------------------
//
// Each case loads a wire as its root, sets ONE field to a value off its
// declaration, and saves through the variable-form Save the wire surface uses.
// `bytes` sets a narrow text's exact bytes and `units` a wide text's exact
// UTF-16 units, so storage a program built holding ill-formed text reaches the
// writer as it would from a caller.
// The Go writer refuses in every build: Save returns -1 and MeasureReason
// names invalid_value. The answer is `refused` or `wrote`, and
// `unrepresentable` only when the Go storage cannot hold the value at all.

// wrefInt parses an integer value; ok is false when it lies outside [lo, hi],
// the range of the Go storage it is bound for.
func wrefInt(text string, lo, hi int64) (int64, bool, error) {
	n, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return 0, false, fmt.Errorf("value %q is not an integer", text)
	}
	return n, n >= lo && n <= hi, nil
}

func wrefFloat(text string) (float64, error) {
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

// wrefMutate sets the one field a case names. It answers false for a value the
// Go storage cannot hold; a root, path or kind it does not know is an error,
// never a silent pass.
func wrefMutate(value any, path, kind, text string) (bool, error) {
	unknown := fmt.Errorf("no write-refusal mutation for %T %s %s", value, path, kind)
	switch v := value.(type) {
	case *tabledemo.RootConfig:
		weapon := &v.Weapons[0]
		switch path + " " + kind {
		case "weapons[0].penetration int":
			n, ok, err := wrefInt(text, math.MinInt32, math.MaxInt32)
			if err != nil || !ok {
				return false, err
			}
			weapon.Penetration = int32(n)
		case "weapons[0].channel int":
			n, ok, err := wrefInt(text, 0, math.MaxUint32)
			if err != nil || !ok {
				return false, err
			}
			weapon.Channel = uint32(n)
		case "weapons[0].damage float":
			f, err := wrefFloat(text)
			if err != nil {
				return false, err
			}
			weapon.Damage = float32(f)
		case "weapons[0].effect tag":
			n, ok, err := wrefInt(text, 0, math.MaxUint8)
			if err != nil || !ok {
				return false, err
			}
			weapon.Effect.Type = tabledemo.EffectType(n)
			// a lawful tag takes its arm at the arm's declared defaults
			switch weapon.Effect.Type {
			case tabledemo.EffectTypeBuff:
				tabledemo.BuffReset(&weapon.Effect.Buff().Value)
			case tabledemo.EffectTypeDebuff:
				tabledemo.DebuffReset(&weapon.Effect.Debuff().Value)
			}
		case "weapons count":
			n, ok, err := wrefInt(text, math.MinInt32, math.MaxInt32)
			if err != nil || !ok {
				return false, err
			}
			if v.WeaponsCount < 1 {
				return false, fmt.Errorf("weapons count: the wire holds no element 0 to copy")
			}
			// new elements are copies of element 0 as far as the storage
			// reaches; the count is set even past it, and the writer refuses
			// before it reads past the storage
			for i := int(v.WeaponsCount); i < int(min(n, int64(len(v.Weapons)))); i++ {
				v.Weapons[i] = v.Weapons[0]
			}
			v.WeaponsCount = int32(n)
		case "version_note length":
			n, ok, err := wrefInt(text, math.MinInt32, math.MaxInt32)
			if err != nil || !ok {
				return false, err
			}
			for i := range int(max(0, min(n, int64(len(v.VersionNote))))) {
				v.VersionNote[i] = 'a'
			}
			v.VersionNoteLength = int32(n)
		case "version_note bytes":
			return wrefSetText(v.VersionNote[:16], &v.VersionNoteLength, text)
		default:
			return false, unknown
		}
	case *widedemo.Caption:
		switch path + " " + kind {
		case "title units":
			units, err := wrefUnits(text)
			if err != nil {
				return false, err
			}
			if len(units) > len(v.Title) {
				return false, nil
			}
			clear(v.Title[:])
			copy(v.Title[:], units)
			v.TitleLength = int32(len(units))
		default:
			return false, unknown
		}
	case *backenddemo.StorePurchase:
		switch path + " " + kind {
		case "quantity int":
			n, ok, err := wrefInt(text, 0, math.MaxUint8)
			if err != nil || !ok {
				return false, err
			}
			v.Quantity = uint8(n)
		case "sku length":
			n, ok, err := wrefInt(text, math.MinInt32, math.MaxInt32)
			if err != nil || !ok {
				return false, err
			}
			for i := range int(max(0, min(n, int64(len(v.Sku))))) {
				v.Sku[i] = 'a'
			}
			v.SkuLength = int32(n)
		case "sku bytes":
			return wrefSetText(v.Sku[:], &v.SkuLength, text)
		case "currency ordinal":
			n, ok, err := wrefInt(text, 0, math.MaxUint8)
			if err != nil || !ok {
				return false, err
			}
			v.Currency = backenddemo.Currency(n)
		default:
			return false, unknown
		}
	case *backenddemo.MatchResult:
		switch path + " " + kind {
		case "players[0].score int":
			n, ok, err := wrefInt(text, math.MinInt32, math.MaxInt32)
			if err != nil || !ok {
				return false, err
			}
			v.Players[0].Score = int32(n)
		default:
			return false, unknown
		}
	case *tabledemo.LoadoutConfig:
		switch path + " " + kind {
		case "grade ordinal":
			n, ok, err := wrefInt(text, 0, math.MaxUint8)
			if err != nil || !ok {
				return false, err
			}
			v.Grade = tabledemo.Grade(n)
		default:
			return false, unknown
		}
	default:
		return false, unknown
	}
	return true, nil
}

// wrefMeasureReason is the root's typed MeasureReason, so the surface confirms
// that a refusal names invalid_value.
func wrefMeasureReason(value any) (int64, error) {
	switch v := value.(type) {
	case *tabledemo.RootConfig:
		return tabledemo.RootConfigMeasureReason(v)
	case *tabledemo.LoadoutConfig:
		return tabledemo.LoadoutConfigMeasureReason(v)
	case *widedemo.Caption:
		return widedemo.CaptionMeasureReason(v)
	}
	return 0, fmt.Errorf("no MeasureReason for %T", value)
}

func surfaceWriteRefusal(lines []line, out string) error {
	for _, f := range lines {
		if f[0] != "write-refusal" {
			continue
		}
		if len(f) < 8 {
			return fmt.Errorf("write-refusal line has %d fields, wants 8", len(f))
		}
		name, unit, root, wirePath, path, kind, text := f[1], f[2], f[3], f[4], f[5], f[6], f[7]
		c := findCodec(unit, root)
		if c == nil {
			return fmt.Errorf("%s: no codec for %s %s", name, unit, root)
		}
		wire, err := os.ReadFile(wirePath)
		if err != nil {
			return err
		}
		value := c.fresh()
		var rep report
		if !c.load(value, wire, &rep) {
			return fmt.Errorf("%s: load refused %s", name, wirePath)
		}
		held, err := wrefMutate(value, path, kind, text)
		if err != nil {
			return fmt.Errorf("%s: %v", name, err)
		}
		if !held {
			if err := spill(out, name, []byte("unrepresentable\n")); err != nil {
				return err
			}
			continue
		}
		size, reason := wrefMeasureReason(value)
		if size >= 0 && reason != nil {
			return fmt.Errorf("%s: %v", name, reason)
		}
		if size < 0 && !errors.Is(reason, tabledemo.TableRefuseInvalidValue) && !errors.Is(reason, widedemo.TableRefuseInvalidValue) {
			return fmt.Errorf("%s: measure refused with %v, wants invalid_value", name, reason)
		}
		// an ample buffer: the wire it came from plus room for every byte the
		// mutation could add
		buffer := make([]byte, len(wire)+64*1024)
		saved := c.save(value, buffer)
		if (saved < 0) != (size < 0) {
			return fmt.Errorf("%s: measure answered %d and save answered %d", name, size, saved)
		}
		answer := "wrote\n"
		if saved < 0 {
			answer = "refused\n"
		}
		if err := spill(out, name, []byte(answer)); err != nil {
			return err
		}
	}
	return nil
}
