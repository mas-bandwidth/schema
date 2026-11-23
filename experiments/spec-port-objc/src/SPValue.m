// SPValue.m — values, construction and defaults (SPEC §4.2).

#import "SPValue.h"
#include <math.h>

@implementation SPInt
+ (SPInt *)u:(sp_u128)v { SPInt *x = [SPInt new]; x->raw = v; return x; }
+ (SPInt *)i:(sp_i128)v { SPInt *x = [SPInt new]; x->raw = (sp_u128)v; return x; }
- (sp_i128)sval { return (sp_i128)raw; }
- (id)copyWithZone:(NSZone *)z { return self; }
@end

@implementation SPFloat
+ (SPFloat *)d:(double)v { SPFloat *x = [SPFloat new]; x->d = v; return x; }
- (id)copyWithZone:(NSZone *)z { return self; }
@end

@implementation SPTableVal
@end
@implementation SPUnionVal
@end
@implementation SPBlob
@end
@implementation SPMapEntry
@end

BOOL SPTypeSigned(SPType *t) {
    switch (t.kind) {
    case SPKInt: case SPKFixed: case SPKInt128: return t.isSigned;
    case SPKTimestamp: return YES;
    default: return NO;
    }
}

sp_i128 SPIntSigned(SPInt *v, SPType *t) {
    // every kind but uint128 fits a signed 128-bit value
    return (sp_i128)v->raw;
}

BOOL SPIntEqual(id a, id b) {
    return [a isKindOfClass:[SPInt class]] && [b isKindOfClass:[SPInt class]] && ((SPInt *)a)->raw == ((SPInt *)b)->raw;
}

BOOL SPFloatBitsEqual(id a, id b) {
    double x = ((SPFloat *)a)->d, y = ((SPFloat *)b)->d;
    return memcmp(&x, &y, sizeof x) == 0;
}

int SPCompareKeys(id a, id b, SPType *kt) {
    if (kt.kind == SPKString) {
        NSData *x = a, *y = b;
        NSUInteger n = MIN(x.length, y.length);
        int c = n ? memcmp(x.bytes, y.bytes, n) : 0;
        if (c) return c < 0 ? -1 : 1;
        return x.length < y.length ? -1 : x.length > y.length ? 1 : 0;
    }
    SPInt *x = a, *y = b;
    if (SPTypeSigned(kt)) {
        sp_i128 p = (sp_i128)x->raw, q = (sp_i128)y->raw;
        return p < q ? -1 : p > q ? 1 : 0;
    }
    return x->raw < y->raw ? -1 : x->raw > y->raw ? 1 : 0;
}

BOOL SPValidUTF8(NSData *data, BOOL allowZero) {
    const uint8_t *p = data.bytes;
    NSUInteger n = data.length, i = 0;
    while (i < n) {
        uint8_t c = p[i];
        if (c < 0x80) {
            if (c == 0 && !allowZero) return NO;
            i++; continue;
        }
        int len; uint32_t cp, minv;
        if ((c & 0xE0) == 0xC0) { len = 2; cp = c & 0x1F; minv = 0x80; }
        else if ((c & 0xF0) == 0xE0) { len = 3; cp = c & 0x0F; minv = 0x800; }
        else if ((c & 0xF8) == 0xF0) { len = 4; cp = c & 0x07; minv = 0x10000; }
        else return NO;
        if (i + len > n) return NO;
        for (int k = 1; k < len; k++) {
            if ((p[i + k] & 0xC0) != 0x80) return NO;
            cp = (cp << 6) | (p[i + k] & 0x3F);
        }
        if (cp < minv || cp > 0x10FFFF || (cp >= 0xD800 && cp <= 0xDFFF)) return NO;
        i += len;
    }
    return YES;
}

BOOL SPGuardsHold(SPTableVal *t, SPField *f) {
    for (SPGuard *g in f.guards) {
        SPInt *v = t.values[(NSUInteger)g.fieldIndex];
        BOOL on = v->raw != 0;
        if (on != g.wantTrue) return NO;
    }
    return YES;
}

id SPNewValue(SPType *t) {
    switch (t.kind) {
    case SPKInt: case SPKBits: case SPKBool: case SPKFixed: case SPKInt128:
    case SPKTimestamp: case SPKEnum: case SPKFlags:
        return [SPInt u:0];
    case SPKFloat: return [SPFloat d:0.0];
    case SPKString: case SPKBytes: return [NSData data];
    case SPKStruct: return SPNewTable(t.decl);
    case SPKUnion: { SPUnionVal *u = [SPUnionVal new]; u.value = [NSNull null]; return u; }
    case SPKPointer: case SPKBlobBytes: case SPKBlobString: return [NSNull null];
    case SPKArray: {
        NSMutableArray *a = [NSMutableArray array];
        long long n = t.shape == SPArrayUnbounded ? 0 : t.minCount;
        for (long long i = 0; i < n; i++) [a addObject:t.elemOptional ? (id)[NSNull null] : SPNewValue(t.elem)];
        return a;
    }
    case SPKKeyed: {
        NSMutableArray *a = [NSMutableArray array];
        for (NSUInteger i = 0; i < t.keyEnum.variants.count; i++) [a addObject:SPNewValue(t.elem)];
        return a;
    }
    case SPKMap: return [NSMutableArray array];
    }
    return [NSNull null];
}

