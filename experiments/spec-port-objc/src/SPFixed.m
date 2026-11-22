// SPFixed.m — the fixed form, form byte 3.

#import "SPFixed.h"
#include <math.h>

NSString *const SPAbsent = @"SPAbsent";

// ---------------------------------------------------------------------------
// sizes (FIXED-FORM-ALGORITHM §3's C table)

static int ordinalWidth(unsigned long long top) {
    return top <= 0xFF ? 1 : top <= 0xFFFF ? 2 : top <= 0xFFFFFFFFull ? 4 : 8;
}

static NSUInteger C(SPType *t);

static NSUInteger fieldC(SPField *f) { return (f.optional ? 1 : 0) + (f.type ? C(f.type) : 0); }

static NSUInteger structC(SPDecl *d) {
    NSUInteger s = 0;
    for (SPField *f in d.fields) s += fieldC(f);
    return s;
}

// SPEC-GAP 10: §3's C table gives a union `tag width + max(C(arm))` and never
// says how the tag width is chosen; the enum's rule (note a: the top ordinal,
// here the arm count, at 1/2/4/8 bytes) is used, and every lock hash matches.
static NSUInteger unionTagW(SPDecl *d) { return (NSUInteger)ordinalWidth(d.fields.count); }

static NSUInteger unionWidest(SPDecl *d) {
    NSUInteger w = 0;
    for (SPField *a in d.fields) if (a.type && C(a.type) > w) w = C(a.type);
    return w;
}

static NSUInteger C(SPType *t) {
    switch (t.kind) {
    case SPKBool: return 1;
    case SPKInt: return (NSUInteger)t.bits / 8;
    case SPKBits: return t.bits <= 32 ? 4 : 8;
    case SPKFloat: return (t.bits == 32 || t.hasRange) ? 4 : 8;
    case SPKFixed: return (NSUInteger)t.bits / 8;
    case SPKInt128: return 16;
    case SPKEnum: return (NSUInteger)ordinalWidth(t.decl.variants.count);
    case SPKFlags: return 8;
    case SPKString:
        if (t.cap < 0) SPSchemaFail(@"an unbounded string in a fixed table");
        return 4 + (NSUInteger)t.cap;
    case SPKBytes: return 4 + (NSUInteger)t.cap;
    case SPKStruct: return structC(t.decl);
    case SPKUnion: return unionTagW(t.decl) + unionWidest(t.decl);
    case SPKArray:
        if (t.elemOptional) SPSchemaFail(@"an optional element in a fixed table");
        if (t.shape == SPArrayFixed) return (NSUInteger)t.maxCount * C(t.elem);
        if (t.shape == SPArrayCounted) return 4 + (NSUInteger)t.maxCount * C(t.elem);
        SPSchemaFail(@"an unbounded array in a fixed table");
    case SPKKeyed: return t.keyEnum.variants.count * C(t.elem);
    default:
        SPSchemaFail(@"a kind the fixed form refuses (a pointer, a map, a blob or a timestamp)");
    }
}

NSUInteger SPFixedRecordSize(SPDecl *root) { return structC(root); }

// ---------------------------------------------------------------------------
// the layout (§1): pre-order entries of 17 bytes

static void putLE(NSMutableData *d, sp_u128 v, int w) {
    for (int i = 0; i < w; i++) { uint8_t b = (uint8_t)(v >> (8 * i)); [d appendBytes:&b length:1]; }
}

static void entry(NSMutableData *d, uint64_t id, int kind, NSUInteger size, NSUInteger children) {
    putLE(d, id, 8);
    putLE(d, (sp_u128)kind, 1);
    putLE(d, size, 4);
    putLE(d, children, 4);
}

static NSUInteger layoutType(NSMutableData *d, uint64_t id, SPType *t);

static NSUInteger layoutStruct(NSMutableData *d, uint64_t id, SPDecl *decl) {
    entry(d, id, 13, structC(decl), decl.fields.count);
    NSUInteger n = 1;
    for (SPField *f in decl.fields) {
        if (f.optional) {
            entry(d, f.wireId, 35, fieldC(f), 1);
            n++;
        }
        n += layoutType(d, f.wireId, f.type);
    }
    return n;
}

