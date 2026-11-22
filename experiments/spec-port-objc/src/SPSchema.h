// SPSchema.h — the schema language (docs/SPEC.md §4, docs/SPEC-TABLES.md §2)
// read from `.schema` text. Written from the specification alone.
//
// A port receives a schema as `.schema` source: the specification names no
// exported IR a port can read (SPEC-GAPS.md, gap 1), so this file is a
// scanner, a recursive-descent parser and a resolver for the subset of the
// grammar the table forms need.

#import <Foundation/Foundation.h>

typedef unsigned __int128 sp_u128;
typedef __int128 sp_i128;

// A refusal anywhere in this port is an NSException with this name. It is
// never a nil return: nil messaging answers 0 in Objective-C, and a refusal
// that could be read as 0 is a refusal that could be lost (SPEC-GAPS.md,
// the Objective-C notes). The reason is diagnostic only (SPEC-TABLES §4).
extern NSString *const SPRefusal;
// A schema this port cannot load (a parse or resolve error, or a construct
// it does not carry). Distinct from a refusal of DATA.
extern NSString *const SPSchemaError;

void SPRefuse(NSString *reason, ...) NS_FORMAT_FUNCTION(1, 2) __attribute__((noreturn));
void SPSchemaFail(NSString *reason, ...) NS_FORMAT_FUNCTION(1, 2) __attribute__((noreturn));

uint64_t SPFnv1a64(NSData *bytes);
uint64_t SPFnv1a64Str(NSString *s);

typedef NS_ENUM(int, SPKind) {
    SPKInt = 1,        // intN / uintN, ranged or bare
    SPKBits,           // bits(N)
    SPKBool,
    SPKFloat,          // float32 / float64, compressed when hasRange
    SPKString,         // string(N), or string with cap < 0 (unbounded)
    SPKBytes,          // bytes(N)
    SPKFixed,          // fixed(I,F) / ufixed(I,F)
    SPKInt128,         // int128 / uint128
    SPKTimestamp,
    SPKEnum,
    SPKFlags,
    SPKStruct,         // a `type` or a `table` held by value
    SPKUnion,
    SPKPointer,        // *T, T a table
    SPKBlobBytes,      // *bytes
    SPKBlobString,     // *string
    SPKArray,          // [N]T, [A..B]T, []T
    SPKKeyed,          // [E]T
    SPKMap,            // map[K]V
};

typedef NS_ENUM(int, SPArrayShape) {
    SPArrayFixed = 1,
    SPArrayCounted,
    SPArrayUnbounded,
};

@class SPDecl;

// A folded constant: an integer (128 bits here) or a float64.
@interface SPNum : NSObject
@property (nonatomic) BOOL isFloat;
@property (nonatomic) sp_i128 i;
@property (nonatomic) double d;
- (double)asDouble;
@end

@interface SPType : NSObject
@property (nonatomic) SPKind kind;
@property (nonatomic) int bits;          // storage width: int 8..64, float 32/64, fixed I+F, bits(N) N
@property (nonatomic) BOOL isSigned;
@property (nonatomic) BOOL hasRange;     // ranged int, compressed float, fixed (always)
@property (nonatomic) sp_i128 imin, imax; // integer range (raw scale for fixed)
@property (nonatomic) double fmin, fmax, fres;
@property (nonatomic) int fixedI, fixedF;
@property (nonatomic) long long cap;     // string/bytes N, -1 unbounded
@property (nonatomic, strong) SPDecl *decl; // enum / flags / struct / union / pointer target
@property (nonatomic, strong) NSString *refName; // unresolved
@property (nonatomic, strong) SPType *elem;    // array, keyed, map value
@property (nonatomic, strong) SPType *key;     // map key
@property (nonatomic) BOOL elemOptional;       // [..N]?string(M), map[K]?V
@property (nonatomic) SPArrayShape shape;
@property (nonatomic) long long minCount, maxCount; // counted array bounds; fixed: both N
@property (nonatomic, strong) SPDecl *keyEnum; // [E]T
@property (nonatomic) long long total;   // text list total, -1 none
// the attribute section's min/max/resolution, pending until the innermost
// scalar is known (they qualify the element of an array: SPEC §4.2)
@property (nonatomic, strong) NSDictionary *pendingAttrs;
- (int)wireKind;          // SPEC-TABLES §3's kind number
- (BOOL)isStructural;     // counts toward the nesting bound (SPEC-TABLES §4)
@end

@interface SPGuard : NSObject
@property (nonatomic) NSInteger fieldIndex;
@property (nonatomic) BOOL wantTrue;
@end

@interface SPField : NSObject
@property (nonatomic, strong) NSString *name;
@property (nonatomic, strong) NSString *wireName;   // `was`, else name
@property (nonatomic, strong) NSString *jsonKey;    // `json`, else name
@property (nonatomic, strong) SPType *type;         // nil: a payload-free arm
@property (nonatomic) BOOL optional;                // ?T
@property (nonatomic) BOOL required;
@property (nonatomic) BOOL deprecated;
@property (nonatomic, strong) id defaultLiteral;    // parsed default, or nil
@property (nonatomic, strong) id defaultValue;      // the resolved construction value (SPValue.h)
@property (nonatomic, strong) NSArray<SPGuard *> *guards; // conjunction
@property (nonatomic) uint64_t wireId;
@property (nonatomic, strong) NSDictionary *attrs;
@end

typedef NS_ENUM(int, SPDeclKind) {
    SPDeclEnum = 1,
    SPDeclFlags,
    SPDeclType,
    SPDeclTable,
    SPDeclUnion,
};

@interface SPVariant : NSObject
@property (nonatomic, strong) NSString *name;
@property (nonatomic, strong) NSString *wireName;
@property (nonatomic) uint64_t wireId;
@property (nonatomic) BOOL deprecated;
@end

@interface SPDecl : NSObject
@property (nonatomic) SPDeclKind kind;
@property (nonatomic) BOOL fixed;       // `fixed table`
@property (nonatomic, strong) NSString *name;
@property (nonatomic, strong) NSString *wireName;
@property (nonatomic) uint64_t wireId;
@property (nonatomic, strong) NSMutableArray<SPField *> *fields;     // type/table; union arms
@property (nonatomic, strong) NSMutableArray<SPVariant *> *variants; // enum/flags
@property (nonatomic) int flagsWidth;   // flags: max = K, else count
@property (nonatomic, strong) NSDictionary *attrs;
@property (nonatomic) BOOL integrity;
@property (nonatomic) BOOL jsonArray;
- (NSInteger)variantIndexForWireId:(uint64_t)wid; // 1-based ordinal, 0 none
- (NSInteger)fieldIndexForWireId:(uint64_t)wid;   // -1 none
@end

@interface SPUnit : NSObject
@property (nonatomic, strong) NSString *package;
@property (nonatomic, strong) NSMutableDictionary<NSString *, SPDecl *> *decls;
@property (nonatomic, strong) NSMutableDictionary<NSString *, id> *consts;
@property (nonatomic, strong) NSMutableDictionary<NSNumber *, SPDecl *> *tablesByWireId;
+ (SPUnit *)loadPaths:(NSArray<NSString *> *)paths; // files or directories of .schema
- (SPDecl *)declNamed:(NSString *)name;
@end
