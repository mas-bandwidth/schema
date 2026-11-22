// SPWire.h — the variable form, form byte 1 (docs/SPEC-TABLES.md §3, §3.1,
// §3.2, §4, §22.1–§22.5). Both directions throw SPRefusal; neither returns
// a partial value or partial bytes.

#import "SPValue.h"

#define SP_NODE_TABLE_ID 0xFFFFFFFFFFFFFFFFULL
#define SP_BUILD_VERSION_ID 0xFFFFFFFFFFFFFFFEULL
#define SP_VOCABULARY_ID 0xFFFFFFFFFFFFFFFDULL
#define SP_KEY_ID 0x3DC94A19365B10ECULL
#define SP_VALUE_ID 0x7CE4FD9430E80CEAULL
#define SP_MAX_IDS 4096
#define SP_MAX_DEPTH 128

uint32_t SPCrc32c(const uint8_t *p, NSUInteger n);

NSData *SPSaveVariable(SPTableVal *root);
SPTableVal *SPLoadVariable(NSData *wire, SPDecl *root, SPUnit *unit);
// the form-1 reader at any root, a declared fixed one included (SPEC-GAP 11)
SPTableVal *SPLoadForm1(NSData *wire, SPDecl *root, SPUnit *unit);

// write-contract checks shared with the text writer (SPEC §5.1)
void SPCheckScalar(id v, SPType *t, NSString *where);