static NSUInteger layoutEnum(NSMutableData *d, uint64_t id, SPDecl *e) {
    entry(d, id, 30, (NSUInteger)ordinalWidth(e.variants.count), e.variants.count);
    for (SPVariant *v in e.variants) entry(d, v.wireId, 32, 0, 0);
    return 1 + e.variants.count;
}

static NSUInteger layoutType(NSMutableData *d, uint64_t id, SPType *t) {
    switch (t.kind) {
    case SPKStruct: return layoutStruct(d, id, t.decl);
    case SPKEnum: return layoutEnum(d, id, t.decl);
    case SPKUnion: {
        entry(d, id, 15, C(t), t.decl.fields.count);
        NSUInteger n = 1;
        for (SPField *a in t.decl.fields) {
            if (!a.type) { entry(d, a.wireId, 32, 0, 0); n++; }
            else n += layoutType(d, a.wireId, a.type);
        }
        return n;
    }
    case SPKBytes:
        entry(d, id, 14, C(t), 1);
        entry(d, 0, 6, 1, 0);
        return 2;
    case SPKArray:
        entry(d, id, 14, C(t), 1);
        return 1 + layoutType(d, 0, t.elem);
    case SPKKeyed:
        entry(d, id, 16, C(t), 2);
        return 1 + layoutEnum(d, t.keyEnum.wireId, t.keyEnum) + layoutType(d, 0, t.elem);
    default:
        entry(d, id, t.wireKind, C(t), 0);
        return 1;
    }
}

NSData *SPFixedLayout(SPDecl *root) {
    NSMutableData *body = [NSMutableData data];
    NSUInteger n = layoutStruct(body, root.wireId, root);
    NSMutableData *d = [NSMutableData data];
    putLE(d, n, 4);
    [d appendData:body];
    return d;
}

// ---------------------------------------------------------------------------
// the v2 definitions digest (FIXED-FORM-ALGORITHM §5.2, WireLaw 3)

static void seg(NSMutableData *p, char role, int kind, uint64_t id) {
    putLE(p, (sp_u128)(uint8_t)role, 1);
    putLE(p, (sp_u128)kind, 4);
    putLE(p, id, 8);
}

static void row(NSMutableData *out, char tag, NSData *path, NSData *payload) {
    putLE(out, (sp_u128)(uint8_t)tag, 1);
    putLE(out, path.length, 4);
    [out appendData:path];
    putLE(out, payload.length, 4);
    [out appendData:payload];
}

static void canonInt(NSMutableData *p, sp_i128 v, BOOL isSigned) {
    BOOL neg = isSigned && v < 0;
    sp_u128 m = neg ? (sp_u128)0 - (sp_u128)v : (sp_u128)v;
    uint8_t be[16];
    int n = 0;
    for (sp_u128 x = m; x; x >>= 8) n++;
    for (int i = 0; i < n; i++) be[i] = (uint8_t)(m >> (8 * (n - 1 - i)));
    putLE(p, (sp_u128)(neg ? 1 : 0), 1);
    putLE(p, (sp_u128)n, 4);
    [p appendBytes:be length:(NSUInteger)n];
}

static void digestFields(NSMutableData *out, SPDecl *decl, NSData *path);

