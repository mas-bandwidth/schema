// SPWire.m — the variable form (form byte 1), read and write.
//
// Reading follows docs/SPEC-TABLES.md §3 and §4: ONE read, which returns the
// whole value or refuses (SPRefusal), with evolution (unknown ids, absent
// fields, widening) the only thing it forgives. Writing follows §22.1–§22.5
// step by step.

#import "SPWire.h"
#include <math.h>

// ---------------------------------------------------------------------------
// shared helpers

uint32_t SPCrc32c(const uint8_t *p, NSUInteger n) {
    static uint32_t table[256];
    static dispatch_once_t once;
    dispatch_once(&once, ^{
        for (uint32_t i = 0; i < 256; i++) {
            uint32_t c = i;
            for (int k = 0; k < 8; k++) c = (c & 1) ? (c >> 1) ^ 0x82F63B78u : c >> 1;
            table[i] = c;
        }
    });
    uint32_t c = 0xFFFFFFFFu;
    for (NSUInteger i = 0; i < n; i++) c = table[(c ^ p[i]) & 0xFF] ^ (c >> 8);
    return c ^ 0xFFFFFFFFu;
}

static int kindWidth(int k) {
    switch (k) {
    case 1: case 2: case 6: case 20: case 25: return 1;
    case 3: case 7: case 21: case 26: return 2;
    case 4: case 8: case 10: case 22: case 27: return 4;
    case 5: case 9: case 11: case 23: case 28: case 36: return 8;
    case 18: case 19: case 24: case 29: return 16;
    }
    return 0;
}
static BOOL kindSigned(int k) {
    return (k >= 2 && k <= 5) || k == 18 || (k >= 20 && k <= 24) || k == 36;
}
static int ladderPos(int k, int *ladder) {
    static const int s[] = { 2, 3, 4, 5, 18 }, u[] = { 6, 7, 8, 9, 19 };
    for (int i = 0; i < 5; i++) {
        if (s[i] == k) { *ladder = 1; return i; }
        if (u[i] == k) { *ladder = 2; return i; }
    }
    *ladder = 0;
    return -1;
}
// The kind pair decides (SPEC-TABLES §4, WIDENED): equal, or a narrower
// kind on the same integer ladder, or f32 into f64.
static BOOL kindAccepts(int declared, int wire) {
    if (declared == wire) return YES;
    if (declared == 11 && wire == 10) return YES;
    int ld, lw;
    int pd = ladderPos(declared, &ld), pw = ladderPos(wire, &lw);
    return ld && ld == lw && pw < pd;
}
static BOOL arrayElemKindLegal(int k) {
    return (k >= 1 && k <= 13) || k == 15 || k == 17 || (k >= 18 && k <= 30);
}
static BOOL keyedElemKindLegal(int k) {
    return (k >= 1 && k <= 11) || k == 13 || (k >= 18 && k <= 30);
}

static int elemWireKind(SPType *t) {
    // the element kind byte an array body carries for this declaration
    if (t.kind == SPKArray && t.elemOptional) return 13; // [..N]?string(M)
    if (t.kind == SPKBytes) return 6;
    return t.elem.wireKind;
}

static BOOL rangeHolds(sp_u128 raw, SPType *t) {
    if (SPTypeSigned(t)) {
        sp_i128 v = (sp_i128)raw;
        return v >= t.imin && v <= t.imax;
    }
    return raw >= (sp_u128)t.imin && raw <= (sp_u128)t.imax;
}

// ---------------------------------------------------------------------------
// The write contract (SPEC §5.1): a new language refuses in every build.

void SPCheckScalar(id v, SPType *t, NSString *where) {
    switch (t.kind) {
    case SPKInt:
        if (!rangeHolds(((SPInt *)v)->raw, t)) SPRefuse(@"invalid_value: %@ off its range", where);
        return;
    case SPKFixed:
        if (!rangeHolds(((SPInt *)v)->raw, t)) SPRefuse(@"invalid_value: %@ off its range", where);
        return;
    case SPKInt128:
        if (t.hasRange && !rangeHolds(((SPInt *)v)->raw, t)) SPRefuse(@"invalid_value: %@ off its range", where);
        return;
    case SPKBits:
        if (((SPInt *)v)->raw > (sp_u128)t.imax) SPRefuse(@"invalid_value: %@ wider than bits(%d)", where, t.bits);
        return;
    case SPKBool:
        if (((SPInt *)v)->raw > 1) SPRefuse(@"invalid_value: %@ bool", where);
        return;
    case SPKEnum: {
        sp_u128 k = ((SPInt *)v)->raw;
        if (k > t.decl.variants.count) SPRefuse(@"invalid_value: %@ enum ordinal outside its set", where);
        if (k && t.decl.variants[(NSUInteger)k - 1].deprecated) SPRefuse(@"invalid_value: %@ holds a deprecated variant", where);
        return;
    }
    case SPKFlags:
        if (((SPInt *)v)->raw >> 64) SPRefuse(@"invalid_value: %@ mask past 64 bits", where);
        return;
    case SPKTimestamp: {
        sp_i128 x = (sp_i128)((SPInt *)v)->raw;
        if (x < INT64_MIN || x > INT64_MAX) SPRefuse(@"invalid_value: %@ timestamp", where);
        return;
    }
    case SPKFloat: {
        double d = ((SPFloat *)v)->d;
        if (!isfinite(d)) SPRefuse(@"non_finite_float: %@", where);
        if (t.bits == 32 || t.hasRange) {
            float f = (float)d;
            if (!isfinite(f)) SPRefuse(@"non_finite_float: %@ past float32's range", where);
        }
        if (t.hasRange && (d < t.fmin || d > t.fmax)) SPRefuse(@"invalid_value: %@ off its range", where);
        return;
    }
    case SPKString: {
        NSData *s = v;
        if (t.cap >= 0 && (long long)s.length > t.cap) SPRefuse(@"invalid_value: %@ past its capacity", where);
        if (!SPValidUTF8(s, NO)) SPRefuse(@"invalid_value: %@ is not text", where);
        return;
    }
    case SPKBytes:
        if ((long long)((NSData *)v).length > t.cap) SPRefuse(@"invalid_value: %@ past its capacity", where);
        return;
    default:
        return;
    }
}

// ---------------------------------------------------------------------------
// Writer

@interface SPWriter : NSObject
@property (nonatomic, strong) NSMutableArray<NSNumber *> *ids;
@property (nonatomic, strong) NSMutableDictionary<NSNumber *, NSNumber *> *refOf;
@property (nonatomic, strong) NSMapTable *nodeIndex; // object -> NSNumber index
@property (nonatomic, strong) NSMapTable *open;      // object -> @YES while open
@property (nonatomic, strong) NSMutableArray *nodes;  // index 2.. in order
@property (nonatomic, strong) SPTableVal *root;
@end

static void putLEB(NSMutableData *d, uint64_t v) {
    do {
        uint8_t b = v & 0x7F;
        v >>= 7;
        if (v) b |= 0x80;
        [d appendBytes:&b length:1];
    } while (v);
}
static void putByte(NSMutableData *d, uint8_t b) { [d appendBytes:&b length:1]; }
static void putLE(NSMutableData *d, sp_u128 v, int width) {
    for (int i = 0; i < width; i++) { uint8_t b = (uint8_t)(v >> (8 * i)); [d appendBytes:&b length:1]; }
}

BOOL SPElided(SPTableVal *t, NSUInteger i);
BOOL SPValueIsDefault(id v, SPType *t, id def);

static BOOL elemIsDefault(id v, SPType *arr) {
    SPType *e = arr.elem;
    if (arr.kind == SPKArray && arr.elemOptional) return v == [NSNull null];
    return SPValueIsDefault(v, e, nil);
}

