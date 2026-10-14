package main

import (
	"tblm1"
	"testing"
)

func TestRowW3(t *testing.T) {
	var value tblm1.Msg
	tblm1.MsgReset(&value)
	value.Seq = 1
	value.Body.Type = tblm1.BodyTypeQuit
	value.Body.Quit.Code = 7

	buf := make([]byte, 4096)
	n := tblm1.MsgFixedSave([]tblm1.Msg{value}, buf)
	if n < 0 {
		t.Fatal("MsgFixedSave refused")
	}
	buf = buf[:n]

	bodyStart := int(tblm1.TableFixedHeaderBytes + 4 + len(tblm1.MsgFixedLayout))
	body := buf[bodyStart+8:] // skip record hash (8 bytes)

	if int32(body[0])|int32(body[1])<<8|int32(body[2])<<16|int32(body[3])<<24 != 1 {
		t.Fatalf("seq: got %x %x %x %x, want 1 little-endian", body[0], body[1], body[2], body[3])
	}
	if body[4] != 3 {
		t.Fatalf("tag: got %d, want 3 (BodyTypeQuit)", body[4])
	}
	code := int32(body[5]) | int32(body[6])<<8 | int32(body[7])<<16 | int32(body[8])<<24
	if code != 7 {
		t.Fatalf("code: got %d, want 7", code)
	}
	for i := 9; i < int(tblm1.MsgFixedBodyBytes); i++ {
		if body[i] != 0 {
			t.Fatalf("byte %d: got %d, want 0 (zero behind narrower arm)", i, body[i])
		}
	}
}
