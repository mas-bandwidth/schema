package main

import (
	"bytes"
	"fmt"
	"os"
	"strconv"
	"tblrt1"
	"unsafe"
)

func surfaceRetain(lines []line, out string) error     { return retainSurface(lines, out, false) }
func surfaceRetainSave(lines []line, out string) error { return retainSurface(lines, out, true) }
func retainSurface(lines []line, out string, saves bool) error {
	for _, f := range lines {
		if f[0] != "retain" && f[0] != "retain-message" {
			continue
		}
		if saves && f[len(f)-1] == "-" {
			continue
		}
		if f[0] == "retain-message" {
			if err := retainMessage(lines, f, out, saves); err != nil {
				return err
			}
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
		store := tblrt1.TableRetain{Bytes: make([]byte, capacity), Ids: make([]tblrt1.TableRetainId, idCapacity)}
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
		if root == nil || report.Malformed {
			return fmt.Errorf("%s load failed: %+v", f[1], report)
		}
		retained, lost, unknown := report.Retained, report.RetainLost, report.Unknown
		n = tblrt1.NodeMeasureRetain(root, &store)
		if n < 0 {
			return fmt.Errorf("%s retaining measure refused", f[1])
		}
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
			wireOut = []byte(fmt.Sprintf("%d,%d,%d %d\n", retained, lost, unknown, saveLost))
		}
		if err := spill(out, f[1], wireOut); err != nil {
			return err
		}
	}
	return nil
}

func retainMessage(lines []line, f line, out string, saves bool) error {
	if f[3] != "tblrt1" || f[4] != "Node" {
		return spillAbsent(out, f[1])
	}
	var announcement []byte
	var err error
	for _, c := range lines {
		if c[0] == "connection" && c[1] == f[2] {
			announcement, err = os.ReadFile(c[4])
			if err != nil {
				return err
			}
			break
		}
	}
	var vocabulary tblrt1.TableVocabulary
	vocabulary.Init(make([]tblrt1.TableMessageEntry, 4096))
	var report tblrt1.TableReport
	if !tblrt1.AnnounceRead(&vocabulary, announcement, &report) {
		return fmt.Errorf("%s announcement: %+v", f[1], report)
	}
	wire, err := os.ReadFile(f[5])
	if err != nil {
		return err
	}
	n := tblrt1.NodeLoadMessagesMeasure(&vocabulary, wire)
	if n < 0 {
		return fmt.Errorf("%s measure refused", f[1])
	}
	raw := make([]byte, n+63)
	off := (-uintptr(unsafe.Pointer(&raw[0]))) & 63
	region := raw[off : off+uintptr(n)]
	count := int(wire[1]) + 1
	values := make([]*tblrt1.Node, count)
	stores := make([]tblrt1.TableRetain, count)
	idCapacity := 1 << 17
	if f[7] != "full" {
		idCapacity, err = strconv.Atoi(f[7])
		if err != nil {
			return err
		}
	}
	for i := range stores {
		stores[i] = tblrt1.TableRetain{Bytes: make([]byte, 1<<20), Ids: make([]tblrt1.TableRetainId, idCapacity)}
	}
	if f[6] == "short" {
		if _, ok := tblrt1.NodeLoadRetainMessages(values, region, &vocabulary, wire, stores, &report); !ok {
			return fmt.Errorf("%s capacity probe", f[1])
		}
		for i := range stores {
			stores[i].Bytes = stores[i].Bytes[:max(0, stores[i].Used-1)]
		}
	}
	report = tblrt1.TableReport{}
	got, ok := tblrt1.NodeLoadRetainMessages(values, region, &vocabulary, wire, stores, &report)
	if !ok || got != int64(count) || report.Malformed {
		return fmt.Errorf("%s retaining message load: %+v", f[1], report)
	}
	retained, lost, unknown := report.Retained, report.RetainLost, report.Unknown
	var combined []byte
	for i, v := range values {
		size := tblrt1.NodeMeasureRetain(v, &stores[i])
		if size < 0 {
			return fmt.Errorf("%s measure retain", f[1])
		}
		buffer := make([]byte, size)
		if tblrt1.NodeSaveRetain(v, &stores[i], buffer, &report) != size {
			return fmt.Errorf("%s save retain", f[1])
		}
		combined = append(combined, buffer...)
	}
	if !saves {
		combined = []byte(fmt.Sprintf("%d,%d,%d %d\n", retained, lost, unknown, report.RetainLost-lost))
	}
	return spill(out, f[1], combined)
}
