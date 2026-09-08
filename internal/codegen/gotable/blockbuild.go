package gotable

import "github.com/mas-bandwidth/schema/v2/ir"

const blockBuildRuntime = `// TableBlockAllocator supplies exactly one allocation per storage lifetime.
// Both functions are required; the free receives the original, unsliced buffer.
type TableBlockAllocator struct {
 Alloc func(int64) []byte
 Free func([]byte)
}

func TableBlockDefaultAllocator() TableBlockAllocator {
 return TableBlockAllocator{Alloc:func(n int64) []byte {
  if n <= 0 || uint64(n)>uint64(^uint(0)>>1) {return nil}
  return make([]byte,int(n))
 }, Free:func([]byte){}}
}

// TableBlockRefusal names the producer count that violates its declared bound.
type TableBlockRefusal struct { Field string; Count, Maximum int64 }

`

func (g *blockGen) emitBlockBuild(bl *ir.BlockLayout) {
	n := bl.Table.Name
	g.hf("// %sCounts is gathered before Begin; clamping is the caller's policy.\ntype %sCounts struct {\n", n, n)
	for _, a := range bl.Arrays {
		g.hf("%s int32\n", ir.GoExportName(a.Field.Name))
	}
	g.hf("}\n\n")
	g.hf("// %sBlockStorage owns a maximum-sized extent. Do not copy an initialized storage.\n", n)
	g.hf("type %sBlockStorage struct { base unsafe.Pointer; allocation []byte; allocator TableBlockAllocator }\n", n)
	g.hf("func (s *%sBlockStorage) Create(a TableBlockAllocator) bool {\n", n)
	g.hf("if s==nil || s.allocation!=nil || a.Alloc==nil || a.Free==nil {return false}\n")
	g.hf("raw:=a.Alloc(%sBlockMaxBytes+63)\n", n)
	g.hf("if int64(len(raw))<%sBlockMaxBytes+63 {if raw!=nil {a.Free(raw)};return false}\n", n)
	g.hf("s.allocation=raw;s.allocator=a;offset:=(-uintptr(unsafe.Pointer(&raw[0])))&63;s.base=unsafe.Pointer(&raw[offset]);return true\n}\n")
	g.hf("func (s *%sBlockStorage) Destroy() {if s!=nil && s.allocation!=nil {s.allocator.Free(s.allocation);*s=%sBlockStorage{}}}\n\n", n, n)
	g.hf("// %sBlockBegin writes only the prologue and triples, never rows or padding.\n", n)
	g.hf("// Storage and all counts are checked before any byte of the extent changes.\n")
	g.hf("func %sBlockBegin(b *%sBlock,s *%sBlockStorage,c %sCounts,refusal *TableBlockRefusal) bool {\n", n, n, n, n)
	g.hf("if refusal!=nil {*refusal=TableBlockRefusal{}};if b==nil {return false};*b=%sBlock{}\n", n)
	g.hf("if s==nil || s.base==nil {return false}\n")
	for _, a := range bl.Arrays {
		field := ir.GoExportName(a.Field.Name)
		g.hf("if c.%s<0 || c.%s>%d {if refusal!=nil {*refusal=TableBlockRefusal{Field:%q,Count:int64(c.%s),Maximum:%d}};return false}\n", field, field, a.Max, a.Field.Name, field, a.Max)
	}
	g.hf("p:=(*%sBlockProjection)(s.base);p.Magic=TableBlockMagic;p.BuildVersion=BuildVersion;p.ByteOrder=TableBlockByteOrder\n", n)
	g.hf("offset:=int64(%d)\n", bl.Projection.Size)
	for _, a := range bl.Arrays {
		field := ir.GoExportName(a.Field.Name)
		g.hf("offset=(offset+%d)&^int64(%d)\n", blockStartAlign(a)-1, blockStartAlign(a)-1)
		g.hf("p.%s=TableBlockTriple{OffsetOf:uint64(offset),Count:uint32(c.%s),Stride:%d};offset+=int64(c.%s)*%d\n", field, field, a.Stride, field, a.Stride)
	}
	g.hf("*b=%sBlock{Base:s.base,Projection:p,Bytes:(offset+63)&^int64(63)};return true\n}\n\n", n)
	g.hf("func %sBlockBytes(b *%sBlock) int64 {if b==nil || b.Projection==nil {return 0};used:=int64(%d)\n", n, n, bl.Projection.Size)
	for _, a := range bl.Arrays {
		field := ir.GoExportName(a.Field.Name)
		g.hf("if end:=int64(b.Projection.%s.OffsetOf)+int64(b.Projection.%s.Count)*int64(b.Projection.%s.Stride);end>used {used=end}\n", field, field, field)
	}
	g.hf("return (used+63)&^int64(63)\n}\n\n")
}
