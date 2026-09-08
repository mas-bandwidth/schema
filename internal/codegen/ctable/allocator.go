package ctable

const tableAllocatorRuntime = `
#ifndef SCHEMA_TABLE_ALLOCATOR_RUNTIME
#define SCHEMA_TABLE_ALLOCATOR_RUNTIME
#ifndef schema_allocate
#include <stdlib.h>
#define schema_allocate(bytes) calloc(1,(size_t)(bytes))
#endif
#ifndef schema_release
#include <stdlib.h>
#define schema_release(pointer) free(pointer)
#endif
typedef struct TableAllocator
{
    void * (*alloc)(void * context,int64_t bytes);
    void (*free)(void * context,void * pointer);
    void * context;
} TableAllocator;
static SCHEMA_UNUSED void * table_default_allocate(void * context,int64_t bytes)
{ (void)context; return bytes>0 && (uint64_t)bytes<=SIZE_MAX ? schema_allocate(bytes) : NULL; }
static SCHEMA_UNUSED void table_default_release(void * context,void * pointer)
{ (void)context; schema_release(pointer); }
static SCHEMA_UNUSED TableAllocator table_default_allocator(void)
{
    TableAllocator allocator={table_default_allocate,table_default_release,NULL}; return allocator;
}
static SCHEMA_UNUSED void * table_allocate(TableAllocator allocator,int64_t bytes)
{
    if(allocator.alloc==NULL || allocator.free==NULL || bytes<=0 || (uint64_t)bytes>SIZE_MAX) { return NULL; }
    return allocator.alloc(allocator.context,bytes);
}
static SCHEMA_UNUSED void table_release(TableAllocator allocator,void * pointer)
{ if(allocator.free!=NULL) { allocator.free(allocator.context,pointer); } }
#endif
`