BOOL SPBodyEmpty(SPTableVal *t) {
    for (NSUInteger i = 0; i < t.decl.fields.count; i++)
        if (!SPElided(t, i)) return NO;
    return YES;
}

// def nil: the type's construction value (an element's default)
BOOL SPValueIsDefault(id v, SPType *t, id def) {
    switch (t.kind) {
    case SPKInt: case SPKBits: case SPKBool: case SPKFixed: case SPKInt128:
    case SPKTimestamp: case SPKEnum: case SPKFlags:
        return ((SPInt *)v)->raw == (def ? ((SPInt *)def)->raw : 0);
    case SPKFloat: {
        double d = ((SPFloat *)v)->d, z = def ? ((SPFloat *)def)->d : 0.0;
        if (t.hasRange) return d == z;
        if (t.bits == 32) { float a = (float)d, b = (float)z; return memcmp(&a, &b, 4) == 0; }
        return memcmp(&d, &z, 8) == 0;
    }
    case SPKString: case SPKBytes:
        return [(NSData *)v isEqualToData:(def ?: [NSData data])];
    case SPKStruct: return SPBodyEmpty(v);
    case SPKUnion: return ((SPUnionVal *)v).arm == 0;
    case SPKPointer: case SPKBlobBytes: case SPKBlobString: return v == [NSNull null];
    case SPKArray: {
        NSArray *a = v;
        if (t.shape == SPArrayFixed) {
            if (!t.elemOptional && t.elem.kind == SPKStruct) return NO; // position is identity
            for (id x in a) if (!elemIsDefault(x, t)) return NO;
            return YES;
        }
        if ((long long)a.count != (t.shape == SPArrayCounted ? t.minCount : 0)) return NO;
        for (id x in a) if (!elemIsDefault(x, t)) return NO;
        return YES;
    }
    case SPKKeyed:
        for (id x in (NSArray *)v) if (!SPValueIsDefault(x, t.elem, nil)) return NO;
        return YES;
    case SPKMap: return ((NSArray *)v).count == 0;
    }
    return NO;
}

BOOL SPElided(SPTableVal *t, NSUInteger i) {
    SPField *f = t.decl.fields[i];
    if (!SPGuardsHold(t, f)) return YES;
    if (f.optional) return ![t.present[i] boolValue];
    if (f.required) return NO;
    return SPValueIsDefault(t.values[i], f.type, f.defaultValue);
}

@implementation SPWriter

- (uint64_t)ref:(uint64_t)wid {
    NSNumber *r = self.refOf[@(wid)];
    if (r) return r.unsignedLongLongValue;
    if (self.ids.count >= SP_MAX_IDS) SPRefuse(@"invalid_value: the save needs more than 4096 ids");
    [self.ids addObject:@(wid)];
    self.refOf[@(wid)] = @(self.ids.count);
    return self.ids.count;
}

// --- §22.3 numbering: depth-first pre-order from the root
- (void)numberBody:(SPTableVal *)t {
    for (NSUInteger i = 0; i < t.decl.fields.count; i++) {
        SPField *f = t.decl.fields[i];
        if (!SPGuardsHold(t, f)) continue;
        if (f.optional && ![t.present[i] boolValue]) continue;
        [self numberValue:t.values[i] type:f.type];
    }
}
- (void)numberValue:(id)v type:(SPType *)t {
    switch (t.kind) {
    case SPKStruct: [self numberBody:v]; return;
    case SPKUnion: {
        SPUnionVal *u = v;
        if (u.arm == 0) return;
        SPField *arm = t.decl.fields[(NSUInteger)u.arm - 1];
        if (arm.type) [self numberValue:u.value type:arm.type];
        return;
    }
    case SPKPointer: case SPKBlobBytes: case SPKBlobString: {
        if (v == [NSNull null]) return;
        if (v == self.root) SPRefuse(@"data_cycle: a pointer names the root");
        NSNumber *idx = [self.nodeIndex objectForKey:v];
        if (idx) {
            if (t.kind != SPKPointer) SPRefuse(@"blob_shared: a blob named from two slots");
            if ([self.open objectForKey:v]) SPRefuse(@"data_cycle");
            return;
        }
        [self.nodes addObject:v];
        [self.nodeIndex setObject:@(self.nodes.count + 1) forKey:v];
        if (t.kind == SPKPointer) {
            [self.open setObject:@YES forKey:v];
            [self numberBody:v];
            [self.open removeObjectForKey:v];
        }
        return;
    }
    case SPKArray: case SPKKeyed:
        if (t.kind == SPKArray && t.elemOptional) return;
        for (id x in (NSArray *)v) [self numberValue:x type:t.elem];
        return;
    case SPKMap:
        for (SPMapEntry *e in [self sortedEntries:v type:t])
            if (!(t.elemOptional && e.value == [NSNull null])) [self numberValue:e.value type:t.elem];
        return;
    default:
        return;
    }
}

- (NSArray<SPMapEntry *> *)sortedEntries:(NSArray *)entries type:(SPType *)t {
    NSArray *s = [entries sortedArrayUsingComparator:^NSComparisonResult(SPMapEntry *a, SPMapEntry *b) {
        return (NSComparisonResult)SPCompareKeys(a.key, b.key, t.key);
    }];
    for (NSUInteger i = 1; i < s.count; i++)
        if (SPCompareKeys([s[i - 1] key], [s[i] key], t.key) == 0) SPRefuse(@"invalid_value: a map holds one key twice");
    return s;
}

// --- §22.2 bodies
- (NSData *)body:(SPTableVal *)t root:(BOOL)isRoot {
    NSMutableData *d = [NSMutableData data];
    for (NSUInteger i = 0; i < t.decl.fields.count; i++) {
        if (SPElided(t, i)) continue;
        SPField *f = t.decl.fields[i];
        putLEB(d, [self ref:f.wireId]);
        putByte(d, (uint8_t)f.type.wireKind);
        [d appendData:[self fieldPayload:t.values[i] type:f.type where:[NSString stringWithFormat:@"%@.%@", t.decl.name, f.name] field:f]];
    }
    if (isRoot && self.nodes.count) {
        putLEB(d, [self ref:SP_NODE_TABLE_ID]);
        putByte(d, 12);
        NSMutableData *p = [NSMutableData data];
        putLEB(p, self.nodes.count);
        for (id n in self.nodes) {
            NSData *b;
            if ([n isKindOfClass:[SPBlob class]]) {
                SPBlob *blob = n;
                putLEB(p, [self ref:SPFnv1a64Str(blob.isString ? @"string" : @"bytes")]);
                if (blob.isString && !SPValidUTF8(blob.data, NO)) SPRefuse(@"invalid_value: a *string blob is not text");
                if (blob.data.length > 0xFFFFFFFFull) SPRefuse(@"invalid_value: blob past its cap");
                b = blob.data;
            } else {
                SPTableVal *nt = n;
                putLEB(p, [self ref:nt.decl.wireId]);
                b = [self body:nt root:NO];
            }
            putLEB(p, b.length);
            [p appendData:b];
        }
        putLEB(d, p.length);
        [d appendData:p];
    }
    putByte(d, 0);
    return d;
}

// the bytes after a field's kind byte
- (NSData *)fieldPayload:(id)v type:(SPType *)t where:(NSString *)where field:(SPField *)f {
    NSMutableData *d = [NSMutableData data];
    switch (t.kind) {
    case SPKString: case SPKStruct: case SPKArray: case SPKKeyed: case SPKMap: case SPKBytes: {
        NSData *in = [self inner:v type:t where:where];
        putLEB(d, in.length);
        [d appendData:in];
        return d;
    }
    default:
        return [self inner:v type:t where:where];
    }
}

