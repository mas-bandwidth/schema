// THE COOK SURFACE's half of the driver (docs/SPEC-TABLES.md §7, §7.5).
//
// The canonical NODE DUMP: the walk this leg makes through its OWN derefs,
// written as text, so two implementations' walks are byte-compared rather than
// merely both succeeding. A record laid out one byte differently INSIDE a node
// moves no node offset and no directory entry, so this is the gate the
// attribution check cannot be.
//
// It is GENERIC over the cook descriptors, which is the whole point of them: a
// pointer slot is eight bytes holding the SIGNED SELF-RELATIVE delta of §6.3,
// so a deref is one add and needs no base pointer, and a delta of zero is null.
// A by-value nesting — a record inside a record, an element of a bounded or
// enum-keyed array — is not a node; it is storage inside one, and the walk
// descends through it to reach the pointer slots inside.
//
// The C++ and C# legs delegate this surface to a binary they already had. This
// leg does it in process, so the five cooks cost one exec rather than five.
package main

import (
	"encoding/binary"
	"fmt"
	"os"
	"strconv"
	"strings"
	"unsafe"

	"graphdemo"
)

// cookAlignment is the alignment the header NAMES, which is where a forged
// buffer has to be placed for the base-alignment check to mean anything. A
// forged word that is not an alignment at all puts the buffer at the format's
// own floor instead.
func cookAlignment(source []byte) int64 {
	if len(source) < 48 {
		return 8
	}
	a := int64(binary.NativeEndian.Uint64(source[40:]))
	if a < 1 || a > 64 || a&(a-1) != 0 {
		return 8
	}
	return a
}

// openCookForged opens one cooked file by root name over a forged placement:
// the buffer is exactly the extent the caller claims, its base `lead` bytes
// past an aligned address, or absent entirely.
func openCookForged(root string, data []byte, extent int64, lead int, nilBuffer bool) (bool, error) {
	base, bytes, keep := place(data, extent, lead, cookAlignment(data))
	if nilBuffer {
		base = nil
	}
	opened := false
	switch root {
	case "Scene":
		var c graphdemo.SceneCook
		opened = graphdemo.SceneOpen(&c, base, bytes)
	case "Depot":
		var c graphdemo.DepotCook
		opened = graphdemo.DepotOpen(&c, base, bytes)
	case "Album":
		var c graphdemo.AlbumCook
		opened = graphdemo.AlbumOpen(&c, base, bytes)
	case "TreeNode":
		var c graphdemo.TreeNodeCook
		opened = graphdemo.TreeNodeOpen(&c, base, bytes)
	case "ListNode":
		var c graphdemo.ListNodeCook
		opened = graphdemo.ListNodeOpen(&c, base, bytes)
	case "Settings":
		var c graphdemo.SettingsCook
		opened = graphdemo.SettingsOpen(&c, base, bytes)
	case "Meta":
		var c graphdemo.MetaCook
		opened = graphdemo.MetaOpen(&c, base, bytes)
	case "Layer":
		var c graphdemo.LayerCook
		opened = graphdemo.LayerOpen(&c, base, bytes)
	default:
		return false, fmt.Errorf("no cook root named %s", root)
	}
	keepAlive(keep)
	return opened, nil
}

// openCook opens one cooked file by root name and hands back its region, the
// root's descriptor, and the backing allocation the caller must keep live.
func openCook(root string, data []byte) (unsafe.Pointer, int64, *graphdemo.TableCookInfo, []byte, error) {
	base, _, keep := aligned(data, -1)
	length := int64(len(data))
	switch root {
	case "Scene":
		var c graphdemo.SceneCook
		if !graphdemo.SceneOpen(&c, base, length) {
			return nil, 0, nil, nil, fmt.Errorf("%s: the cook does not open", root)
		}
		return c.Region, c.RegionLength, c.Type(), keep, nil
	case "Depot":
		var c graphdemo.DepotCook
		if !graphdemo.DepotOpen(&c, base, length) {
			return nil, 0, nil, nil, fmt.Errorf("%s: the cook does not open", root)
		}
		return c.Region, c.RegionLength, c.Type(), keep, nil
	case "Album":
		var c graphdemo.AlbumCook
		if !graphdemo.AlbumOpen(&c, base, length) {
			return nil, 0, nil, nil, fmt.Errorf("%s: the cook does not open", root)
		}
		return c.Region, c.RegionLength, c.Type(), keep, nil
	case "TreeNode":
		var c graphdemo.TreeNodeCook
		if !graphdemo.TreeNodeOpen(&c, base, length) {
			return nil, 0, nil, nil, fmt.Errorf("%s: the cook does not open", root)
		}
		return c.Region, c.RegionLength, c.Type(), keep, nil
	case "ListNode":
		var c graphdemo.ListNodeCook
		if !graphdemo.ListNodeOpen(&c, base, length) {
			return nil, 0, nil, nil, fmt.Errorf("%s: the cook does not open", root)
		}
		return c.Region, c.RegionLength, c.Type(), keep, nil
	}
	return nil, 0, nil, nil, fmt.Errorf("no cook root named %s", root)
}

