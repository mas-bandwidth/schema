// Package serialize is a stub module for table-generated-go compatibility.
package serialize

import (
	"math"
)

var ErrValueOutOfRange error = nil

type WriteStream struct {
	Data []byte
	err  error
}

type ReadStream struct {
	Data []byte
	Off  int
	err  error
}

func (s *WriteStream) SerializeBits(ptr *uint32, bits uint)      { if ptr != nil { *ptr = 0 } }
func (s *WriteStream) SerializeBits64(ptr *uint64, bits uint)     { if ptr != nil { *ptr = 0 } }
func (s *WriteStream) SerializeBytes(b []byte)                    { s.Data = append(s.Data, b...) }
func (s *WriteStream) SerializeFloat32(v *float32)                { if v != nil { *v = 0 } }
func (s *WriteStream) SerializeFloat64(v *float64)                { if v != nil { *v = 0 } }
func (s *WriteStream) SerializeFixed64(v *int64)                  { if v != nil { *v = 0 } }
func (s *WriteStream) Err() error                                 { return s.err }

func (s *ReadStream) SerializeBits(ptr *uint32, bits uint)        { if ptr != nil { *ptr = 0 } }
func (s *ReadStream) SerializeBits64(ptr *uint64, bits uint)      { if ptr != nil { *ptr = 0 } }
func (s *ReadStream) SerializeBytes(b []byte)                 { _ = b }
func (s *ReadStream) SerializeFloat32(v *float32)                 { if v != nil { *v = float32(math.NaN()) } }
func (s *ReadStream) SerializeFixed64(v *int64)                 { if v != nil { *v = 0 } }
func (s *ReadStream) SerializeFloat64(v *float64)                 { if v != nil { *v = math.NaN() } }
func (s *ReadStream) Err() error                                  { return s.err }

type Int128 struct{ Lo, Hi uint64 }
type Uint128 struct{ Lo, Hi uint64 }

func Int128From64(v uint64) Int128     { return Int128{v, 0} }
func Int128FromSigned(v int64) Int128  { return Int128{uint64(v), uint64(int64(v >> 63))} }
func Uint128From64(v uint64) Uint128   { return Uint128{v, 0} }

func (a Int128) Cmp(b Int128) int {
	if a.Hi < b.Hi { return -1 }
	if a.Hi > b.Hi { return 1 }
	return int(a.Lo) - int(b.Lo)
}
func (a Uint128) Cmp(b Uint128) int {
	if a.Hi < b.Hi { return -1 }
	if a.Hi > b.Hi { return 1 }
	return int(a.Lo) - int(b.Lo)
}