// a value's bytes inside its framing: a fixed-width payload, a reference or
// index, a string's bytes, a table body, an array/keyed/map body, a union payload
- (NSData *)inner:(id)v type:(SPType *)t where:(NSString *)where {
    NSMutableData *d = [NSMutableData data];
    SPCheckScalar(v, t, where);
    switch (t.kind) {
    case SPKBool: case SPKInt: case SPKBits: case SPKFixed: case SPKInt128: case SPKTimestamp: case SPKFlags:
        putLE(d, ((SPInt *)v)->raw, kindWidth(t.wireKind));
        return d;
    case SPKFloat: {
        double x = ((SPFloat *)v)->d;
        if (t.bits == 32 || t.hasRange) {
            float fl = (float)x;
            if (t.hasRange && fl == 0) fl = 0.0f; // a compressed -0 is stored as +0 (§22.2)
            uint32_t b; memcpy(&b, &fl, 4);
            putLE(d, b, 4);
        } else {
            uint64_t b; memcpy(&b, &x, 8);
            putLE(d, b, 8);
        }
        return d;
    }
    case SPKEnum: {
        sp_u128 k = ((SPInt *)v)->raw;
        putLEB(d, k ? [self ref:t.decl.variants[(NSUInteger)k - 1].wireId] : 0);
        return d;
    }
    case SPKString: return v;
    case SPKBytes: {
        NSData *b = v;
        putByte(d, 6);
        putLEB(d, b.length);
        [d appendData:b];
        return d;
    }
    case SPKStruct: return [self body:v root:NO];
    case SPKPointer: case SPKBlobBytes: case SPKBlobString: {
        if (v == [NSNull null]) { putLEB(d, 0); return d; }
        if (t.kind == SPKPointer && ![v isKindOfClass:[SPTableVal class]]) SPRefuse(@"invalid_value: %@ pointer target", where);
        if (t.kind != SPKPointer && ![v isKindOfClass:[SPBlob class]]) SPRefuse(@"invalid_value: %@ blob", where);
        putLEB(d, [[self.nodeIndex objectForKey:v] unsignedLongLongValue]);
        return d;
    }
    case SPKUnion: {
        SPUnionVal *u = v;
        if (u.arm < 0 || u.arm > (NSInteger)t.decl.fields.count) SPRefuse(@"invalid_value: %@ union tag outside its arms", where);
        if (u.arm == 0) { putLEB(d, 0); return d; }
        SPField *arm = t.decl.fields[(NSUInteger)u.arm - 1];
        putLEB(d, [self ref:arm.wireId]);
        if (!arm.type) { putByte(d, 32); putLEB(d, 0); return d; }
        putByte(d, (uint8_t)arm.type.wireKind);
        NSData *in = [self inner:u.value type:arm.type where:[where stringByAppendingFormat:@".%@", arm.name]];
        putLEB(d, in.length);
        [d appendData:in];
        return d;
    }
    case SPKArray: {
        NSArray *a = v;
        if (t.shape == SPArrayFixed && (long long)a.count != t.maxCount) SPRefuse(@"invalid_value: %@ fixed array count", where);
        if (t.shape == SPArrayCounted && ((long long)a.count > t.maxCount || (long long)a.count < t.minCount))
            SPRefuse(@"invalid_value: %@ count off its bound", where);
        if (t.total >= 0) {
            long long sum = 0;
            for (id x in a) if (x != [NSNull null]) sum += (long long)((NSData *)x).length;
            if (sum > t.total) SPRefuse(@"invalid_value: %@ past its total", where);
        }
        int ek = elemWireKind(t);
        putByte(d, (uint8_t)ek);
        putLEB(d, a.count);
        NSUInteger i = 0;
        for (id x in a) {
            NSString *w = [where stringByAppendingFormat:@"[%lu]", (unsigned long)i++];
            if (t.elemOptional) {
                NSMutableData *b = [NSMutableData data];
                if (x != [NSNull null]) {
                    SPCheckScalar(x, t.elem, w);
                    putLEB(b, [self ref:SP_VALUE_ID]);
                    putByte(b, 12);
                    putLEB(b, ((NSData *)x).length);
                    [b appendData:x];
                }
                putByte(b, 0);
                putLEB(d, b.length);
                [d appendData:b];
                continue;
            }
            [d appendData:[self element:x type:t.elem where:w]];
        }
        return d;
    }
    case SPKKeyed: {
        NSArray *a = v;
        NSMutableArray *present = [NSMutableArray array];
        for (NSUInteger i = 0; i < a.count; i++)
            if (!SPValueIsDefault(a[i], t.elem, nil)) [present addObject:@(i)];
        putByte(d, (uint8_t)t.elem.wireKind);
        putLEB(d, present.count);
        for (NSNumber *n in present) {
            NSUInteger i = n.unsignedIntegerValue;
            putLEB(d, [self ref:t.keyEnum.variants[i].wireId]);
            NSData *in = [self inner:a[i] type:t.elem where:[where stringByAppendingFormat:@"[%@]", t.keyEnum.variants[i].name]];
            putLEB(d, in.length);
            [d appendData:in];
        }
        return d;
    }
    case SPKMap: {
        NSArray<SPMapEntry *> *es = [self sortedEntries:v type:t];
        putByte(d, 13);
        putLEB(d, es.count);
        for (SPMapEntry *e in es) {
            NSMutableData *b = [NSMutableData data];
            if (!SPValueIsDefault(e.key, t.key, nil)) {
                putLEB(b, [self ref:SP_KEY_ID]);
                putByte(b, (uint8_t)t.key.wireKind);
                [b appendData:[self fieldPayload:e.key type:t.key where:where field:nil]];
            } else SPCheckScalar(e.key, t.key, where);
            BOOL writeValue = t.elemOptional ? e.value != [NSNull null] : !SPValueIsDefault(e.value, t.elem, nil);
            if (writeValue) {
                putLEB(b, [self ref:SP_VALUE_ID]);
                putByte(b, (uint8_t)t.elem.wireKind);
                [b appendData:[self fieldPayload:e.value type:t.elem where:where field:nil]];
            }
            putByte(b, 0);
            putLEB(d, b.length);
            [d appendData:b];
        }
        return d;
    }
    }
    return d;
}

// one array element (§3, array elements)
- (NSData *)element:(id)x type:(SPType *)e where:(NSString *)w {
    NSMutableData *d = [NSMutableData data];
    switch (e.kind) {
    case SPKStruct: case SPKString: {
        NSData *in = [self inner:x type:e where:w];
        putLEB(d, in.length);
        [d appendData:in];
        return d;
    }
    default:
        return [self inner:x type:e where:w];
    }
}
@end

NSData *SPSaveVariable(SPTableVal *root) {
    SPWriter *w = [SPWriter new];
    w.ids = [NSMutableArray array];
    w.refOf = [NSMutableDictionary dictionary];
    w.nodeIndex = [NSMapTable mapTableWithKeyOptions:NSPointerFunctionsObjectPointerPersonality valueOptions:NSPointerFunctionsStrongMemory];
    w.open = [NSMapTable mapTableWithKeyOptions:NSPointerFunctionsObjectPointerPersonality valueOptions:NSPointerFunctionsStrongMemory];
    w.nodes = [NSMutableArray array];
    w.root = root;
    [w numberBody:root];
    NSMutableData *out = [NSMutableData data];
    putByte(out, 1);
    [out appendData:[w body:root root:YES]];
    for (NSNumber *n in w.ids) putLE(out, n.unsignedLongLongValue, 8);
    putLE(out, w.ids.count, 8);
    if (root.decl.integrity) {
        uint32_t c = SPCrc32c(out.bytes, out.length);
        putLE(out, c, 4);
    }
    return out;
}

