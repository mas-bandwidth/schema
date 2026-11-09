package main

import (
	"bytes"
	"fmt"
	"os"
	"strconv"
	"tblrt1"
	"unsafe"
)

func init() {
	wrapRetainBody(&tblrt1.NodeTableInfo, 0x0a8f12cc5f9a0c03)
	wrapRetainBody(&tblrt1.LeafTableInfo, 0xe5316cbaa025f028)
}

// wrapRetainBody marks any node record reached by a kind-17 pointer nested
// inside an unknown field (such as a table, array, keyed slot, or union arm
// the retaining reader skips and excludes from retention) so the post-load
// graph walk sees that record as reachable (docs/SPEC-TABLES.md §4, §6.6).
func wrapRetainBody(info *tblrt1.TableTypeInfo, knownPtrID uint64) {
	orig := info.LoadBodyRetain
	info.LoadBodyRetain = func(r tblrt1.TableRetainReader, p unsafe.Pointer) bool {
		if !orig(r, p) || r.Report.Refused() {
			return false
		}
		markUnknownNodeRefs(tblrt1.TableReader{Buffer: r.Buffer, Ids: r.Ids, Nodes: r.Nodes, Report: r.Report}, knownPtrID)
		return true
	}
}

func markUnknownNodeRefs(r tblrt1.TableReader, knownPtrID uint64) {
	for {
		ref, ok := r.Leb()
		if !ok || ref == 0 {
			return
		}
		fieldID, ok := r.Resolve(ref)
		if !ok || !r.Has(1) {
			return
		}
		kind := r.Get8()
		if fieldID == 0xffffffffffffffff || (fieldID == knownPtrID && kind == 17) {
			if !r.Skip(kind) {
				return
			}
			continue
		}
		if !markUnknownValue(&r, kind) {
			return
		}
	}
}

func markUnknownValue(r *tblrt1.TableReader, kind uint8) bool {
	switch kind {
	case 17:
		index, ok := r.Leb()
		if !ok {
			return false
		}
		r.Nodes.Mark(index)
		return true
	case 13, 14, 16:
		sub, ok := r.Body()
		if !ok {
			return false
		}
		return markUnknownFramed(sub, kind)
	case 15:
		armRef, ok := r.Leb()
		if !ok {
			return false
		}
		if armRef == 0 {
			return true
		}
		if !r.Has(1) {
			return false
		}
		armKind := r.Get8()
		arm, ok := r.Body()
		if !ok {
			return false
		}
		return markUnknownFramed(arm, armKind)
	default:
		return r.Skip(kind)
	}
}

func markUnknownFramed(sub tblrt1.TableReader, kind uint8) bool {
	switch kind {
	case 13:
		markUnknownNodeRefs(sub, 0)
		return true
	case 14, 16:
		if len(sub.Buffer) < 2 {
			return false
		}
		elemKind := sub.Get8()
		count, ok := sub.Leb()
		if !ok {
			return false
		}
		for range count {
			if kind == 16 {
				if _, ok := sub.Leb(); !ok {
					return false
				}
				elem, ok := sub.Body()
				if !ok || !markUnknownFramed(elem, elemKind) {
					return false
				}
			} else if !markUnknownValue(&sub, elemKind) {
				return false
			}
		}
		return true
	default:
		return markUnknownValue(&sub, kind)
	}
}

func surfaceRetain(lines []line, out string) error     { return retainSurface(lines, out, false) }
func surfaceRetainSave(lines []line, out string) error { return retainSurface(lines, out, true) }
func retainSurface(lines []line, out string, saves bool) error {
	for _, f := range lines {
		if f[0] != "retain" {
			continue
		}
		if saves && f[len(f)-1] == "-" {
			continue
		}
		if f[2] != "tblrt1" || f[3] != "Node" {
			if err := spillAbsent(out, f[1]); err != nil {
				return err
			}
			continue
		}
		wire, err := os.ReadFile(f[4])
		if err != nil {
			return err
		}
		n := tblrt1.NodeLoadMeasure(wire)
		if n < 0 {
			return fmt.Errorf("%s measure refused", f[1])
		}
		raw := make([]byte, n+63)
		off := (-uintptr(unsafe.Pointer(&raw[0]))) & 63
		region := raw[off : off+uintptr(n)]
		capacity, idCapacity := 1<<20, 1<<17
		if f[6] != "full" {
			idCapacity, err = strconv.Atoi(f[6])
			if err != nil {
				return err
			}
		}
		store := tblrt1.TableRetain{Bytes: make([]byte, capacity), Ids: make([]tblrt1.TableRetainID, idCapacity)}
		var report tblrt1.TableReport
		if f[5] == "short" {
			if tblrt1.NodeLoadRetain(region, wire, &store, &report) == nil {
				return fmt.Errorf("%s capacity probe refused", f[1])
			}
			capacity = int(store.Used) - 1
			store.Bytes = store.Bytes[:max(0, capacity)]
			report = tblrt1.TableReport{}
		}
		root := tblrt1.NodeLoadRetain(region, wire, &store, &report)
		if root == nil || report.Refused() {
			// THE ONE READ'S VERDICT is the answer (test/conformance/README.md):
			// a refused retaining read owes the refusal alone, and no bytes
			if saves {
				continue
			}
			if err := spill(out, f[1], []byte("refused\n")); err != nil {
				return err
			}
			continue
		}
		n = tblrt1.NodeMeasureRetain(root, &store)
		if n < 0 {
			return fmt.Errorf("%s retaining measure refused", f[1])
		}
		lost := report.RetainLost
		wireOut := make([]byte, n)
		if tblrt1.NodeSaveRetain(root, &store, wireOut, &report) != n {
			return fmt.Errorf("%s retaining save refused", f[1])
		}
		saveLost := report.RetainLost - lost
		again := make([]byte, n)
		var second tblrt1.TableReport
		if tblrt1.NodeSaveRetain(root, &store, again, &second) != n || !bytes.Equal(again, wireOut) || second.RetainLost != saveLost {
			return fmt.Errorf("%s save is not repeatable", f[1])
		}
		if !saves {
			// the verdict, then the §16 text of the known value the read left:
			// retention keeps the unknown records for the save and never
			// changes the value a reader of this build sees
			size := tblrt1.NodeToJSONMeasure(root)
			if size < 0 {
				return fmt.Errorf("%s: ToJSONMeasure refused the value", f[1])
			}
			text := make([]byte, size)
			if tblrt1.NodeToJSON(root, text) != size {
				return fmt.Errorf("%s: ToJSON did not write ToJSONMeasure's answer", f[1])
			}
			wireOut = append([]byte("read\n"), text...)
		}
		if err := spill(out, f[1], wireOut); err != nil {
			return err
		}
	}
	return nil
}
