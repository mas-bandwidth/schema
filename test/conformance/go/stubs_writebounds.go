//go:build writebounds_only

package main

import (
	"fmt"
	"reflect"
)

var codecTable []codec

var writeRefusalGoPointees = map[reflect.Type]map[string]reflect.Type{}

func isBlobSharedErr(error) bool { return false }

func surfaces() []string {
	return []string{"wire", "json-read", "json-write", "write-refusal"}
}

type messageCodec struct {
	saveOne func(any) int64
}

func findMessageCodec(string, string) *messageCodec { return nil }
func writeRefusalBlock(string, any) (bool, error) {
	return false, fmt.Errorf("write-refusal-block absent in writebounds_only build")
}
func wireFuzz(bool) error     { return fmt.Errorf("wire-fuzz absent in writebounds_only build") }
func foreign(b []byte) []byte { return b }
func openBlock(string, []byte, int64) (bool, error) {
	return false, fmt.Errorf("block absent in writebounds_only build")
}
func openBlockForgedReason(string, []byte, int64, int, bool) (error, error) {
	return nil, fmt.Errorf("forgery absent in writebounds_only build")
}
func openCookForgedReason(string, []byte, int64, int, bool) (error, error) {
	return nil, fmt.Errorf("cook-forgery absent in writebounds_only build")
}
func surfaceRetain([]line, string) error {
	return fmt.Errorf("retain absent in writebounds_only build")
}
func surfaceRetainSave([]line, string) error {
	return fmt.Errorf("retain-save absent in writebounds_only build")
}
func surfaceMessage([]line, string) error {
	return fmt.Errorf("message absent in writebounds_only build")
}
func surfaceMessagePrior([]line, string) error {
	return fmt.Errorf("message-prior absent in writebounds_only build")
}
func surfaceCookWrite([]line, string) error {
	return fmt.Errorf("cook-write absent in writebounds_only build")
}
func surfaceCook([]line, string) error { return fmt.Errorf("cook absent in writebounds_only build") }
func surfaceCookForeign([]line, string) error {
	return fmt.Errorf("cook-foreign absent in writebounds_only build")
}
func surfaceBlockDump([]line, string) error {
	return fmt.Errorf("block-dump absent in writebounds_only build")
}
func surfaceAnnounce([]line, string) error {
	return fmt.Errorf("announce absent in writebounds_only build")
}
func surfaceReadInto([]line, string) error {
	return fmt.Errorf("read-into absent in writebounds_only build")
}
func surfaceJSONWriteInto([]line, string) error {
	return fmt.Errorf("json-write-into absent in writebounds_only build")
}
