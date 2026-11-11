package main

import (
	"fmt"
	"strings"
	"testing"
)

// emitPacketBody renders the packet body the generator writes for one unit.
func emitPacketBody(template string) string {
	return strings.NewReplacer(
		"{N}", "Unit",
		"{T}", "Root",
		"{F}", "u00_unit_Root",
		"{U}", "u00_unit",
	).Replace(template)
}

// packetLengthMultipliedIn32Bits reports whether the body multiplies the
// payload length by 8 without a 64-bit operand, which wraps for a payload
// larger than 256 MiB.
func packetLengthMultipliedIn32Bits(body string) bool {
	for i := 0; ; {
		j := strings.Index(body[i:], "d.Length")
		if j < 0 {
			return false
		}
		j += i
		i = j + len("d.Length")

		rest := strings.TrimLeft(body[j+len("d.Length"):], " \t")
		if !strings.HasPrefix(rest, "*") {
			continue
		}
		rest = strings.TrimLeft(rest[1:], " \t")
		if !strings.HasPrefix(rest, "8") {
			continue
		}
		after := rest[1:]
		if strings.HasPrefix(after, "L") || strings.HasPrefix(after, "l") {
			continue
		}
		if strings.HasSuffix(strings.TrimRight(body[:j], " \t"), "(long)") {
			continue
		}
		return true
	}
}

// checkPacketAcceptBuffer pins the accept path's writer buffer size: the
// payload term is 64-bit, and an array the runtime cannot hold is reported by
// name before it is allocated.
func checkPacketAcceptBuffer(body string) error {
	if packetLengthMultipliedIn32Bits(body) {
		return fmt.Errorf("packet body multiplies the payload length by 8 in 32-bit arithmetic; the writer buffer size must be computed in 64-bit arithmetic")
	}
	alloc := strings.Index(body, "new Serialize.WriteStream(new byte[")
	if alloc < 0 {
		return fmt.Errorf("packet body has no writer buffer allocation")
	}
	guard := strings.Index(body, "Array.MaxLength")
	if guard < 0 || guard > alloc {
		return fmt.Errorf("packet body has no array-size guard before the writer buffer allocation")
	}
	segment := body[guard:alloc]
	if !strings.Contains(segment, "too large for the runner") {
		return fmt.Errorf("packet body's array-size guard does not name the input as too large for the runner")
	}
	if !strings.Contains(segment, "return ") || !strings.Contains(segment, "\"error: ") {
		return fmt.Errorf("packet body's array-size guard does not return the named runner error")
	}
	return nil
}

func TestPacketAcceptBufferSize(t *testing.T) {
	t.Parallel()
	if err := checkPacketAcceptBuffer(emitPacketBody(packetBody)); err != nil {
		t.Fatal(err)
	}
}

func TestPacketAcceptBufferSizeNegativeControl(t *testing.T) {
	t.Parallel()
	const wide = "(long)d.Length * 8"
	sabotaged := strings.Replace(packetBody, wide, "d.Length * 8", 1)
	if sabotaged == packetBody {
		t.Fatalf("negative control did not apply: the packet body no longer carries %q", wide)
	}
	err := checkPacketAcceptBuffer(emitPacketBody(sabotaged))
	if err == nil {
		t.Fatal("negative control: the 32-bit writer buffer size was accepted")
	}
	if !strings.Contains(err.Error(), "32-bit") {
		t.Fatalf("negative control: the check went red without naming the 32-bit multiplication: %v", err)
	}
	if err := checkPacketAcceptBuffer(emitPacketBody(packetBody)); err != nil {
		t.Fatalf("negative control: the unmodified packet body is not green: %v", err)
	}
}
