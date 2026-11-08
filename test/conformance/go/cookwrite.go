package main

import (
	"bytes"
	"fmt"
	"os"
)

func surfaceCookWrite(lines []line, out string) error {
	for _, f := range lines {
		if f[0] != "cook-write" {
			continue
		}
		var instance line
		for _, candidate := range lines {
			if candidate[0] == "instance" && candidate[1] == f[1] {
				instance = candidate
				break
			}
		}
		if instance == nil {
			return fmt.Errorf("missing instance %s", f[1])
		}
		c := findCodec(instance[2], instance[3])
		if c == nil || c.cook == nil {
			if err := spillAbsent(out, f[1]); err != nil {
				return err
			}
			if err := spillAbsent(out, f[1]+"-be"); err != nil {
				return err
			}
			continue
		}
		// THE CONSTRUCTION RECIPE (test/conformance/harness/cookinput.go): the
		// value is read from the line's text input, or from the instance's wire
		// where the line names none, and a `share` pair then points the alias
		// slot at the source slot's blob. The cook accepts a shared blob in
		// memory (§7.6) though neither the wire nor the text can spell one.
		value := c.fresh()
		var r report
		if len(f) > 2 && f[2] != "-" {
			text, err := os.ReadFile(f[2])
			if err != nil {
				return err
			}
			if c.fromJson == nil || !c.fromJson(value, text, &r) || r.refused {
				return fmt.Errorf("%s: cook input text did not read", f[1])
			}
		} else {
			wire, err := os.ReadFile(instance[4])
			if err != nil {
				return err
			}
			if !c.load(value, wire, &r) || r.refused {
				return fmt.Errorf("%s: cook input did not load", f[1])
			}
		}
		if len(f) > 4 && f[3] != "-" {
			if c.share == nil || !c.share(value, f[3], f[4]) {
				return fmt.Errorf("%s: the share recipe %s:%s did not apply", f[1], f[3], f[4])
			}
		}
		size := c.cookMeasure(value)
		if size < 1 {
			return fmt.Errorf("%s: cook measure refused", f[1])
		}
		for _, big := range []bool{false, true} {
			name := f[1]
			if big {
				name += "-be"
			}
			buffer := bytes.Repeat([]byte{0xa5}, int(size))
			if c.cook(value, buffer[:len(buffer)-1], big) || !bytes.Equal(buffer, bytes.Repeat([]byte{0xa5}, len(buffer))) {
				return fmt.Errorf("%s: short capacity wrote bytes", name)
			}
			if !c.cook(value, buffer, big) {
				return fmt.Errorf("%s: cook write refused", name)
			}
			if err := spill(out, name, buffer); err != nil {
				return err
			}
		}
	}
	return nil
}