static void digestFacts(NSMutableData *out, SPType *t, NSData *path) {
    if (t.kind == SPKArray && t.shape == SPArrayCounted) {
        NSMutableData *p = [NSMutableData data];
        putLE(p, (sp_u128)t.minCount, 8);
        row(out, 'C', path, p);
    }
    if ((t.kind == SPKInt || t.kind == SPKInt128) && t.hasRange) {
        NSMutableData *p = [NSMutableData data];
        putLE(p, (sp_u128)t.wireKind, 4);
        putLE(p, (sp_u128)(t.isSigned ? 1 : 0), 1);
        putLE(p, 0, 4);
        canonInt(p, t.imin, t.isSigned);
        canonInt(p, t.imax, t.isSigned);
        row(out, 'R', path, p);
    }
    if (t.kind == SPKFixed) {
        NSMutableData *p = [NSMutableData data];
        putLE(p, (sp_u128)t.wireKind, 4);
        putLE(p, (sp_u128)(t.isSigned ? 1 : 0), 1);
        putLE(p, (sp_u128)t.fixedF, 4);
        canonInt(p, t.imin >> t.fixedF, t.isSigned);
        canonInt(p, t.imax >> t.fixedF, t.isSigned);
        row(out, 'R', path, p);
    }
    if (t.kind == SPKFloat && t.hasRange) {
        NSMutableData *p = [NSMutableData data];
        putLE(p, (sp_u128)t.wireKind, 4);
        double mn = t.fmin, mx = t.fmax, res = t.fres;
        uint64_t b;
        memcpy(&b, &mn, 8); putLE(p, b, 8);
        memcpy(&b, &mx, 8); putLE(p, b, 8);
        row(out, 'D', path, p);
        NSMutableData *q = [NSMutableData data];
        memcpy(&b, &res, 8); putLE(q, b, 8);
        row(out, 'Q', path, q);
    }
    if (t.kind == SPKBits) {
        NSMutableData *p = [NSMutableData data];
        putLE(p, (sp_u128)t.bits, 4);
        row(out, 'B', path, p);
    }
    if (t.kind == SPKFixed) {
        NSMutableData *p = [NSMutableData data];
        putLE(p, (sp_u128)t.fixedI, 4);
        putLE(p, (sp_u128)t.fixedF, 4);
        putLE(p, (sp_u128)(t.isSigned ? 1 : 0), 1);
        row(out, 'X', path, p);
    }
    if (t.kind == SPKFlags) {
        NSMutableData *p = [NSMutableData data];
        NSUInteger w = t.decl.flagsWidth ? (NSUInteger)t.decl.flagsWidth : t.decl.variants.count;
        putLE(p, w, 4);
        putLE(p, t.decl.variants.count, 4);
        for (SPVariant *v in t.decl.variants) putLE(p, v.wireId, 8);
        row(out, 'F', path, p);
    }
    switch (t.kind) {
    case SPKStruct: digestFields(out, t.decl, path); break;
    case SPKUnion:
        for (SPField *a in t.decl.fields) {
            NSMutableData *ap = [path mutableCopy];
            seg(ap, 'a', a.type ? a.type.wireKind : 32, a.wireId);
            if (a.type) digestFacts(out, a.type, ap);
        }
        break;
    case SPKArray: case SPKKeyed: {
        NSMutableData *ep = [path mutableCopy];
        seg(ep, 'e', t.elem.wireKind, 0);
        digestFacts(out, t.elem, ep);
        break;
    }
    default: break;
    }
}

static void digestFields(NSMutableData *out, SPDecl *decl, NSData *path) {
    for (SPField *f in decl.fields) {
        NSMutableData *fp = [path mutableCopy];
        seg(fp, 'f', f.type.wireKind, f.wireId);
        if (f.optional) seg(fp, '?', 35, 0);
        digestFacts(out, f.type, fp);
    }
}

NSData *SPFixedDigest(SPDecl *root) {
    static const uint8_t prefix[] = { 0x00, 's', 'c', 'h', 'e', 'm', 'a', '-', 'f', 'i', 'x', 'e', 'd', '-',
                                      'd', 'e', 'f', 'i', 'n', 'i', 't', 'i', 'o', 'n', 's', 0x02 };
    NSMutableData *out = [NSMutableData dataWithBytes:prefix length:sizeof prefix];
    NSMutableData *path = [NSMutableData data];
    seg(path, 't', 13, root.wireId);
    digestFields(out, root, path);
    return out;
}

uint64_t SPFixedHash(SPDecl *root) {
    uint64_t h = 0xcbf29ce484222325ULL;
    for (NSData *d in @[ SPFixedLayout(root), SPFixedDigest(root) ]) {
        const uint8_t *p = d.bytes;
        for (NSUInteger i = 0; i < d.length; i++) { h ^= p[i]; h *= 0x100000001b3ULL; }
    }
    return h;
}

// ---------------------------------------------------------------------------
// write (§3, §3.1): the zeroed template, then the live extent of every field

static void store(uint8_t *b, sp_u128 v, NSUInteger w) {
    for (NSUInteger i = 0; i < w; i++) b[i] = (uint8_t)(v >> (8 * i));
}

static void writeValue(uint8_t *b, id v, SPType *t, NSString *where);