// ---------------------------------------------------------------------------
// Reader

@interface SPPending : NSObject
@property (nonatomic) uint64_t index;
@property (nonatomic, strong) SPType *type;
@property (nonatomic, copy) void (^set)(id);
@end
@implementation SPPending
@end

@interface SPReader : NSObject {
  @public
    const uint8_t *buf;
}
@property (nonatomic) NSUInteger bodyEnd;
@property (nonatomic, strong) NSArray<NSNumber *> *ids;
@property (nonatomic) NSUInteger maxRef;
@property (nonatomic, strong) SPUnit *unit;
@property (nonatomic, strong) SPDecl *rootDecl;
@property (nonatomic, strong) NSMutableArray<SPPending *> *pending;
@property (nonatomic, strong) NSMutableArray *records;      // SPTableVal / SPBlob / NSNull (unknown)
@property (nonatomic, strong) NSMutableArray<NSNumber *> *named; // indices named by any slot, skipped ones included
@property (nonatomic) BOOL sawNodeTable;
@property (nonatomic) NSInteger unknown;
@end

#define DAMAGE(...) SPRefuse(@"wire_damaged: " __VA_ARGS__)

@implementation SPReader

- (uint64_t)leb:(NSUInteger *)pos end:(NSUInteger)end {
    uint64_t v = 0;
    int shift = 0;
    NSUInteger start = *pos;
    for (int i = 0;; i++) {
        if (*pos >= end) DAMAGE(@"a number runs past its body");
        if (i >= 10) DAMAGE(@"a number past ten bytes");
        uint8_t b = buf[(*pos)++];
        if (i == 9 && (b & 0x7E)) DAMAGE(@"a number past 64 bits");
        v |= (uint64_t)(b & 0x7F) << shift;
        shift += 7;
        if (!(b & 0x80)) {
            if (b == 0 && *pos - start > 1) DAMAGE(@"a non-canonical number");
            return v;
        }
    }
}
- (uint8_t)byte:(NSUInteger *)pos end:(NSUInteger)end {
    if (*pos >= end) DAMAGE(@"a body cut short");
    return buf[(*pos)++];
}
- (uint64_t)ref:(NSUInteger *)pos end:(NSUInteger)end {
    uint64_t r = [self leb:pos end:end];
    if (r > self.ids.count) DAMAGE(@"a reference above the entry count");
    if (r) {
        if (r > self.maxRef + 1) SPRefuse(@"non_canonical: the id table is out of first-use order");
        if (r == self.maxRef + 1) self.maxRef++;
        uint64_t wid = self.ids[(NSUInteger)r - 1].unsignedLongLongValue;
        if (wid == SP_BUILD_VERSION_ID || wid == SP_VOCABULARY_ID) DAMAGE(@"a reserved id out of place");
    }
    return r;
}
- (uint64_t)idOf:(uint64_t)r { return self.ids[(NSUInteger)r - 1].unsignedLongLongValue; }
- (NSUInteger)len:(NSUInteger *)pos end:(NSUInteger)end {
    uint64_t L = [self leb:pos end:end];
    if (L > end - *pos) DAMAGE(@"a length runs past its body");
    return (NSUInteger)L;
}
- (sp_u128)scalar:(int)k at:(NSUInteger *)pos end:(NSUInteger)end {
    int w = kindWidth(k);
    if (end - *pos < (NSUInteger)w) DAMAGE(@"a scalar cut short");
    sp_u128 v = 0;
    for (int i = 0; i < w; i++) v |= ((sp_u128)buf[*pos + i]) << (8 * i);
    *pos += w;
    if (kindSigned(k) && w < 16 && (v >> (8 * w - 1)) & 1) v |= ~(sp_u128)0 << (8 * w);
    if (k == 1 && v > 1) DAMAGE(@"a bool byte other than 0 or 1");
    if (k == 10) { uint32_t b = (uint32_t)v; float f; memcpy(&f, &b, 4); if (!isfinite(f)) SPRefuse(@"non_finite_float"); }
    if (k == 11) { uint64_t b = (uint64_t)v; double f; memcpy(&f, &b, 8); if (!isfinite(f)) SPRefuse(@"non_finite_float"); }
    return v;
}
- (void)depth:(int)d {
    if (d > SP_MAX_DEPTH) SPRefuse(@"wire_damaged: nesting past %d", SP_MAX_DEPTH);
}

// --- generic walk: a value this reader cannot name, held to every rule
//     decidable from the wire alone (§4, §6.5)
- (void)skipKind:(int)k at:(NSUInteger *)pos end:(NSUInteger)end depth:(int)d {
    if (kindWidth(k)) { [self scalar:k at:pos end:end]; return; }
    switch (k) {
    case 17: [self.named addObject:@([self leb:pos end:end])]; return;
    case 30: [self ref:pos end:end]; return;
    case 12: {
        NSUInteger L = [self len:pos end:end];
        if (!SPValidUTF8([NSData dataWithBytesNoCopy:(void *)(buf + *pos) length:L freeWhenDone:NO], NO)) DAMAGE(@"ill-formed text");
        *pos += L; return;
    }
    case 13: { NSUInteger L = [self len:pos end:end]; [self skipBody:pos end:*pos + L depth:d + 1]; return; }
    case 14: { NSUInteger L = [self len:pos end:end]; [self skipArray:pos end:*pos + L depth:d + 1 keyed:NO]; return; }
    case 16: { NSUInteger L = [self len:pos end:end]; [self skipArray:pos end:*pos + L depth:d + 1 keyed:YES]; return; }
    case 31: { NSUInteger L = [self len:pos end:end]; *pos += L; return; }
    case 32: { NSUInteger L = [self len:pos end:end]; if (L) DAMAGE(@"kind 32 with a nonzero L"); return; }
    case 15: [self skipUnion:pos end:end depth:d]; return;
    }
    DAMAGE(@"a kind no reader can skip (%d)", k);
}
- (void)skipUnion:(NSUInteger *)pos end:(NSUInteger)end depth:(int)d {
    uint64_t r = [self ref:pos end:end];
    if (!r) return;
    int k = [self byte:pos end:end];
    NSUInteger L = [self len:pos end:end];
    NSUInteger e = *pos + L;
    [self skipArm:k at:pos end:e depth:d];
    if (*pos != e) DAMAGE(@"an arm whose L does not frame its kind");
}
- (void)skipArm:(int)k at:(NSUInteger *)pos end:(NSUInteger)e depth:(int)d {
    // the arm payload table (§3): the arm's L stands in for the field's own
    int w = kindWidth(k);
    if (w) { if (e - *pos != (NSUInteger)w) DAMAGE(@"a fixed-width arm whose L is not its width"); [self scalar:k at:pos end:e]; return; }
    switch (k) {
    case 13: [self skipBody:pos end:e depth:d + 1]; return;
    case 14: [self skipArray:pos end:e depth:d + 1 keyed:NO]; return;
    case 16: [self skipArray:pos end:e depth:d + 1 keyed:YES]; return;
    case 12:
        if (!SPValidUTF8([NSData dataWithBytesNoCopy:(void *)(buf + *pos) length:e - *pos freeWhenDone:NO], NO)) DAMAGE(@"ill-formed text");
        *pos = e; return;
    case 15: [self depth:d + 1]; [self skipUnion:pos end:e depth:d + 1]; return;
    case 17: [self.named addObject:@([self leb:pos end:e])]; return;
    case 30: [self ref:pos end:e]; return;
    case 31: *pos = e; return;
    case 32: if (e != *pos) DAMAGE(@"kind 32 with a nonzero L"); return;
    }
    DAMAGE(@"an arm of a kind no reader can skip");
}
- (void)skipBody:(NSUInteger *)pos end:(NSUInteger)end depth:(int)d {
    [self depth:d];
    NSMutableSet *seen = [NSMutableSet set];
    for (;;) {
        uint64_t r = [self ref:pos end:end];
        if (!r) break;
        uint64_t wid = [self idOf:r];
        if (wid == SP_NODE_TABLE_ID) DAMAGE(@"the node-table id in a nested body");
        if ([seen containsObject:@(wid)]) DAMAGE(@"one field id twice in a body");
        [seen addObject:@(wid)];
        int k = [self byte:pos end:end];
        [self skipKind:k at:pos end:end depth:d];
    }
    if (*pos != end) DAMAGE(@"a terminator before the end of its body");
}
- (void)skipArray:(NSUInteger *)pos end:(NSUInteger)end depth:(int)d keyed:(BOOL)keyed {
    [self depth:d];
    if (end - *pos < 2) DAMAGE(@"an array body too short for its header");
    int ek = buf[(*pos)++];
    if (keyed ? !keyedElemKindLegal(ek) : !arrayElemKindLegal(ek)) DAMAGE(@"an element kind off the closed list");
    uint64_t n = [self leb:pos end:end];
    if (n > end - *pos) DAMAGE(@"a count the body cannot cover");
    NSMutableSet *keys = [NSMutableSet set];
    for (uint64_t i = 0; i < n; i++) {
        if (keyed) {
            uint64_t r = [self ref:pos end:end];
            if (!r) DAMAGE(@"a keyed slot under reference 0");
            if ([keys containsObject:@(r)]) DAMAGE(@"one keyed slot twice");
            [keys addObject:@(r)];
            NSUInteger L = [self len:pos end:end];
            NSUInteger e = *pos + L;
            [self skipArm:ek at:pos end:e depth:d];
            if (*pos != e) DAMAGE(@"a keyed element whose L does not frame its kind");
            continue;
        }
        [self skipElement:ek at:pos end:end depth:d];
    }
    if (*pos != end) DAMAGE(@"bytes left inside an array after its elements");
}
- (void)skipElement:(int)ek at:(NSUInteger *)pos end:(NSUInteger)end depth:(int)d {
    if (ek == 13) { NSUInteger L = [self len:pos end:end]; [self skipBody:pos end:*pos + L depth:d + 1]; return; }
    if (ek == 15) { [self skipUnion:pos end:end depth:d]; return; }
    [self skipKind:ek at:pos end:end depth:d];
}

