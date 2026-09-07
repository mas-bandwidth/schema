package ctable

import (
	"fmt"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// Blobs are graph nodes. Their wire body is the used bytes; their storage has
// an eight-byte header and, for text, a terminator outside the wire extent.
func tableBlobRuntime() string {
	return fmt.Sprintf(`
#ifndef SCHEMA_TABLE_BLOB_RUNTIME
#define SCHEMA_TABLE_BLOB_RUNTIME
typedef struct TableBlob { uint32_t length, zero; } TableBlob;
typedef struct TableBytesView { const uint8_t * data; int64_t length; } TableBytesView;
typedef struct TableStringView { const char * data; int64_t length; } TableStringView;
#define kTableBytesTypeId UINT64_C(0x%016x)
#define kTableStringTypeId UINT64_C(0x%016x)

static SCHEMA_UNUSED int64_t table_blob_storage( int64_t length, int terminated )
{
    if ( length < 0 || (uint64_t)length > UINT32_MAX ) { return -1; }
    return table_align_up64(8+length+(terminated ? 1 : 0));
}

static SCHEMA_UNUSED const TableBlob * table_blob_at( const TableCtx * ctx, const TableRef * ref )
{
    if ( ref->value == 0 ) { return NULL; }
    return (const TableBlob *)(const void *)(ctx != NULL && ctx->arena != NULL
        ? table_arena_at(ctx->arena,(uint32_t)ref->value) : (const uint8_t *)(const void *)ref+ref->value);
}
static SCHEMA_UNUSED TableBytesView table_bytes_at( const TableCtx * ctx, const TableRef * ref )
{
    const TableBlob * blob=table_blob_at(ctx,ref);
    TableBytesView view={NULL,0};
    if(blob!=NULL) { view.data=(const uint8_t *)(blob+1); view.length=blob->length; } return view;
}
static SCHEMA_UNUSED TableStringView table_string_at( const TableCtx * ctx, const TableRef * ref )
{
    const TableBlob * blob=table_blob_at(ctx,ref);
    TableStringView view={NULL,0};
    if(blob!=NULL) { view.data=(const char *)(blob+1); view.length=blob->length; } return view;
}
static SCHEMA_UNUSED TableBlob * table_blob_emplace( TableWorker * worker, TableRef * ref, int64_t length, int terminated )
{
    int64_t bytes=table_blob_storage(length,terminated);
    uint32_t at;
    TableBlob * blob;
    if(bytes<0) { return NULL; }
    at=table_worker_alloc(worker,bytes);
    if(at==kTableAllocFailed) { return NULL; }
    blob=(TableBlob *)(void *)table_arena_at(worker->arena,at);
    blob->length=(uint32_t)length; blob->zero=0; ref->value=at; return blob;
}
static SCHEMA_UNUSED uint8_t * table_bytes_emplace( TableWorker * worker, TableRef * ref, int64_t length )
{
    TableBlob * blob=table_blob_emplace(worker,ref,length,0);
    return blob!=NULL ? (uint8_t *)(blob+1) : NULL;
}
static SCHEMA_UNUSED char * table_string_emplace( TableWorker * worker, TableRef * ref, const char * text, int64_t length )
{
    TableBlob * blob=table_blob_emplace(worker,ref,length,1);
    char * data;
    if(blob==NULL) { return NULL; }
    data=(char *)(blob+1); if(text!=NULL && length>0) { memcpy(data,text,(size_t)length); } data[length]=0; return data;
}
#endif
`, ir.BytesWireTypeId, ir.StringWireTypeId)
}

const tableBlobGraphRuntime = `
static SCHEMA_UNUSED int table_blob_save( TableWriter * w, const void * value )
{
    const TableBlob * blob=(const TableBlob *)value;
    table_writer_raw(w,blob+1,blob->length); return !w->overflow;
}
static SCHEMA_UNUSED int table_blob_edges( TableNumbering * n, const void * value )
{ (void)n; (void)value; return 1; }
static const TableNodeType table_bytes_node_type = { kTableBytesTypeId, 0, table_blob_save, table_blob_edges, 1 };
static const TableNodeType table_string_node_type = { kTableStringTypeId, 0, table_blob_save, table_blob_edges, 2 };
`

func (g *tableGen) graphFieldType(f *ir.Field) string {
	if f.Type.Blob() {
		if f.Type.Kind == ir.TString {
			return "table_string_node_type"
		}
		return "table_bytes_node_type"
	}
	return g.graphType(f.Type.Name)
}
