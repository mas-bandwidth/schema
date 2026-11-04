package main

import (
	"blobdemo"
	"bytes"
	"fmt"
	"os"
	"unsafe"
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
		value, err := cookInput(c, instance, f)
		if err != nil {
			return fmt.Errorf("%s: %w", f[1], err)
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

// The recipe is input, never an expected cook: a JSON file and optionally two
// schema field names to alias after reading. Deep graphs without text use wire.
func cookInput(c *codec, instance, recipe line) (any, error) {
	if len(recipe) != 5 {
		return nil, fmt.Errorf("invalid cook input recipe")
	}
	path := recipe[2]
	value := c.fresh()
	var rep report
	if path == "-" {
		wire, err := os.ReadFile(instance[4])
		if err != nil {
			return nil, err
		}
		if !c.load(value, wire, &rep) {
			return nil, fmt.Errorf("cook wire input refused")
		}
		return value, nil
	}
	text, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if recipe[3] != "-" || recipe[4] != "-" {
		if c.cookInput == nil {
			return nil, fmt.Errorf("no builder for cook alias recipe")
		}
		return c.cookInput(text, recipe[3], recipe[4])
	}
	if c.fromJson == nil || !c.fromJson(value, text, &rep) {
		return nil, fmt.Errorf("cook JSON input refused")
	}
	return value, nil
}

func withCookInput(c codec, input func([]byte, string, string) (any, error)) codec {
	c.cookInput = input
	return c
}

func cookCatalogInput(text []byte, source, alias string) (any, error) {
	var b blobdemo.CatalogBuilder
	if !b.Init() {
		return nil, fmt.Errorf("cook builder initialization failed")
	}
	defer b.Shutdown()
	var rep blobdemo.TableReport
	if !blobdemo.CatalogFromJSON(&b, text, &rep) {
		return nil, fmt.Errorf("cook JSON input refused")
	}
	root := b.GetRoot()
	var from, to *blobdemo.TableRef
	for _, field := range blobdemo.CatalogTableType().Fields {
		if !field.Pointer || field.TypeName != "bytes" || field.IsArray || field.Optional {
			continue
		}
		slot := (*blobdemo.TableRef)(unsafe.Add(unsafe.Pointer(root), field.Offset))
		if field.Name == source {
			from = slot
		}
		if field.Name == alias {
			to = slot
		}
	}
	if from == nil || to == nil || from == to || *from == 0 {
		return nil, fmt.Errorf("cook alias must name two blob slots with a present source")
	}
	*to = *from
	if !b.Lock() {
		return nil, fmt.Errorf("cook builder lock failed")
	}
	return &regionValue[blobdemo.Catalog]{root: b.AsConst(), storage: b.Region()}, nil
}
