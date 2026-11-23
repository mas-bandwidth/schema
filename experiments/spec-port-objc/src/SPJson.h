// SPJson.h — the text form (docs/SPEC-TABLES.md §16.2, §16.3, §16.7, §16.14).
//
// Reading takes BYTES and validates UTF-8 itself (§16.2: "Every JSON read is
// handed BYTES"); it returns the whole value or throws SPRefusal. Writing is
// §16.14's W0–W14 with every option off: the canonical text, ending in one
// line feed, or SPRefusal and no text at all (W0).

#import "SPWire.h"

SPTableVal *SPFromJson(NSData *text, SPDecl *root, SPUnit *unit);
NSData *SPToJson(SPTableVal *root);

// shared with the driver's patcher
NSString *SPU128Decimal(sp_u128 v, BOOL negative);
