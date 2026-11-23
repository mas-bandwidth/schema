// SPValue.h — the in-memory value a read produces and a write consumes.
//
// The Objective-C choices, stated once:
// - text is held as RAW UTF-8 BYTES in NSData, never as NSString: an
//   NSString would replace ill-formed bytes or carry lone surrogates, and
//   the forms' rules are about bytes (SPEC-TABLES §3, §16.3);
// - every integer kind, enum ordinal, flags mask, bool, fixed-point raw value
//   and timestamp is an SPInt holding 128 bits, because NSNumber cannot hold
//   a uint128 and boxing through `long long` would wrap a uint64 silently;
// - a null pointer, an absent optional element and a payload-free arm are
//   NSNull, never nil, so a missing value is never read as 0 by a message
//   sent to nil.

#import "SPSchema.h"

@interface SPInt : NSObject <NSCopying> {
  @public
    sp_u128 raw; // two's complement at 128 bits
}
+ (SPInt *)u:(sp_u128)v;
+ (SPInt *)i:(sp_i128)v;
- (sp_i128)sval;
@end

@interface SPFloat : NSObject <NSCopying> {
  @public
    double d; // a float32 is held exactly in a double
}
+ (SPFloat *)d:(double)v;
@end

@interface SPTableVal : NSObject
@property (nonatomic, strong) SPDecl *decl;
@property (nonatomic, strong) NSMutableArray *values;
@property (nonatomic, strong) NSMutableArray<NSNumber *> *present; // optional fields
@end

@interface SPUnionVal : NSObject
@property (nonatomic) NSInteger arm; // 0: None, else 1-based arm index
@property (nonatomic, strong) id value; // NSNull for a payload-free arm or None
@end

@interface SPBlob : NSObject
@property (nonatomic, strong) NSData *data;
@property (nonatomic) BOOL isString;
@end

@interface SPMapEntry : NSObject
@property (nonatomic, strong) id key;
@property (nonatomic, strong) id value; // NSNull: an absent optional value
@end

// The value a fresh field holds: its specified default or its zero
// (SPEC §4.2), a counted array born at its minimum.
id SPNewValue(SPType *t);
SPTableVal *SPNewTable(SPDecl *d);
id SPCopyValue(id v);
void SPResolveDefault(SPField *f);
sp_i128 SPIntSigned(SPInt *v, SPType *t); // the value as its type reads it
BOOL SPTypeSigned(SPType *t);
BOOL SPIntEqual(id a, id b);
BOOL SPFloatBitsEqual(id a, id b);
int SPCompareKeys(id a, id b, SPType *keyType);
BOOL SPValidUTF8(NSData *d, BOOL allowZero);
// a field's value is shown at its guard (SPEC §4.4): every enclosing guard holds
BOOL SPGuardsHold(SPTableVal *t, SPField *f);
