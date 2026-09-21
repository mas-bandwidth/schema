package main

import (
	"testing"

	"tblp3"
)

func TestRowP3(t *testing.T) {
	// Build a valid Chain with known values, then save to fixed form.
	// The hostile bytes sweep flips each bit and verifies the reader answers
	// one of three ways: a named refusal, a malformed read, or a read that
	// lands values (docs/FIXED-FORM-ALGORITHM.md §7 item 5).
	values := []tblp3.Chain{
		{
			Name:       [16]byte{'h', 'e', 'l', 'l', 'o'},
			NameLength: 5,
			Link: tblp3.Link{
				Value:     42,
				Tag:       [8]byte{'t', 'a', 'g'},
				TagLength: 3,
			},
			LinkPresent: true,
		},
	}
	buf := make([]byte, tblp3.ChainFixedMeasure(1))
	n := tblp3.ChainFixedSave(values, buf)
	if n < 0 {
		t.Fatal("ChainFixedSave failed")
	}
	data := buf[:n]
	plan := make([]tblp3.TableFixedEntry, 64)
	var scratch [1]tblp3.Chain

	// Sanity: the valid file loads cleanly and returns exactly one record.
	var sanityReport tblp3.TableReport
	scratch[0] = tblp3.Chain{}
	sanityN := tblp3.ChainFixedLoad(scratch[:], data, plan, &sanityReport)
	if sanityN != 1 || sanityReport.Malformed || sanityReport.Verdict != tblp3.TableOpenOk {
		t.Fatalf("valid file: n=%d malformed=%v verdict=%v", sanityN, sanityReport.Malformed, sanityReport.Verdict)
	}
	if !scratch[0].LinkPresent || scratch[0].Link.Value != 42 {
		t.Fatalf("valid file: wrong values: LinkPresent=%v Value=%d", scratch[0].LinkPresent, scratch[0].Link.Value)
	}

	for bi := range data {
		for bit := 0; bit < 8; bit++ {
			mutated := make([]byte, len(data))
			copy(mutated, data)
			mutated[bi] ^= 1 << bit

			var report tblp3.TableReport
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("byte %d bit %d: panic: %v", bi, bit, r)
					}
				}()
				scratch[0] = tblp3.Chain{}
				_ = tblp3.ChainFixedLoad(scratch[:], mutated, plan, &report)
			}()

			switch {
			case report.Verdict == tblp3.TableOpenRefused:
			case report.Malformed:
			case report.Verdict == tblp3.TableOpenOk && !report.Malformed:
			default:
				t.Errorf("byte %d bit %d: unexpected outcome verdict=%v malformed=%v",
					bi, bit, report.Verdict, report.Malformed)
			}
		}
	}
}