static void writeStruct(uint8_t *b, SPTableVal *tv, NSString *where) {
    NSUInteger off = 0;
    for (NSUInteger i = 0; i < tv.decl.fields.count; i++) {
        SPField *f = tv.decl.fields[i];
        NSString *w = [where stringByAppendingFormat:@".%@", f.name];
        if (f.optional) {
            BOOL present = [tv.present[i] boolValue];
            b[off] = present ? 1 : 0;
            if (present) writeValue(b + off + 1, tv.values[i], f.type, w);
        } else writeValue(b + off, tv.values[i], f.type, w);
        off += fieldC(f);
    }
}

static void writeValue(uint8_t *b, id v, SPType *t, NSString *where) {
    SPCheckScalar(v, t, where);
    switch (t.kind) {
    case SPKBool: case SPKInt: case SPKBits: case SPKFixed: case SPKInt128: case SPKFlags: case SPKEnum:
        store(b, ((SPInt *)v)->raw, C(t));
        return;
    case SPKFloat: {
        double x = ((SPFloat *)v)->d;
        if (C(t) == 4) {
            float f = (float)x;
            if (t.hasRange && f == 0) f = 0.0f; // a compressed -0 rides as +0
            uint32_t u; memcpy(&u, &f, 4); store(b, u, 4);
        } else { uint64_t u; memcpy(&u, &x, 8); store(b, u, 8); }
        return;
    }
    case SPKString: case SPKBytes: {
        NSData *s = v;
        store(b, s.length, 4);
        memcpy(b + 4, s.bytes, s.length);
        return;
    }
    case SPKStruct: writeStruct(b, v, where); return;
    case SPKUnion: {
        SPUnionVal *u = v;
        if (u.arm < 0 || u.arm > (NSInteger)t.decl.fields.count) SPRefuse(@"invalid_value: %@ union tag outside its arms", where);
        store(b, (sp_u128)u.arm, unionTagW(t.decl));
        if (u.arm) {
            SPField *a = t.decl.fields[(NSUInteger)u.arm - 1];
            if (a.type) writeValue(b + unionTagW(t.decl), u.value, a.type, [where stringByAppendingFormat:@".%@", a.name]);
        }
        return;
    }
    case SPKArray: {
        NSArray *a = v;
        NSUInteger ec = C(t.elem), base = 0;
        if (t.shape == SPArrayFixed) {
            if ((long long)a.count != t.maxCount) SPRefuse(@"invalid_value: %@ fixed array count", where);
        } else {
            if ((long long)a.count > t.maxCount || (long long)a.count < t.minCount) SPRefuse(@"invalid_value: %@ count off its bound", where);
            store(b, a.count, 4);
            base = 4;
        }
        for (NSUInteger i = 0; i < a.count; i++)
            writeValue(b + base + i * ec, a[i], t.elem, [where stringByAppendingFormat:@"[%lu]", (unsigned long)i]);
        return;
    }
    case SPKKeyed: {
        NSArray *a = v;
        NSUInteger ec = C(t.elem);
        for (NSUInteger i = 0; i < a.count; i++)
            writeValue(b + i * ec, a[i], t.elem, [where stringByAppendingFormat:@"[%@]", t.keyEnum.variants[i].name]);
        return;
    }
    default:
        SPRefuse(@"invalid_value: %@ a kind the fixed form does not carry", where);
    }
}

NSData *SPSaveFixed(SPTableVal *root) {
    SPDecl *d = root.decl;
    NSData *layout = SPFixedLayout(d);
    uint64_t h = SPFixedHash(d);
    NSUInteger rc = structC(d);
    NSMutableData *rec = [NSMutableData dataWithLength:8 + rc];
    store(rec.mutableBytes, h, 8);
    writeStruct((uint8_t *)rec.mutableBytes + 8, root, d.name);
    NSMutableData *out = [NSMutableData data];
    putLE(out, 3, 1);
    putLE(out, 0, 7);
    putLE(out, h, 8);
    putLE(out, layout.length, 4);
    [out appendData:layout];
    [out appendData:rec];
    return out;
}

// ---------------------------------------------------------------------------
// read (§2, §4): one record under this build's own layout

#define DAMAGE(...) SPRefuse(@"wire_damaged: " __VA_ARGS__)

static sp_u128 load(const uint8_t *b, NSUInteger w) {
    sp_u128 v = 0;
    for (NSUInteger i = 0; i < w; i++) v |= (sp_u128)b[i] << (8 * i);
    return v;
}