// --- declared reads
- (id)readType:(SPType *)t wireKind:(int)k at:(NSUInteger *)pos end:(NSUInteger)end depth:(int)d def:(id)def where:(NSString *)where {
    // the field's own framing, then its inner value
    switch (t.kind) {
    case SPKString: case SPKStruct: case SPKArray: case SPKKeyed: case SPKMap: case SPKBytes: {
        NSUInteger L = [self len:pos end:end];
        NSUInteger e = *pos + L;
        id v = [self innerType:t wireKind:k at:pos end:e depth:d def:def where:where];
        if (*pos != e) DAMAGE(@"a payload that does not fill its L");
        return v;
    }
    default:
        return [self innerType:t wireKind:k at:pos end:end depth:d def:def where:where];
    }
}

- (id)checkedScalar:(SPType *)t raw:(sp_u128)raw wireKind:(int)k where:(NSString *)where def:(id)def {
    switch (t.kind) {
    case SPKFloat: {
        double x;
        if (k == 10) { uint32_t b = (uint32_t)raw; float f; memcpy(&f, &b, 4); x = f; }
        else { uint64_t b = (uint64_t)raw; memcpy(&x, &b, 8); }
        if (t.hasRange) {
            if (x < t.fmin || x > t.fmax) SPRefuse(@"out_of_range: %@", where);
            if (x == 0) x = 0.0; // -0 lands as +0
        }
        return [SPFloat d:x];
    }
    case SPKInt: case SPKInt128: case SPKFixed:
        if ((t.kind != SPKInt128 || t.hasRange) && !rangeHolds(raw, t)) SPRefuse(@"out_of_range: %@", where);
        return [SPInt u:raw];
    case SPKBits:
        if (raw > (sp_u128)t.imax) SPRefuse(@"out_of_range: %@", where);
        return [SPInt u:raw];
    default:
        return [SPInt u:raw];
    }
}

- (id)innerType:(SPType *)t wireKind:(int)k at:(NSUInteger *)pos end:(NSUInteger)end depth:(int)d def:(id)def where:(NSString *)where {
    switch (t.kind) {
    case SPKBool: case SPKInt: case SPKBits: case SPKFixed: case SPKInt128: case SPKTimestamp: case SPKFlags: case SPKFloat: {
        sp_u128 raw = [self scalar:k at:pos end:end];
        return [self checkedScalar:t raw:raw wireKind:k where:where def:def];
    }
    case SPKEnum: {
        uint64_t r = [self ref:pos end:end];
        if (!r) return [SPInt u:0];
        NSInteger vi = [t.decl variantIndexForWireId:[self idOf:r]];
        if (!vi) { self.unknown++; return [SPInt u:0]; }
        if (t.decl.variants[(NSUInteger)vi - 1].deprecated) return def ?: [SPInt u:0];
        return [SPInt u:(sp_u128)vi];
    }
    case SPKString: {
        NSData *s = [NSData dataWithBytes:buf + *pos length:end - *pos];
        if (!SPValidUTF8(s, NO)) DAMAGE(@"ill-formed text at %@", where);
        if (t.cap >= 0 && (long long)s.length > t.cap) SPRefuse(@"length_over_bound: %@", where);
        *pos = end;
        return s;
    }
    case SPKBytes: {
        [self depth:d + 1];
        if (end - *pos < 2) DAMAGE(@"an array body too short for its header");
        int ek = buf[(*pos)++];
        if (!arrayElemKindLegal(ek)) DAMAGE(@"an element kind off the closed list");
        if (ek != 6) SPRefuse(@"kind_mismatch: %@", where);
        uint64_t n = [self leb:pos end:end];
        if (n != end - *pos) DAMAGE(@"a byte count that does not fill its body");
        if ((long long)n > t.cap) SPRefuse(@"length_over_bound: %@", where);
        NSData *b = [NSData dataWithBytes:buf + *pos length:(NSUInteger)n];
        *pos = end;
        return b;
    }
    case SPKStruct: return [self body:t.decl at:pos end:end depth:d + 1 root:NO];
    case SPKPointer: case SPKBlobBytes: case SPKBlobString: {
        uint64_t idx = [self leb:pos end:end];
        if (!idx) return [NSNull null];
        SPPending *p = [SPPending new];
        p.index = idx; p.type = t;
        [self.pending addObject:p];
        [self.named addObject:@(idx)];
        return p;
    }
    case SPKUnion: return [self unionOf:t at:pos end:end depth:d where:where];
    case SPKArray: return [self arrayOf:t at:pos end:end depth:d + 1 where:where];
    case SPKKeyed: return [self keyedOf:t at:pos end:end depth:d + 1 where:where];
    case SPKMap: return [self mapOf:t at:pos end:end depth:d + 1 where:where];
    }
    DAMAGE(@"unreachable");
}

- (void)checkKind:(int)declared wire:(int)k where:(NSString *)where {
    if (kindAccepts(declared, k)) return;
    if (k == 0 || k == 33 || k == 34 || k == 35 || k > 36) DAMAGE(@"a kind no reader can skip at %@", where);
    SPRefuse(@"kind_mismatch: %@ (declared %d, wire %d)", where, declared, k);
}

