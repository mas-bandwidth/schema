//go:build !writebounds_only

package main

import (
	"fmt"
	"os"
	"tblrt1"
	"tblvarevo1"
	"unsafe"
)

// THE RETAIN-REFUSAL SURFACE (test/conformance/README.md): one store through
// LoadRetain of the first wire, SaveRetain, LoadRetain of the refused wire
// into the same store, and SaveRetain of the first value again. The answer is
// the two verdicts and what the second save did; the saves ride in
// `<case>.before` and `<case>.wire`.
type retainSequence struct {
	measure func([]byte) int64
	load    func(region, wire []byte) (root unsafe.Pointer, refused bool)
	save    func(root unsafe.Pointer) ([]byte, error)
}

func alignedRegion(n int64) []byte {
	raw := make([]byte, n+63)
	off := (-uintptr(unsafe.Pointer(&raw[0]))) & 63
	return raw[off : off+uintptr(n)]
}

func retainSequenceFor(unit, root string) *retainSequence {
	switch {
	case unit == "tblrt1" && root == "Node":
		store := &tblrt1.TableRetain{Bytes: make([]byte, 1<<20), Ids: make([]tblrt1.TableRetainID, 1<<17)}
		return &retainSequence{
			measure: tblrt1.NodeLoadMeasure,
			load: func(region, wire []byte) (unsafe.Pointer, bool) {
				var report tblrt1.TableReport
				r := tblrt1.NodeLoadRetain(region, wire, store, &report)
				return unsafe.Pointer(r), r == nil || report.Refused()
			},
			save: func(p unsafe.Pointer) ([]byte, error) {
				r := (*tblrt1.Node)(p)
				n := tblrt1.NodeMeasureRetain(r, store)
				if n < 0 {
					return nil, nil
				}
				out := make([]byte, n)
				var report tblrt1.TableReport
				if got := tblrt1.NodeSaveRetain(r, store, out, &report); got != n {
					if got < 0 {
						return nil, nil
					}
					return nil, fmt.Errorf("SaveRetain wrote %d bytes and MeasureRetain said %d", got, n)
				}
				return out, nil
			},
		}
	case unit == "tblvarevo1" && root == "EvoDoc":
		store := &tblvarevo1.TableRetain{Bytes: make([]byte, 1<<20), Ids: make([]tblvarevo1.TableRetainID, 1<<17)}
		return &retainSequence{
			measure: tblvarevo1.EvoDocLoadMeasure,
			load: func(region, wire []byte) (unsafe.Pointer, bool) {
				var report tblvarevo1.TableReport
				r := tblvarevo1.EvoDocLoadRetain(region, wire, store, &report)
				return unsafe.Pointer(r), r == nil || report.Refused()
			},
			save: func(p unsafe.Pointer) ([]byte, error) {
				r := (*tblvarevo1.EvoDoc)(p)
				n := tblvarevo1.EvoDocMeasureRetain(r, store)
				if n < 0 {
					return nil, nil
				}
				out := make([]byte, n)
				var report tblvarevo1.TableReport
				if got := tblvarevo1.EvoDocSaveRetain(r, store, out, &report); got != n {
					if got < 0 {
						return nil, nil
					}
					return nil, fmt.Errorf("SaveRetain wrote %d bytes and MeasureRetain said %d", got, n)
				}
				return out, nil
			},
		}
	}
	return nil
}

func surfaceRetainRefusal(lines []line, out string) error {
	for _, f := range lines {
		if f[0] != "retain-refusal" {
			continue
		}
		name := f[1]
		seq := retainSequenceFor(f[2], f[3])
		if seq == nil {
			if err := spillAbsent(out, name); err != nil {
				return err
			}
			continue
		}
		first, err := os.ReadFile(f[4])
		if err != nil {
			return err
		}
		refusedWire, err := os.ReadFile(f[5])
		if err != nil {
			return err
		}
		n := seq.measure(first)
		if n < 0 {
			if err := spill(out, name, []byte("refused\n")); err != nil {
				return err
			}
			continue
		}
		root, refused := seq.load(alignedRegion(n), first)
		if refused {
			if err := spill(out, name, []byte("refused\n")); err != nil {
				return err
			}
			continue
		}
		before, err := seq.save(root)
		if err != nil {
			return fmt.Errorf("%s: first save: %w", name, err)
		}
		if before == nil {
			return fmt.Errorf("%s: the first save through the store refused", name)
		}
		if err := spill(out, name+".before", before); err != nil {
			return err
		}
		// the refused read gets a region of its own, so the first value
		// stays whole; where LoadMeasure refuses the wire, the first's size
		m := seq.measure(refusedWire)
		if m < 0 {
			m = n
		}
		_, secondRefused := seq.load(alignedRegion(m), refusedWire)
		answer := "read\nrefused\n"
		if !secondRefused {
			answer = "read\nread\n"
		}
		after, err := seq.save(root)
		if err != nil {
			return fmt.Errorf("%s: second save: %w", name, err)
		}
		if after == nil {
			answer += "save-refused\n"
		} else {
			answer += "saved\n"
			if err := spill(out, name+".wire", after); err != nil {
				return err
			}
		}
		if err := spill(out, name, []byte(answer)); err != nil {
			return err
		}
	}
	return nil
}
