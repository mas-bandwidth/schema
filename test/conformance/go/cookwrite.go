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
		wire, err := os.ReadFile(instance[4])
		if err != nil {
			return err
		}
		value := c.fresh()
		var r report
		if !c.load(value, wire, &r) || r.malformed || r.refused {
			return fmt.Errorf("%s: cook input did not load", f[1])
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
