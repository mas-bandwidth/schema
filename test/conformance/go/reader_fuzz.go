package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
)

type readerFuzzReport struct {
	Unknown      int32 `json:"unknown"`
	KindMismatch int32 `json:"kind_mismatch"`
	Widened      int32 `json:"widened"`
	Clamped      int32 `json:"clamped"`
	Duplicate    int32 `json:"duplicate"`
	Malformed    bool  `json:"malformed"`
	Refused      bool  `json:"refused"`
}

type readerFuzzReply struct {
	Loaded bool             `json:"loaded"`
	Report readerFuzzReport `json:"report"`
	Wire   []byte           `json:"wire,omitempty"`
}

// readerFuzz binds one generated Go table reader to the shared text-fuzz
// protocol. The harness supplies arbitrary wire or JSON bytes on stdin and
// compares this typed answer with the independent table engine.
func readerFuzz(unit, root, form string) error {
	input, err := io.ReadAll(os.Stdin)
	if err != nil {
		return err
	}
	c := findCodec(unit, root)
	if c == nil {
		return fmt.Errorf("no generated Go reader for %s.%s", unit, root)
	}
	value := c.fresh()
	var rep report
	var loaded bool
	switch form {
	case "wire":
		loaded = c.load(value, input, &rep)
	case "json":
		if c.fromJson == nil {
			return fmt.Errorf("generated Go reader has no JSON surface for %s.%s", unit, root)
		}
		loaded = c.fromJson(value, input, &rep)
	default:
		return fmt.Errorf("unknown reader form %q", form)
	}
	reply := readerFuzzReply{
		Loaded: loaded && !rep.malformed && !rep.refused,
		Report: readerFuzzReport{rep.unknown, rep.kindMismatch, rep.widened, rep.clamped, rep.duplicate, rep.malformed, rep.refused},
	}
	if reply.Loaded {
		size := c.measure(value)
		if size < 0 {
			return fmt.Errorf("generated Go reader measured a successfully loaded value as refused")
		}
		wire := make([]byte, size)
		if c.save(value, wire) != size {
			return fmt.Errorf("generated Go reader save did not write its measured size")
		}
		reply.Wire = wire
	}
	return json.NewEncoder(os.Stdout).Encode(reply)
}