// reached is one visited node: a node is visited ONCE, and sharing and a
// back-reference are the same fact (§6.3).
type reached struct {
	offset uint64
	typ    *graphdemo.TableCookInfo
}

type cookWalk struct {
	region unsafe.Pointer
	length uint64
	nodes  []reached
	dump   strings.Builder
}

func (w *cookWalk) find(offset uint64) int {
	for i := range w.nodes {
		if w.nodes[i].offset == offset {
			return i
		}
	}
	return -1
}

func (w *cookWalk) node(offset uint64, typ *graphdemo.TableCookInfo, depth int) error {
	if depth > 4096 {
		return fmt.Errorf("the walk nested past any depth a region can hold — a cycle the deref did not close")
	}
	if at := w.find(offset); at >= 0 {
		if w.nodes[at].typ != typ {
			return fmt.Errorf("two references name the node at offset %d as two different tables: %s and %s",
				offset, w.nodes[at].typ.Name, typ.Name)
		}
		return nil
	}
	if offset > w.length || uint64(typ.Size) > w.length-offset {
		return fmt.Errorf("the node at offset %d (%s, size %d) does not fit inside the region's %d bytes",
			offset, typ.Name, typ.Size, w.length)
	}
	index := len(w.nodes)
	w.nodes = append(w.nodes, reached{offset: offset, typ: typ})
	fmt.Fprintf(&w.dump, "node %d %s @%d\n", index, typ.Name, offset)
	return w.storage(unsafe.Add(w.region, uintptr(offset)), typ, "", depth)
}

func (w *cookWalk) storage(storage unsafe.Pointer, typ *graphdemo.TableCookInfo, path string, depth int) error {
	for i := range typ.Fields {
		f := &typ.Fields[i]
		name := f.Name
		if path != "" {
			name = path + "." + f.Name
		}

		// every COUNT COMPANION, against its declared bound, and a NEGATIVE one
		// refuses too — an extent is never negative, and a walker handed one
		// indexes backwards out of the region (§7.4's pass two)
		used := int32(-1)
		if f.CountOffset >= 0 {
			used = *(*int32)(unsafe.Add(storage, uintptr(f.CountOffset)))
			if used < 0 || used > f.ArrayBound {
				return fmt.Errorf("%s.%s carries a count companion of %d, outside [ 0, %d ]",
					typ.Name, f.Name, used, f.ArrayBound)
			}
		}

		if f.IsPointer {
			slot := unsafe.Add(storage, uintptr(f.Offset))
			delta := *(*int64)(slot)
			if delta == 0 {
				// NULL IN A REGION IS A DELTA OF ZERO (§6.3)
				w.emit(name, "null")
				continue
			}
			target := unsafe.Add(slot, uintptr(delta))
			at := int64(uintptr(target)) - int64(uintptr(w.region))
			if at < 0 || uint64(at) >= w.length {
				return fmt.Errorf("%s.%s resolves outside the region — a delta of %d from a slot at %d",
					typ.Name, f.Name, delta, int64(uintptr(slot))-int64(uintptr(w.region)))
			}
			if f.Record == nil {
				return fmt.Errorf("%s.%s is a pointer whose descriptor names no record", typ.Name, f.Name)
			}
			w.emit(name, "-> @"+strconv.FormatUint(uint64(at), 10))
			if err := w.node(uint64(at), f.Record(), depth+1); err != nil {
				return err
			}
			continue
		}

		switch f.Storage {
		case graphdemo.TableCookStorageString, graphdemo.TableCookStorageBytes:
			w.emit(name, cookText(unsafe.Add(storage, uintptr(f.Offset)), used))
		case graphdemo.TableCookStorageRecord:
			// a nested record — by value, or every slot of an array of them. A
			// COUNTED array writes all N slots (§7.2), and a slot past the live
			// count holds the value-initialised element, whose pointer slots are
			// zero: walking all of them is what the check does too.
			slots := int32(1)
			if f.IsArray {
				slots = f.ArrayBound
			}
			for s := int32(0); s < slots; s++ {
				element := name
				if f.IsArray {
					element = name + "[" + strconv.Itoa(int(s)) + "]"
				}
				at := unsafe.Add(storage, uintptr(f.Offset)+uintptr(s)*uintptr(f.ElemSize))
				if err := w.storage(at, f.Record(), element, depth); err != nil {
					return err
				}
			}
		default:
			slots := int32(1)
			if f.IsArray {
				slots = f.ArrayBound
			}
			for s := int32(0); s < slots; s++ {
				element := name
				if f.IsArray {
					element = name + "[" + strconv.Itoa(int(s)) + "]"
				}
				at := unsafe.Add(storage, uintptr(f.Offset)+uintptr(s)*uintptr(f.ElemSize))
				value, err := cookScalar(at, f.Storage, f.ElemSize)
				if err != nil {
					return err
				}
				w.emit(element, value)
			}
		}

		if f.CountOffset >= 0 && f.Storage != graphdemo.TableCookStorageString && f.Storage != graphdemo.TableCookStorageBytes {
			w.emit(name+"#count", strconv.Itoa(int(used)))
		}
		if f.PresentOffset >= 0 {
			present := "false"
			if *(*byte)(unsafe.Add(storage, uintptr(f.PresentOffset))) != 0 {
				present = "true"
			}
			w.emit(name+"#present", present)
		}
	}
	return nil
}

