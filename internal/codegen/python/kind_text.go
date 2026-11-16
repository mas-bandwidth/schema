package python

import (
	"strconv"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// The text kinds, string(N) and bytes(N): one wire shape, the length over
// [0, N], align, then the used bytes (SPEC §4.7).
//
// string(N) is serialize's own serialize_string over a buffer of N + 1, the
// wire oracle SPEC §4.7 names, so the length, the align, the interior-NUL
// refusal and the strict UTF-8 decode are the runtime's, checked in every
// build. The bound is N bytes of UTF-8, not N characters. A write validates
// first: a value that is not a str, holds a NUL, is not encodable as UTF-8 (a
// lone surrogate) or is longer than N bytes raises InvalidValue, naming the
// field. An ASCII str of at most N characters and no NUL is the inline fast
// path; anything else goes to validate_string, which accepts what is valid.
//
// bytes(N) composes the length from serialize_int (N is capped at int32,
// SPEC §4.3) and the payload from
// serialize_bytes, which aligns. A read returns bytes, a copy the caller owns
// (docs/PORT-PYTHON.md, TEXT AND BYTES); a write takes bytes, a bytearray or a
// one-dimensional "B" memoryview of at most N bytes.
func init() {
	registerKind(&kindEmitter{
		name: "string",
		read: func(c *emitCtx, f *ir.Field, target string) {
			rs := c.ref("_s_rs", `""`)
			op := c.streamOp("serialize_string")
			c.line("if not %s(%s, %d):", op, rs, f.Type.Size+1)
			c.line("    return False")
			c.line("%s = %s.value", target, rs)
		},
		write: func(c *emitCtx, f *ir.Field, expr string) {
			rs := c.ref("_s_rs", `""`)
			op := c.streamOp("serialize_string")
			c.line("%s.value = %s", rs, expr)
			c.line("%s(%s, %d)", op, rs, f.Type.Size+1)
		},
		validate: func(c *emitCtx, f *ir.Field, expr, path string) {
			c.m.needSchema("validate_string")
			tp := c.bind("_s_type", "type")
			st := c.bind("_s_str", "str")
			v := c.local()
			n := strconv.FormatInt(f.Type.Size, 10)
			c.line("%s = %s", v, expr)
			c.line("if %s(%s) is not %s or len(%s) > %s or not %s.isascii() or \"\\x00\" in %s:", tp, v, st, v, n, v, v)
			c.line("    validate_string(%s, %s, %s)", v, fieldPath(path, f.Name), n)
		},
		zero: func(c *emitCtx, f *ir.Field, target string) {
			c.line("%s = \"\"", target)
		},
	})
	registerKind(&kindEmitter{
		name: "bytes",
		read: func(c *emitCtx, f *ir.Field, target string) {
			ri := c.ref("_s_ri", "0")
			rb := c.ref("_s_rby", `b""`)
			op := c.streamOp("serialize_int")
			bop := c.streamOp("serialize_bytes")
			c.line("if not %s(%s, 0, %d):", op, ri, f.Type.Size)
			c.line("    return False")
			c.line("if not %s(%s, %s.value):", bop, rb, ri)
			c.line("    return False")
			c.line("%s = %s.value", target, rb)
		},
		write: func(c *emitCtx, f *ir.Field, expr string) {
			ri := c.ref("_s_ri", "0")
			rb := c.ref("_s_rby", `b""`)
			op := c.streamOp("serialize_int")
			bop := c.streamOp("serialize_bytes")
			c.line("%s.value = %s", rb, expr)
			c.line("%s.value = len(%s.value)", ri, rb)
			c.line("%s(%s, 0, %d)", op, ri, f.Type.Size)
			c.line("%s(%s, %s.value)", bop, rb, ri)
		},
		validate: func(c *emitCtx, f *ir.Field, expr, path string) {
			c.m.needSchema("validate_bytes")
			tp := c.bind("_s_type", "type")
			bt := c.bind("_s_bytes", "bytes")
			v := c.local()
			n := strconv.FormatInt(f.Type.Size, 10)
			c.line("%s = %s", v, expr)
			c.line("if %s(%s) is not %s or len(%s) > %s:", tp, v, bt, v, n)
			c.line("    validate_bytes(%s, %s, %s)", v, fieldPath(path, f.Name), n)
		},
		zero: func(c *emitCtx, f *ir.Field, target string) {
			c.line("%s = b\"\"", target)
		},
	})
}

// textBits is the widest wire a string(N) or bytes(N) field can take: the
// length prefix, up to seven bits of alignment, then N bytes.
func textBits(size int64) int64 {
	return rangeBits64(0, size) + 7 + 8*size
}