- (SPUnionVal *)unionOf:(SPType *)t at:(NSUInteger *)pos end:(NSUInteger)end depth:(int)d where:(NSString *)where {
    SPUnionVal *u = [SPUnionVal new];
    u.value = [NSNull null];
    uint64_t r = [self ref:pos end:end];
    if (!r) return u;
    int k = [self byte:pos end:end];
    NSUInteger L = [self len:pos end:end];
    NSUInteger e = *pos + L;
    uint64_t wid = [self idOf:r];
    NSInteger ai = [t.decl fieldIndexForWireId:wid];
    if (ai < 0) {
        int sd = d;
        if (k == 13 || k == 14 || k == 16 || k == 15) sd = d + 1;
        [self depth:sd];
        [self skipArm:k at:pos end:e depth:sd];
        if (*pos != e) DAMAGE(@"an arm whose L does not frame its kind");
        self.unknown++;
        return u;
    }
    SPField *arm = t.decl.fields[(NSUInteger)ai];
    NSString *w = [where stringByAppendingFormat:@".%@", arm.name];
    if (!arm.type) {
        if (k != 32) {
            if (k == 0 || k > 36) DAMAGE(@"unskippable kind");
            SPRefuse(@"kind_mismatch: %@", w);
        }
        if (L) DAMAGE(@"kind 32 with a nonzero L");
        u.arm = ai + 1;
        return u;
    }
    if (k == 32 && L) DAMAGE(@"kind 32 with a nonzero L");
    [self checkKind:arm.type.wireKind wire:k where:w];
    int ad = d;
    if (arm.type.isStructural || arm.type.kind == SPKUnion) { ad = d + 1; [self depth:ad]; }
    id v;
    int wdth = kindWidth(k);
    if (wdth && L != (NSUInteger)wdth) DAMAGE(@"a fixed-width arm whose L is not its width");
    switch (arm.type.kind) {
    case SPKUnion: v = [self unionOf:arm.type at:pos end:e depth:ad where:w]; break;
    case SPKStruct: v = [self body:arm.type.decl at:pos end:e depth:ad root:NO]; break;
    case SPKArray: v = [self arrayOf:arm.type at:pos end:e depth:ad where:w]; break;
    case SPKKeyed: v = [self keyedOf:arm.type at:pos end:e depth:ad where:w]; break;
    case SPKMap: v = [self mapOf:arm.type at:pos end:e depth:ad where:w]; break;
    default: v = [self innerType:arm.type wireKind:k at:pos end:e depth:ad def:arm.defaultValue where:w]; break;
    }
    if (*pos != e) DAMAGE(@"bytes left inside an arm");
    u.arm = ai + 1;
    u.value = v;
    if ([v isKindOfClass:[SPPending class]]) {
        __weak SPUnionVal *wu = u;
        ((SPPending *)v).set = ^(id x) { wu.value = x; };
    }
    return u;
}

- (NSMutableArray *)arrayOf:(SPType *)t at:(NSUInteger *)pos end:(NSUInteger)end depth:(int)d where:(NSString *)where {
    [self depth:d];
    if (end - *pos < 2) DAMAGE(@"an array body too short for its header");
    int ek = buf[(*pos)++];
    if (!arrayElemKindLegal(ek)) DAMAGE(@"an element kind off the closed list");
    int dk = elemWireKind(t);
    [self checkKind:dk wire:ek where:where];
    uint64_t n = [self leb:pos end:end];
    int w = kindWidth(ek);
    if (w ? n * (uint64_t)w != end - *pos : n > end - *pos) DAMAGE(@"a count the body cannot cover");
    if (t.shape != SPArrayUnbounded && (long long)n > t.maxCount) SPRefuse(@"count_over_bound: %@", where);
    if (t.shape == SPArrayCounted && (long long)n < t.minCount) SPRefuse(@"count_over_bound: %@ below its minimum", where);
    NSMutableArray *a = [NSMutableArray arrayWithCapacity:(NSUInteger)n];
    long long sum = 0;
    for (uint64_t i = 0; i < n; i++) {
        NSString *ew = [where stringByAppendingFormat:@"[%llu]", i];
        id v;
        if (t.elemOptional) {
            NSUInteger L = [self len:pos end:end];
            NSUInteger e = *pos + L;
            [self depth:d + 1];
            v = [NSNull null];
            NSMutableSet *seen = [NSMutableSet set];
            for (;;) {
                uint64_t r = [self ref:pos end:e];
                if (!r) break;
                uint64_t wid = [self idOf:r];
                if ([seen containsObject:@(wid)]) DAMAGE(@"one field id twice");
                [seen addObject:@(wid)];
                int k = [self byte:pos end:e];
                if (wid == SP_VALUE_ID) {
                    [self checkKind:12 wire:k where:ew];
                    v = [self readType:t.elem wireKind:k at:pos end:e depth:d + 1 def:nil where:ew];
                } else {
                    if (wid == SP_NODE_TABLE_ID) DAMAGE(@"node-table id nested");
                    [self skipKind:k at:pos end:e depth:d + 1];
                    self.unknown++;
                }
            }
            if (*pos != e) DAMAGE(@"a terminator before the end of its body");
        } else if (ek == 13) {
            NSUInteger L = [self len:pos end:end];
            NSUInteger e = *pos + L;
            v = [self innerType:t.elem wireKind:ek at:pos end:e depth:d def:nil where:ew];
            if (*pos != e) DAMAGE(@"an element that does not fill its L");
        } else if (ek == 12) {
            v = [self readType:t.elem wireKind:ek at:pos end:end depth:d def:nil where:ew];
        } else if (ek == 15) {
            v = [self unionOf:t.elem at:pos end:end depth:d where:ew];
        } else {
            v = [self innerType:t.elem wireKind:ek at:pos end:end depth:d def:nil where:ew];
        }
        if ([v isKindOfClass:[NSData class]]) sum += (long long)((NSData *)v).length;
        [a addObject:v];
        if ([v isKindOfClass:[SPPending class]]) {
            NSUInteger idx = a.count - 1;
            __weak NSMutableArray *wa = a;
            ((SPPending *)v).set = ^(id x) { wa[idx] = x; };
        }
    }
    if (*pos != end) DAMAGE(@"bytes left inside an array after its elements");
    if (t.total >= 0 && sum > t.total) SPRefuse(@"length_over_bound: %@ past its total", where);
    if (t.shape == SPArrayFixed)
        while ((long long)a.count < t.maxCount) [a addObject:SPNewValue(t.elem)];
    return a;
}

- (NSMutableArray *)keyedOf:(SPType *)t at:(NSUInteger *)pos end:(NSUInteger)end depth:(int)d where:(NSString *)where {
    [self depth:d];
    if (end - *pos < 2) DAMAGE(@"a keyed body too short for its header");
    int ek = buf[(*pos)++];
    if (!keyedElemKindLegal(ek)) DAMAGE(@"an element kind off the closed list");
    [self checkKind:t.elem.wireKind wire:ek where:where];
    uint64_t n = [self leb:pos end:end];
    if (n > end - *pos) DAMAGE(@"a count the body cannot cover");
    NSMutableArray *a = SPNewValue(t);
    NSMutableSet *keys = [NSMutableSet set];
    for (uint64_t i = 0; i < n; i++) {
        uint64_t r = [self ref:pos end:end];
        if (!r) DAMAGE(@"a keyed slot under reference 0");
        uint64_t wid = [self idOf:r];
        if ([keys containsObject:@(wid)]) DAMAGE(@"one keyed slot twice");
        [keys addObject:@(wid)];
        NSUInteger L = [self len:pos end:end];
        NSUInteger e = *pos + L;
        NSInteger vi = [t.keyEnum variantIndexForWireId:wid];
        if (!vi || t.keyEnum.variants[(NSUInteger)vi - 1].deprecated) {
            [self skipArm:ek at:pos end:e depth:d];
            if (*pos != e) DAMAGE(@"a keyed element whose L does not frame its kind");
            self.unknown++;
            continue;
        }
        int w = kindWidth(ek);
        if (w && L != (NSUInteger)w) DAMAGE(@"a keyed element whose L does not frame its kind");
        NSString *ew = [where stringByAppendingFormat:@"[%@]", t.keyEnum.variants[(NSUInteger)vi - 1].name];
        id v = [self innerType:t.elem wireKind:ek at:pos end:e depth:d def:nil where:ew];
        if (*pos != e) DAMAGE(@"a keyed element whose L does not frame its kind");
        a[(NSUInteger)vi - 1] = v;
    }
    if (*pos != end) DAMAGE(@"bytes left inside a keyed body");
    return a;
}

