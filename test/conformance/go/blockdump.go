// THE BLOCK-DUMP SURFACE (docs/SPEC-TABLES.md §19.2).
//
// The `block` surface says only that an image OPENS, which a reader passes by
// checking the prologue and stopping. This is the value-for-value read, so two
// implementations' reads of the same bytes are byte-compared — and it is
// produced from §8's DESCRIPTORS and nothing else, no generated row struct,
// because that is the claim §19.2 makes for them.
package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"unsafe"

	"blockdemo"
)

func dumpJoin(prefix, name string) string {
	if prefix == "" {
		return name
	}
	return prefix + "." + name
}

func dumpSlot(path string, slot int32) string {
	return path + "[" + strconv.Itoa(int(slot)) + "]"
}

// dumpScalar spells one value. A FLOAT is its IEEE-754 BIT PATTERN: a block row
// is a byte-identical projection, so its bits are the fact, and a decimal
// spelling would be a rounding rule two languages have to agree on for no gain.
func dumpScalar(out *strings.Builder, at unsafe.Pointer, kind uint8, width uint32) {
	switch kind {
	case 1: // bool
		if *(*byte)(at) != 0 {
			out.WriteString("true")
		} else {
			out.WriteString("false")
		}
	case 10: // float32: the bit pattern, in this build's own order
		fmt.Fprintf(out, "0x%08x", *(*uint32)(at))
	case 11: // float64
		fmt.Fprintf(out, "0x%016x", *(*uint64)(at))
	case 2, 3, 4, 5: // the SIGNED integers
		var v int64
		switch width {
		case 1:
			v = int64(*(*int8)(at))
		case 2:
			v = int64(*(*int16)(at))
		case 4:
			v = int64(*(*int32)(at))
		default:
			v = *(*int64)(at)
		}
		out.WriteString(strconv.FormatInt(v, 10))
	default: // an enum's ordinal, a flags mask, and every unsigned integer
		var v uint64
		switch width {
		case 1:
			v = uint64(*(*uint8)(at))
		case 2:
			v = uint64(*(*uint16)(at))
		case 4:
			v = uint64(*(*uint32)(at))
		default:
			v = *(*uint64)(at)
		}
		out.WriteString(strconv.FormatUint(v, 10))
	}
}

// dumpRecord writes one record's leaves, at two spaces, in descriptor order.
// `storage` is the record's own base. Out-of-line arrays are the caller's
// business: they are a section of their own, not a leaf.
func dumpRecord(out *strings.Builder, storage unsafe.Pointer, info *blockdemo.TableBlockInfo, path string) error {
	if info == nil {
		return fmt.Errorf("a descriptor names no record")
	}
	for i := range info.Fields {
		f := &info.Fields[i]
		if f.OutOfLine {
			continue
		}
		name := dumpJoin(path, f.Name)

		if f.Counted {
			// a string or a `bytes`: the used length lives at CountOffset
			used := *(*int32)(unsafe.Add(storage, uintptr(f.CountOffset)))
			if used < 0 || used > f.ArrayBound {
				return fmt.Errorf("%s.%s carries a used length of %d, outside [ 0, %d ]",
					info.Name, f.Name, used, f.ArrayBound)
			}
			out.WriteString("  " + name + " = ")
			out.WriteString(cookText(unsafe.Add(storage, uintptr(f.Offset)), used))
			out.WriteString("\n")
		} else {
			slots := int32(1)
			if f.IsArray {
				slots = f.ArrayBound
			}
			for s := int32(0); s < slots; s++ {
				at := name
				if f.IsArray {
					at = dumpSlot(name, s)
				}
				value := unsafe.Add(storage, uintptr(f.Offset)+uintptr(s)*uintptr(f.ElemSize))
				if f.Element != nil {
					if err := dumpRecord(out, value, f.Element(), at); err != nil {
						return err
					}
				} else {
					out.WriteString("  " + at + " = ")
					dumpScalar(out, value, f.Kind, f.ElemSize)
					out.WriteString("\n")
				}
			}
		}

		if f.Optional {
			out.WriteString("  " + name + "#present = ")
			if *(*byte)(unsafe.Add(storage, uintptr(f.PresentOffset))) != 0 {
				out.WriteString("true\n")
			} else {
				out.WriteString("false\n")
			}
		}
	}
	return nil
}

// dumpBlock is the whole dump of one opened block: the projection's own fields,
// then every out-of-line array in declaration order, row by row.
func dumpBlock(out *strings.Builder, base unsafe.Pointer, info *blockdemo.TableBlockInfo) error {
	fmt.Fprintf(out, "projection %s @0\n", info.Name)
	if err := dumpRecord(out, base, info, ""); err != nil {
		return err
	}
	for i := range info.Fields {
		f := &info.Fields[i]
		if !f.OutOfLine {
			continue
		}
		offsetOf := *(*uint64)(unsafe.Add(base, uintptr(f.OffsetOfOffset)))
		count := *(*uint32)(unsafe.Add(base, uintptr(f.CountOffset)))
		stride := *(*uint32)(unsafe.Add(base, uintptr(f.StrideOffset)))
		row := f.Element()
		if row == nil {
			return fmt.Errorf("%s names no element", f.Name)
		}
		fmt.Fprintf(out, "array %s %s @%d count=%d stride=%d\n", f.Name, row.Name, offsetOf, count, stride)
		for r := uint32(0); r < count; r++ {
			at := offsetOf + uint64(r)*uint64(stride)
			fmt.Fprintf(out, "row %d @%d\n", r, at)
			if err := dumpRecord(out, unsafe.Add(base, uintptr(at)), row, ""); err != nil {
				return err
			}
		}
	}
	return nil
}

func blockDump(name string, data []byte) (string, error) {
	base, bytes, keep := aligned(data, -1)
	var out strings.Builder
	var err error
	switch {
	case strings.HasPrefix(name, "block_render"):
		var block blockdemo.RenderFrameBlock
		if !blockdemo.RenderFrameBlockOpen(&block, base, bytes) {
			return "", fmt.Errorf("%s does not open", name)
		}
		err = dumpBlock(&out, block.Base, block.Type())
	case strings.HasPrefix(name, "block_padded"):
		var block blockdemo.PaddedFrameBlock
		if !blockdemo.PaddedFrameBlockOpen(&block, base, bytes) {
			return "", fmt.Errorf("%s does not open", name)
		}
		err = dumpBlock(&out, block.Base, block.Type())
	default:
		return "", fmt.Errorf("no block named %s", name)
	}
	keepAlive(keep)
	if err != nil {
		return "", err
	}
	return out.String(), nil
}

func surfaceBlockDump(lines []line, out string) error {
	for _, f := range lines {
		if f[0] != "block" {
			continue
		}
		data, err := os.ReadFile(f[3])
		if err != nil {
			return err
		}
		text, err := blockDump(f[1], data)
		if err != nil {
			return err
		}
		if err := spill(out, f[1], []byte(text)); err != nil {
			return err
		}
	}
	return nil
}