func (w *cookWalk) emit(path, value string) {
	fmt.Fprintf(&w.dump, "  %s = %s\n", path, value)
}

// cookText is a string's or a `bytes`' USED bytes, without the zero tail past
// them (§7.2) — printed the same way on every leg, every byte outside
// printable ASCII escaped so the comparison stays a byte comparison of text.
func cookText(at unsafe.Pointer, used int32) string {
	if used < 0 {
		used = 0
	}
	var b strings.Builder
	b.WriteByte('"')
	for i := int32(0); i < used; i++ {
		c := *(*byte)(unsafe.Add(at, uintptr(i)))
		if c >= 0x20 && c < 0x7f && c != '"' && c != '\\' {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "\\x%02x", c)
		}
	}
	b.WriteByte('"')
	fmt.Fprintf(&b, " len=%d", used)
	return b.String()
}

func cookScalar(at unsafe.Pointer, storage graphdemo.TableCookStorage, width int32) (string, error) {
	switch storage {
	case graphdemo.TableCookStorageBool:
		if *(*byte)(at) != 0 {
			return "true", nil
		}
		return "false", nil
	case graphdemo.TableCookStorageFloat:
		// Nothing in the pointered corpus is a float, and a canonical
		// cross-language spelling of one is a decision this gate should not
		// make in passing. The day a float arrives, the gate says so rather
		// than drifting.
		return "", fmt.Errorf("the dump met a float, whose canonical cross-language spelling this gate does not fix")
	case graphdemo.TableCookStorageSigned:
		switch width {
		case 1:
			return strconv.FormatInt(int64(*(*int8)(at)), 10), nil
		case 2:
			return strconv.FormatInt(int64(*(*int16)(at)), 10), nil
		case 4:
			return strconv.FormatInt(int64(*(*int32)(at)), 10), nil
		default:
			return strconv.FormatInt(*(*int64)(at), 10), nil
		}
	default:
		switch width {
		case 1:
			return strconv.FormatUint(uint64(*(*uint8)(at)), 10), nil
		case 2:
			return strconv.FormatUint(uint64(*(*uint16)(at)), 10), nil
		case 4:
			return strconv.FormatUint(uint64(*(*uint32)(at)), 10), nil
		default:
			return strconv.FormatUint(*(*uint64)(at), 10), nil
		}
	}
}

func surfaceCook(lines []line, out string) error {
	for _, f := range lines {
		if f[0] != "cook" {
			continue
		}
		// cook <case> <unit> <root> <file>: the CASE names the answer and the
		// ROOT names the reader, because one root can have more than one
		// fixture — Scene and SceneValued are the same table read two ways.
		name, root, file := f[1], f[3], f[4]
		data, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		region, length, typ, keep, err := openCook(root, data)
		if err != nil {
			return err
		}
		w := &cookWalk{region: region, length: uint64(length)}
		if err := w.node(0, typ, 0); err != nil {
			return fmt.Errorf("%s: %w", root, err)
		}
		if err := spill(out, name, []byte(w.dump.String())); err != nil {
			return err
		}
		keepAlive(keep)
	}
	return nil
}