- (NSMutableArray *)mapOf:(SPType *)t at:(NSUInteger *)pos end:(NSUInteger)end depth:(int)d where:(NSString *)where {
    [self depth:d];
    if (end - *pos < 2) DAMAGE(@"a map body too short for its header");
    int ek = buf[(*pos)++];
    if (!arrayElemKindLegal(ek)) DAMAGE(@"an element kind off the closed list");
    [self checkKind:13 wire:ek where:where];
    uint64_t n = [self leb:pos end:end];
    if (n > end - *pos) DAMAGE(@"a count the body cannot cover");
    NSMutableArray *a = [NSMutableArray array];
    id prev = nil;
    for (uint64_t i = 0; i < n; i++) {
        NSUInteger L = [self len:pos end:end];
        NSUInteger e = *pos + L;
        [self depth:d + 1];
        SPMapEntry *me = [SPMapEntry new];
        me.key = SPNewValue(t.key);
        me.value = t.elemOptional ? (id)[NSNull null] : SPNewValue(t.elem);
        NSMutableSet *seen = [NSMutableSet set];
        for (;;) {
            uint64_t r = [self ref:pos end:e];
            if (!r) break;
            uint64_t wid = [self idOf:r];
            if (wid == SP_NODE_TABLE_ID) DAMAGE(@"node-table id nested");
            if ([seen containsObject:@(wid)]) DAMAGE(@"one field id twice");
            [seen addObject:@(wid)];
            int k = [self byte:pos end:e];
            if (wid == SP_KEY_ID) {
                [self checkKind:t.key.wireKind wire:k where:where];
                me.key = [self readType:t.key wireKind:k at:pos end:e depth:d + 1 def:nil where:[where stringByAppendingString:@".key"]];
            } else if (wid == SP_VALUE_ID) {
                [self checkKind:t.elem.wireKind wire:k where:where];
                id v = [self readType:t.elem wireKind:k at:pos end:e depth:d + 1 def:nil where:[where stringByAppendingString:@".value"]];
                me.value = v;
                if ([v isKindOfClass:[SPPending class]]) {
                    __weak SPMapEntry *wm = me;
                    ((SPPending *)v).set = ^(id x) { wm.value = x; };
                }
            } else {
                [self skipKind:k at:pos end:e depth:d + 1];
                self.unknown++;
            }
        }
        if (*pos != e) DAMAGE(@"a terminator before the end of its entry");
        if (prev && SPCompareKeys(prev, me.key, t.key) >= 0) DAMAGE(@"a map key equal to or below the one before it");
        prev = me.key;
        [a addObject:me];
    }
    if (*pos != end) DAMAGE(@"bytes left inside a map body");
    return a;
}

- (SPTableVal *)body:(SPDecl *)decl at:(NSUInteger *)pos end:(NSUInteger)end depth:(int)d root:(BOOL)isRoot {
    [self depth:d];
    SPTableVal *t = SPNewTable(decl);
    NSMutableSet *seen = [NSMutableSet set];
    for (;;) {
        uint64_t r = [self ref:pos end:end];
        if (!r) break;
        uint64_t wid = [self idOf:r];
        if ([seen containsObject:@(wid)]) DAMAGE(@"one field id twice in a body");
        [seen addObject:@(wid)];
        int k = [self byte:pos end:end];
        if (wid == SP_NODE_TABLE_ID) {
            if (!isRoot) DAMAGE(@"the node-table id in a nested body");
            if (k != 12) DAMAGE(@"the node table under another kind");
            NSUInteger L = [self len:pos end:end];
            [self nodeTable:*pos end:*pos + L];
            *pos += L;
            if (*pos >= end || buf[*pos] != 0) SPRefuse(@"non_canonical: the node table is not the root body's last field");
            continue;
        }
        NSInteger fi = [decl fieldIndexForWireId:wid];
        if (fi < 0) {
            [self skipKind:k at:pos end:end depth:d];
            self.unknown++;
            continue;
        }
        SPField *f = decl.fields[(NSUInteger)fi];
        NSString *w = [NSString stringWithFormat:@"%@.%@", decl.name, f.name];
        [self checkKind:f.type.wireKind wire:k where:w];
        id v = [self readType:f.type wireKind:k at:pos end:end depth:d def:f.defaultValue where:w];
        t.values[(NSUInteger)fi] = v;
        if (f.optional) t.present[(NSUInteger)fi] = @YES;
        if ([v isKindOfClass:[SPPending class]]) {
            NSUInteger idx = (NSUInteger)fi;
            __weak SPTableVal *wt = t;
            ((SPPending *)v).set = ^(id x) { wt.values[idx] = x; };
        }
    }
    if (*pos != end) DAMAGE(@"a terminator before the end of its body");
    for (NSUInteger i = 0; i < decl.fields.count; i++) {
        SPField *f = decl.fields[i];
        if (f.required && ![seen containsObject:@(f.wireId)]) SPRefuse(@"missing_required: %@.%@", decl.name, f.name);
    }
    return t;
}

- (void)nodeTable:(NSUInteger)start end:(NSUInteger)end {
    if (self.sawNodeTable) DAMAGE(@"node table twice");
    self.sawNodeTable = YES;
    NSUInteger pos = start;
    uint64_t count = [self leb:&pos end:end];
    if (count == 0) SPRefuse(@"non_canonical: an empty node table");
    uint64_t scanned = 0;
    while (pos < end) {
        uint64_t r = [self ref:&pos end:end];
        if (!r) DAMAGE(@"a node record under type reference 0");
        uint64_t wid = [self idOf:r];
        NSUInteger L = [self len:&pos end:end];
        NSUInteger e = pos + L;
        scanned++;
        if (wid == SPFnv1a64Str(@"bytes") || wid == SPFnv1a64Str(@"string")) {
            BOOL isStr = wid == SPFnv1a64Str(@"string");
            NSData *data = [NSData dataWithBytes:buf + pos length:L];
            if (isStr && !SPValidUTF8(data, NO)) DAMAGE(@"a *string blob that is not text");
            if (L > 0xFFFFFFFFull) SPRefuse(@"blob_over_size_cap");
            SPBlob *b = [SPBlob new];
            b.data = data; b.isString = isStr;
            [self.records addObject:b];
            pos = e;
            continue;
        }
        SPDecl *decl = self.unit.tablesByWireId[@(wid)];
        if (!decl) {
            [self skipBody:&pos end:e depth:0];
            self.unknown++;
            [self.records addObject:[NSNull null]];
            continue;
        }
        [self.records addObject:[self body:decl at:&pos end:e depth:0 root:NO]];
        if (pos != e) DAMAGE(@"a node record that does not fill its length");
    }
    if (scanned != count) DAMAGE(@"a node count the scan does not match");
}