SPTableVal *SPNewTable(SPDecl *d) {
    SPTableVal *v = [SPTableVal new];
    v.decl = d;
    v.values = [NSMutableArray arrayWithCapacity:d.fields.count];
    v.present = [NSMutableArray arrayWithCapacity:d.fields.count];
    for (SPField *f in d.fields) {
        if (!f.defaultValue) SPResolveDefault(f); // a nested default before its own pass
        [v.values addObject:SPCopyValue(f.defaultValue)];
        [v.present addObject:@NO];
    }
    return v;
}

id SPCopyValue(id v) {
    if ([v isKindOfClass:[SPTableVal class]]) {
        SPTableVal *s = v, *c = [SPTableVal new];
        c.decl = s.decl;
        c.values = [NSMutableArray arrayWithCapacity:s.values.count];
        for (id x in s.values) [c.values addObject:SPCopyValue(x)];
        c.present = [s.present mutableCopy];
        return c;
    }
    if ([v isKindOfClass:[SPUnionVal class]]) {
        SPUnionVal *s = v, *c = [SPUnionVal new];
        c.arm = s.arm; c.value = SPCopyValue(s.value);
        return c;
    }
    if ([v isKindOfClass:[NSArray class]]) {
        NSMutableArray *c = [NSMutableArray array];
        for (id x in (NSArray *)v) [c addObject:SPCopyValue(x)];
        return c;
    }
    if ([v isKindOfClass:[SPMapEntry class]]) {
        SPMapEntry *s = v, *c = [SPMapEntry new];
        c.key = s.key; c.value = SPCopyValue(s.value);
        return c;
    }
    return v; // immutable: SPInt, SPFloat, NSData, NSNull, SPBlob (identity kept)
}

static SPNum *needNum(id lit, SPField *f) {
    if (![lit isKindOfClass:[SPNum class]]) SPSchemaFail(@"%@: default is not a number", f.name);
    return lit;
}

void SPResolveDefault(SPField *f) {
    SPType *t = f.type;
    if (!t) { f.defaultValue = [NSNull null]; return; }
    id lit = f.defaultLiteral;
    if (!lit) { f.defaultValue = SPNewValue(t); return; }
    switch (t.kind) {
    case SPKInt: case SPKBits: case SPKInt128: case SPKTimestamp: {
        SPNum *n = needNum(lit, f);
        if (n.isFloat) SPSchemaFail(@"%@: an integer default is required", f.name);
        f.defaultValue = [SPInt i:n.i];
        return;
    }
    case SPKFixed: {
        SPNum *n = needNum(lit, f);
        sp_i128 raw;
        if (n.isFloat) {
            double s = ldexp(n.d, t.fixedF);
            if (s != floor(s)) SPSchemaFail(@"%@: a fixed default finer than its F", f.name);
            raw = (sp_i128)s;
        } else raw = n.i << t.fixedF;
        f.defaultValue = [SPInt i:raw];
        return;
    }
    case SPKFloat: {
        SPNum *n = needNum(lit, f);
        double d = [n asDouble];
        if (t.bits == 32 || t.hasRange) d = (double)(float)d;
        f.defaultValue = [SPFloat d:d];
        return;
    }
    case SPKBool:
        if (![lit isKindOfClass:[NSString class]]) SPSchemaFail(@"%@: a bool default is true or false", f.name);
        f.defaultValue = [SPInt u:[lit isEqualToString:@"true"] ? 1 : 0];
        return;
    case SPKEnum: {
        if (![lit isKindOfClass:[NSString class]]) SPSchemaFail(@"%@: an enum default is a variant name", f.name);
        NSUInteger k = 0;
        for (NSUInteger i = 0; i < t.decl.variants.count; i++)
            if ([t.decl.variants[i].name isEqualToString:lit]) k = i + 1;
        if (!k && ![lit isEqualToString:@"None"]) SPSchemaFail(@"%@: unknown variant %@", f.name, lit);
        f.defaultValue = [SPInt u:k];
        return;
    }
    case SPKString: case SPKBytes:
        if (![lit isKindOfClass:[NSData class]]) SPSchemaFail(@"%@: a string default is a quoted string", f.name);
        f.defaultValue = lit;
        return;
    case SPKFlags: {
        if (![lit isKindOfClass:[NSArray class]]) SPSchemaFail(@"%@: a flags default is a brace list", f.name);
        sp_u128 m = 0;
        for (NSString *nm in lit) {
            BOOL found = NO;
            for (NSUInteger i = 0; i < t.decl.variants.count; i++)
                if ([t.decl.variants[i].name isEqualToString:nm]) { m |= ((sp_u128)1) << i; found = YES; }
            if (!found) SPSchemaFail(@"%@: unknown flags variant %@", f.name, nm);
        }
        f.defaultValue = [SPInt u:m];
        return;
    }
    default:
        SPSchemaFail(@"%@: this field takes no specified default", f.name);
    }
}