static void deadZero(const uint8_t *b, NSUInteger n, NSString *where) {
    for (NSUInteger i = 0; i < n; i++) if (b[i]) DAMAGE(@"a nonzero dead byte at %@", where);
}

static id readValue(const uint8_t *b, SPType *t, id def, NSString *where);

static SPTableVal *readStruct(const uint8_t *b, SPDecl *decl, NSString *where) {
    SPTableVal *tv = SPNewTable(decl);
    NSUInteger off = 0;
    for (NSUInteger i = 0; i < decl.fields.count; i++) {
        SPField *f = decl.fields[i];
        NSString *w = [where stringByAppendingFormat:@".%@", f.name];
        if (f.optional) {
            uint8_t pf = b[off];
            if (pf > 1) DAMAGE(@"a present byte other than 0 or 1 at %@", w);
            if (pf) {
                tv.values[i] = readValue(b + off + 1, f.type, f.defaultValue, w);
                tv.present[i] = @YES;
            } else deadZero(b + off + 1, C(f.type), w);
        } else tv.values[i] = readValue(b + off, f.type, f.defaultValue, w);
        off += fieldC(f);
    }
    return tv;
}

static BOOL rangeHolds(sp_u128 raw, SPType *t) {
    if (SPTypeSigned(t)) return (sp_i128)raw >= t.imin && (sp_i128)raw <= t.imax;
    return raw >= (sp_u128)t.imin && raw <= (sp_u128)t.imax;
}

static sp_u128 signExtend(sp_u128 v, NSUInteger w) {
    if (w < 16 && (v >> (8 * w - 1)) & 1) v |= ~(sp_u128)0 << (8 * w);
    return v;
}

static id readValue(const uint8_t *b, SPType *t, id def, NSString *where) {
    NSUInteger w = C(t);
    switch (t.kind) {
    case SPKBool:
        if (b[0] > 1) DAMAGE(@"a bool byte other than 0 or 1 at %@", where);
        return [SPInt u:b[0]];
    case SPKInt: case SPKFixed: case SPKInt128: {
        sp_u128 v = load(b, w);
        if (t.isSigned) v = signExtend(v, w);
        if (t.hasRange && !rangeHolds(v, t)) SPRefuse(@"out_of_range: %@", where);
        return [SPInt u:v];
    }
    case SPKBits: {
        sp_u128 v = load(b, w);
        if (v > (sp_u128)t.imax) SPRefuse(@"out_of_range: %@ past bits(%d)", where, t.bits);
        return [SPInt u:v];
    }
    case SPKFlags: return [SPInt u:load(b, 8)];
    case SPKFloat: {
        double x;
        if (w == 4) { uint32_t u = (uint32_t)load(b, 4); float f; memcpy(&f, &u, 4); x = f; }
        else { uint64_t u = (uint64_t)load(b, 8); memcpy(&x, &u, 8); }
        if (!isfinite(x)) SPRefuse(@"non_finite_float: %@", where);
        if (t.hasRange) {
            if (x < t.fmin || x > t.fmax) SPRefuse(@"out_of_range: %@", where);
            if (x == 0) x = 0.0;
        }
        return [SPFloat d:x];
    }
    case SPKEnum: {
        sp_u128 k = load(b, w);
        if (k > t.decl.variants.count) SPRefuse(@"out_of_range: %@ an ordinal past the enum", where);
        // SPEC-GAP 7: a deprecated variant on the fixed form; the form-1 rule
        // (it reads as the field's declared default) is applied here too
        if (k && t.decl.variants[(NSUInteger)k - 1].deprecated) return def ? SPCopyValue(def) : [SPInt u:0];
        return [SPInt u:k];
    }
    case SPKString: case SPKBytes: {
        sp_i128 n = (sp_i128)signExtend(load(b, 4), 4);
        if (n < 0 || n > t.cap) SPRefuse(@"length_over_bound: %@", where);
        NSData *s = [NSData dataWithBytes:b + 4 length:(NSUInteger)n];
        if (t.kind == SPKString && !SPValidUTF8(s, NO)) DAMAGE(@"ill-formed text at %@", where);
        deadZero(b + 4 + (NSUInteger)n, (NSUInteger)(t.cap - n), where);
        return s;
    }
    case SPKStruct: return readStruct(b, t.decl, where);
    case SPKUnion: {
        NSUInteger tw = unionTagW(t.decl), widest = unionWidest(t.decl);
        sp_u128 tag = load(b, tw);
        if (tag > t.decl.fields.count) SPRefuse(@"out_of_range: %@ a union tag past its arms", where);
        SPUnionVal *u = [SPUnionVal new];
        u.value = [NSNull null];
        u.arm = (NSInteger)tag;
        NSUInteger used = 0;
        if (tag) {
            SPField *a = t.decl.fields[(NSUInteger)tag - 1];
            if (a.type) {
                u.value = readValue(b + tw, a.type, a.defaultValue, [where stringByAppendingFormat:@".%@", a.name]);
                used = C(a.type);
            }
        }
        deadZero(b + tw + used, widest - used, where);
        return u;
    }
    case SPKArray: {
        NSUInteger ec = C(t.elem), base = 0, n = (NSUInteger)t.maxCount;
        if (t.shape == SPArrayCounted) {
            sp_i128 c = (sp_i128)signExtend(load(b, 4), 4);
            if (c < 0 || c > t.maxCount) SPRefuse(@"count_over_bound: %@", where);
            if (c < t.minCount) SPRefuse(@"count_over_bound: %@ below its minimum", where);
            n = (NSUInteger)c;
            base = 4;
            deadZero(b + 4 + n * ec, ((NSUInteger)t.maxCount - n) * ec, where);
        }
        NSMutableArray *a = [NSMutableArray arrayWithCapacity:n];
        for (NSUInteger i = 0; i < n; i++)
            [a addObject:readValue(b + base + i * ec, t.elem, nil, [where stringByAppendingFormat:@"[%lu]", (unsigned long)i])];
        return a;
    }
    case SPKKeyed: {
        NSUInteger ec = C(t.elem);
        NSMutableArray *a = [NSMutableArray array];
        for (NSUInteger i = 0; i < t.keyEnum.variants.count; i++)
            [a addObject:readValue(b + i * ec, t.elem, nil, [where stringByAppendingFormat:@"[%@]", t.keyEnum.variants[i].name])];
        return a;
    }
    default:
        SPRefuse(@"wire_damaged: a kind the fixed form does not carry at %@", where);
    }
}