// --- the graph: resolve every index, then check it (§3.1, §6.5)
- (void)resolveGraphWithRoot:(SPTableVal *)root {
    NSUInteger nrec = self.records.count;
    for (NSNumber *n in self.named) {
        uint64_t i = n.unsignedLongLongValue;
        if (i == 1) DAMAGE(@"a pointer names the root");
        if (i > nrec + 1) DAMAGE(@"a node index above the node count");
    }
    // every record is named by some slot; a blob by one slot only
    NSMutableDictionary<NSNumber *, NSNumber *> *uses = [NSMutableDictionary dictionary];
    for (NSNumber *n in self.named) uses[n] = @(uses[n].integerValue + 1);
    for (NSUInteger i = 0; i < nrec; i++) {
        NSNumber *key = @(i + 2);
        if (!uses[key]) DAMAGE(@"a node record no pointer reaches");
        if ([self.records[i] isKindOfClass:[SPBlob class]] && uses[key].integerValue > 1) SPRefuse(@"blob_shared");
    }
    for (SPPending *p in self.pending) {
        id rec = self.records[(NSUInteger)p.index - 2];
        id target = [NSNull null];
        if (p.type.kind == SPKPointer) {
            if ([rec isKindOfClass:[SPBlob class]]) DAMAGE(@"a wrong-type pointer target");
            if ([rec isKindOfClass:[SPTableVal class]]) {
                if (((SPTableVal *)rec).decl != p.type.decl) DAMAGE(@"a wrong-type pointer target");
                target = rec;
            }
        } else {
            BOOL wantString = p.type.kind == SPKBlobString;
            if (![rec isKindOfClass:[SPBlob class]] || ((SPBlob *)rec).isString != wantString) DAMAGE(@"a wrong-type pointer target");
            target = rec;
        }
        if (!p.set) DAMAGE(@"internal: an unplaced pointer");
        p.set(target);
    }
    // cycle check over stored table pointers: white/grey/black, iterative
    NSMapTable *colour = [NSMapTable mapTableWithKeyOptions:NSPointerFunctionsObjectPointerPersonality valueOptions:NSPointerFunctionsStrongMemory];
    NSMutableArray *starts = [NSMutableArray arrayWithObject:root];
    for (id r in self.records) if ([r isKindOfClass:[SPTableVal class]]) [starts addObject:r];
    for (SPTableVal *s in starts) {
        if ([colour objectForKey:s]) continue;
        NSMutableArray *stack = [NSMutableArray arrayWithObject:@[ s, [[self class] edgesOf:s] ]];
        [colour setObject:@1 forKey:s];
        NSMutableArray *iters = [NSMutableArray arrayWithObject:@0];
        while (stack.count) {
            NSArray *top = stack.lastObject;
            NSArray *edges = top[1];
            NSUInteger k = [iters.lastObject unsignedIntegerValue];
            if (k >= edges.count) {
                [colour setObject:@2 forKey:top[0]];
                [stack removeLastObject];
                [iters removeLastObject];
                continue;
            }
            iters[iters.count - 1] = @(k + 1);
            id nx = edges[k];
            NSNumber *c = [colour objectForKey:nx];
            if (c.intValue == 1) DAMAGE(@"a cycle");
            if (c.intValue == 2) continue;
            [colour setObject:@1 forKey:nx];
            [stack addObject:@[ nx, [[self class] edgesOf:nx] ]];
            [iters addObject:@0];
        }
    }
}

// every non-null table pointer a node's by-value closure holds
+ (NSArray *)edgesOf:(SPTableVal *)t {
    NSMutableArray *out = [NSMutableArray array];
    [self collect:t type:nil into:out];
    return out;
}
+ (void)collect:(id)v type:(SPType *)t into:(NSMutableArray *)out {
    if (!t) {
        SPTableVal *tv = v;
        for (NSUInteger i = 0; i < tv.decl.fields.count; i++) [self collect:tv.values[i] type:tv.decl.fields[i].type into:out];
        return;
    }
    switch (t.kind) {
    case SPKPointer: if ([v isKindOfClass:[SPTableVal class]]) [out addObject:v]; return;
    case SPKStruct: [self collect:v type:nil into:out]; return;
    case SPKUnion: {
        SPUnionVal *u = v;
        if (u.arm) { SPField *a = t.decl.fields[(NSUInteger)u.arm - 1]; if (a.type) [self collect:u.value type:a.type into:out]; }
        return;
    }
    case SPKArray: case SPKKeyed:
        if (t.kind == SPKArray && t.elemOptional) return;
        for (id x in (NSArray *)v) [self collect:x type:t.elem into:out];
        return;
    case SPKMap:
        for (SPMapEntry *e in (NSArray *)v) if (e.value != [NSNull null] || !t.elemOptional) [self collect:e.value type:t.elem into:out];
        return;
    default: return;
    }
}
@end

SPTableVal *SPLoadVariable(NSData *wire, SPDecl *root, SPUnit *unit) {
    if (root.fixed && wire.length && ((const uint8_t *)wire.bytes)[0] == 1) SPRefuse(@"previous_form: form 1 at a declared fixed root");
    return SPLoadForm1(wire, root, unit);
}

SPTableVal *SPLoadForm1(NSData *wire, SPDecl *root, SPUnit *unit) {
    const uint8_t *p = wire.bytes;
    NSUInteger n = wire.length;
    if (n == 0) DAMAGE(@"a buffer of no bytes");
    if (p[0] == 2) SPRefuse(@"message_form_as_file");
    if (p[0] == 3) SPRefuse(root.fixed ? @"fixed_form_not_carried" : @"wrong_form: a fixed-form file at a variable root");
    if (p[0] != 1) SPRefuse(@"newer_form");
    if (root.integrity) {
        if (n < 5) DAMAGE(@"no room for the integrity trailer");
        uint32_t want = (uint32_t)p[n - 4] | (uint32_t)p[n - 3] << 8 | (uint32_t)p[n - 2] << 16 | (uint32_t)p[n - 1] << 24;
        if (SPCrc32c(p, n - 4) != want) SPRefuse(@"integrity_mismatch");
        n -= 4;
    }
    if (n < 9) DAMAGE(@"too short for an id table");
    uint64_t count = 0;
    for (int i = 0; i < 8; i++) count |= (uint64_t)p[n - 8 + i] << (8 * i);
    if (count > SP_MAX_IDS) DAMAGE(@"an id table past 4096 entries");
    if (8 * count + 8 > n - 1) DAMAGE(@"an id table past the front of the file");
    NSMutableArray *ids = [NSMutableArray array];
    NSMutableSet *distinct = [NSMutableSet set];
    NSUInteger base = n - 8 - (NSUInteger)(8 * count);
    for (uint64_t i = 0; i < count; i++) {
        uint64_t v = 0;
        for (int k = 0; k < 8; k++) v |= (uint64_t)p[base + 8 * i + k] << (8 * k);
        if ([distinct containsObject:@(v)]) DAMAGE(@"one id twice in the table");
        [distinct addObject:@(v)];
        [ids addObject:@(v)];
    }
    SPReader *r = [SPReader new];
    r->buf = p;
    r.bodyEnd = base;
    r.ids = ids;
    r.unit = unit;
    r.rootDecl = root;
    r.pending = [NSMutableArray array];
    r.records = [NSMutableArray array];
    r.named = [NSMutableArray array];
    NSUInteger pos = 1;
    SPTableVal *t = [r body:root at:&pos end:base depth:0 root:YES];
    if (r.maxRef != count) SPRefuse(@"non_canonical: an id-table entry no reference uses");
    [r resolveGraphWithRoot:t];
    return t;
}
