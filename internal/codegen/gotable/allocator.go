package gotable

const tableAllocatorSource = `
// TableAllocator owns authoring and temporary writer storage. Both callbacks
// are required together; an empty pair uses Go-owned storage. Alloc supplies
// zeroed bytes aligned to tableRegionAlign and may return more than requested.
// Free always receives the original slice, with its original length and base.
// Load and LoadMeasure use caller buffers and never call this pair.
type TableAllocator struct { Alloc func(int64) []byte; Free func([]byte) }
func(a TableAllocator) valid()bool{return (a.Alloc==nil)==(a.Free==nil)}
func(a TableAllocator) alloc(n int64)[]byte {
 if !a.valid()||n<=0||uint64(n)>uint64(^uint(0)>>1)-uint64(tableRegionAlign){return nil}
 if a.Alloc!=nil {b:=a.Alloc(n);if int64(len(b))<n||uintptr(unsafe.Pointer(&b[0]))%uintptr(tableRegionAlign)!=0{a.free(b);return nil};return b}
 raw:=make([]byte,n+tableRegionAlign-1);off:=(-uintptr(unsafe.Pointer(&raw[0])))&uintptr(tableRegionAlign-1);return raw[off:off+uintptr(n)]
}
func(a TableAllocator) free(b []byte){if a.Free!=nil&&b!=nil{a.Free(b)}}

// TableWriteContext selects allocation independently from reference addressing.
// Pass a TableAllocator for a loaded/locked region, or *TableArena for authoring
// references and that arena's allocation pair. Omit it for Go-owned temporaries.
type TableWriteContext interface{tableWriteOptions()(*TableArena,TableAllocator)}
func(a TableAllocator)tableWriteOptions()(*TableArena,TableAllocator){return nil,a}
func(a *TableArena)tableWriteOptions()(*TableArena,TableAllocator){if a==nil{return nil,TableAllocator{}};return a,a.Allocator}
func tableWriteOptions(options []TableWriteContext)(*TableArena,TableAllocator){if len(options)>0&&options[0]!=nil{return options[0].tableWriteOptions()};return nil,TableAllocator{}}
`