SPTableVal *SPLoadFixed(NSData *file, SPDecl *root, NSSet<NSNumber *> *lineage) {
    const uint8_t *p = file.bytes;
    NSUInteger n = file.length;
    if (n == 0) SPRefuse(@"truncated");
    if (p[0] == 1) SPRefuse(@"previous_form");
    if (p[0] == 2) SPRefuse(@"message_form_as_file");
    if (p[0] != 3) SPRefuse(@"newer_form");
    if (n < 20) SPRefuse(@"truncated");
    for (int i = 1; i < 8; i++) if (p[i]) SPRefuse(@"reserved_not_zero");
    uint64_t h = (uint64_t)load(p + 8, 8);
    NSUInteger L = (NSUInteger)load(p + 16, 4);
    if (20 + L > n) SPRefuse(@"layout_malformed");
    uint64_t mine = SPFixedHash(root);
    if (h != mine) {
        if ([lineage containsObject:@(h)])
            @throw [NSException exceptionWithName:SPAbsent reason:@"a lineage layout: the §5.2 plan compiler is not carried" userInfo:nil];
        SPRefuse(@"layout_unknown");
    }
    NSData *layout = SPFixedLayout(root);
    if (L != layout.length || memcmp(p + 20, layout.bytes, L) != 0) SPRefuse(@"layout_malformed: a known hash over other bytes");
    NSUInteger rb = 8 + structC(root), rest = n - 20 - L;
    if (rb <= 8 || rest % rb) SPRefuse(@"truncated: the records do not fill the file");
    NSUInteger count = rest / rb;
    if (count == 0) SPRefuse(@"truncated: a fixed-form file holds at least one record");
    // SPEC-GAP 6: a single-root Load is a caller of capacity one, so a batch
    // of two refuses batch_too_large
    if (count > 1) SPRefuse(@"batch_too_large");
    const uint8_t *rec = p + 20 + L;
    if ((uint64_t)load(rec, 8) != h) SPRefuse(@"no_layout");
    return readStruct(rec + 8, root, root.name);
}
