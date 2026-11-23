// SPJson.m — the text form, read and write (SPEC-TABLES §16).

#import "SPJson.h"
#include <math.h>
#include <stdlib.h>
#include <string.h>

#define JMAL(...) SPRefuse(@"malformed: " __VA_ARGS__)

static const int kJsonMaxDepth = 128;
static const NSUInteger kNameCap = 255;

// ---------------------------------------------------------------------------
// 128-bit decimal

NSString *SPU128Decimal(sp_u128 v, BOOL negative) {
    char buf[64];
    int i = 63;
    buf[i] = 0;
    if (v == 0) buf[--i] = '0';
    while (v) { buf[--i] = (char)('0' + (int)(v % 10)); v /= 10; }
    if (negative) buf[--i] = '-';
    return [NSString stringWithUTF8String:buf + i];
}

static NSString *intText(sp_u128 raw, BOOL isSigned) {
    if (isSigned && (sp_i128)raw < 0) return SPU128Decimal((sp_u128)0 - raw, YES);
    return SPU128Decimal(raw, NO);
}

// ---------------------------------------------------------------------------
// The document tree. A parse is the grammar's whole check (RFC 8259 plus
// §16.2's comments, trailing commas, a leading BOM, repeated-key and depth
// refusals, and §16.3's UTF-8 and zero rules); the walk below places it.

typedef NS_ENUM(int, JKind) { JObj, JArr, JStr, JNum, JTrue, JFalse, JNull };

@interface JNode : NSObject
@property (nonatomic) JKind kind;
@property (nonatomic, strong) NSMutableArray<NSData *> *keys;
@property (nonatomic, strong) NSMutableArray<JNode *> *items; // object values, array elements
@property (nonatomic, strong) NSData *str;   // decoded bytes
@property (nonatomic, strong) NSString *num; // the token
@end
@implementation JNode
@end

@interface JParser : NSObject {
  @public
    const uint8_t *p;
    NSUInteger n, i;
}
@end

@implementation JParser

- (void)checkCommentBytes:(NSUInteger)a end:(NSUInteger)b {
    NSData *d = [NSData dataWithBytesNoCopy:(void *)(p + a) length:b - a freeWhenDone:NO];
    if (!SPValidUTF8(d, NO)) JMAL(@"a comment that is not well-formed UTF-8 or holds a NUL");
}

- (void)ws {
    for (;;) {
        if (i >= n) return;
        uint8_t c = p[i];
        if (c == ' ' || c == '\t' || c == '\n' || c == '\r') { i++; continue; }
        if (c == '/' && i + 1 < n && p[i + 1] == '/') {
            NSUInteger a = i + 2;
            i = a;
            while (i < n && p[i] != '\n') i++;
            [self checkCommentBytes:a end:i];
            continue;
        }
        if (c == '/' && i + 1 < n && p[i + 1] == '*') {
            NSUInteger a = i + 2, k = a;
            while (k + 1 < n && !(p[k] == '*' && p[k + 1] == '/')) k++;
            if (k + 1 >= n) JMAL(@"an unclosed block comment");
            [self checkCommentBytes:a end:k];
            i = k + 2;
            continue;
        }
        return;
    }
}

static int hexv(uint8_t c) {
    if (c >= '0' && c <= '9') return c - '0';
    if (c >= 'a' && c <= 'f') return c - 'a' + 10;
    if (c >= 'A' && c <= 'F') return c - 'A' + 10;
    return -1;
}

- (unsigned)hex4 {
    if (i + 4 > n) JMAL(@"a \\u escape cut short");
    unsigned v = 0;
    for (int k = 0; k < 4; k++) {
        int h = hexv(p[i + k]);
        if (h < 0) JMAL(@"a \\u escape that is not four hex digits");
        v = v << 4 | (unsigned)h;
    }
    i += 4;
    return v;
}

static void putUTF8(NSMutableData *d, uint32_t cp) {
    uint8_t b[4];
    int len;
    if (cp < 0x80) { b[0] = (uint8_t)cp; len = 1; }
    else if (cp < 0x800) { b[0] = 0xC0 | (cp >> 6); b[1] = 0x80 | (cp & 0x3F); len = 2; }
    else if (cp < 0x10000) { b[0] = 0xE0 | (cp >> 12); b[1] = 0x80 | ((cp >> 6) & 0x3F); b[2] = 0x80 | (cp & 0x3F); len = 3; }
    else { b[0] = 0xF0 | (cp >> 18); b[1] = 0x80 | ((cp >> 12) & 0x3F); b[2] = 0x80 | ((cp >> 6) & 0x3F); b[3] = 0x80 | (cp & 0x3F); len = 4; }
    [d appendBytes:b length:len];
}

- (NSData *)string {
    // at the opening quote
    i++;
    NSMutableData *out = [NSMutableData data];
    NSUInteger runStart = i;
    for (;;) {
        if (i >= n) JMAL(@"an unclosed string");
        uint8_t c = p[i];
        if (c == '"') {
            [out appendBytes:p + runStart length:i - runStart];
            i++;
            break;
        }
        if (c < 0x20) JMAL(@"a raw control byte inside a string");
        if (c != '\\') { i++; continue; }
        [out appendBytes:p + runStart length:i - runStart];
        i++;
        if (i >= n) JMAL(@"an escape cut short");
        uint8_t e = p[i++];
        switch (e) {
        case '"': putUTF8(out, '"'); break;
        case '\\': putUTF8(out, '\\'); break;
        case '/': putUTF8(out, '/'); break;
        case 'b': putUTF8(out, 8); break;
        case 'f': putUTF8(out, 12); break;
        case 'n': putUTF8(out, 10); break;
        case 'r': putUTF8(out, 13); break;
        case 't': putUTF8(out, 9); break;
        case 'u': {
            unsigned u = [self hex4];
            if (u >= 0xDC00 && u <= 0xDFFF) SPRefuse(@"malformed: string_lone_surrogate");
            if (u >= 0xD800 && u <= 0xDBFF) {
                if (i + 6 > n || p[i] != '\\' || p[i + 1] != 'u') SPRefuse(@"malformed: string_lone_surrogate");
                i += 2;
                unsigned lo = [self hex4];
                if (lo < 0xDC00 || lo > 0xDFFF) SPRefuse(@"malformed: string_lone_surrogate");
                u = 0x10000 + ((u - 0xD800) << 10) + (lo - 0xDC00);
            }
            // §16.3: no read stores U+0000, and a skipped string is held
            // to the same rule as a placed one
            if (u == 0) SPRefuse(@"text_holds_zero");
            putUTF8(out, u);
            break;
        }
        default:
            JMAL(@"a backslash that begins no escape");
        }
        runStart = i;
    }
    if (!SPValidUTF8(out, NO)) SPRefuse(@"malformed: string_not_utf8");
    return out;
}

- (NSString *)number {
    NSUInteger a = i;
    if (i < n && p[i] == '-') i++;
    if (i >= n) JMAL(@"a number cut short");
    if (p[i] == '0') i++;
    else if (p[i] >= '1' && p[i] <= '9') { while (i < n && p[i] >= '0' && p[i] <= '9') i++; }
    else JMAL(@"a token that is not a JSON number");
    if (i < n && p[i] == '.') {
        i++;
        if (i >= n || p[i] < '0' || p[i] > '9') JMAL(@"a fraction with no digit");
        while (i < n && p[i] >= '0' && p[i] <= '9') i++;
    }
    if (i < n && (p[i] == 'e' || p[i] == 'E')) {
        i++;
        if (i < n && (p[i] == '+' || p[i] == '-')) i++;
        if (i >= n || p[i] < '0' || p[i] > '9') JMAL(@"an exponent with no digit");
        while (i < n && p[i] >= '0' && p[i] <= '9') i++;
    }
    return [[NSString alloc] initWithBytes:p + a length:i - a encoding:NSASCIIStringEncoding];
}

- (BOOL)lit:(const char *)s {
    size_t l = strlen(s);
    if (i + l <= n && memcmp(p + i, s, l) == 0) { i += l; return YES; }
    return NO;
}

- (JNode *)value:(int)depth {
    [self ws];
    if (i >= n) JMAL(@"a value expected");
    JNode *v = [JNode new];
    uint8_t c = p[i];
    if (c == '{' || c == '[') {
        // the read's cap counts every object and array, placed or skipped,
        // the root at 0: a container deeper than 128 refuses (§16.2)
        if (depth > kJsonMaxDepth) JMAL(@"nesting past %d", kJsonMaxDepth);
        i++;
        v.kind = c == '{' ? JObj : JArr;
        v.items = [NSMutableArray array];
        if (c == '{') v.keys = [NSMutableArray array];
        NSMutableSet *seen = c == '{' ? [NSMutableSet set] : nil;
        uint8_t close = c == '{' ? '}' : ']';
        for (;;) {
            [self ws];
            if (i >= n) JMAL(@"an unclosed container");
            if (p[i] == close) { i++; break; }
            if (c == '{') {
                if (p[i] != '"') JMAL(@"an object key that is not a string");
                NSData *k = [self string];
                if ([seen containsObject:k]) JMAL(@"a repeated key");
                [seen addObject:k];
                [self ws];
                if (i >= n || p[i] != ':') JMAL(@"a key with no colon");
                i++;
                [v.keys addObject:k];
            }
            [v.items addObject:[self value:depth + 1]];
            [self ws];
            if (i >= n) JMAL(@"an unclosed container");
            if (p[i] == ',') { i++; continue; } // a trailing comma is accepted
            if (p[i] == close) { i++; break; }
            JMAL(@"a missing comma");
        }
        return v;
    }
    if (c == '"') { v.kind = JStr; v.str = [self string]; return v; }
    if (c == '-' || (c >= '0' && c <= '9')) { v.kind = JNum; v.num = [self number]; return v; }
    if ([self lit:"true"]) { v.kind = JTrue; return v; }
    if ([self lit:"false"]) { v.kind = JFalse; return v; }
    if ([self lit:"null"]) { v.kind = JNull; return v; }
    JMAL(@"a token that is not JSON");
}

+ (JNode *)parse:(NSData *)text {
    JParser *ps = [JParser new];
    ps->p = text.bytes;
    ps->n = text.length;
    ps->i = 0;
    if (ps->n >= 3 && ps->p[0] == 0xEF && ps->p[1] == 0xBB && ps->p[2] == 0xBF) ps->i = 3;
    JNode *v = [ps value:0];
    [ps ws];
    if (ps->i != ps->n) JMAL(@"bytes after the document");
    return v;
}
@end

// ---------------------------------------------------------------------------
// Exact decimal arithmetic for a token (no double on the integer paths)

typedef struct { uint32_t *w; int n, cap; } Big;

static void bigInit(Big *b, int cap) { b->w = calloc((size_t)cap, 4); b->n = 0; b->cap = cap; }
static void bigFree(Big *b) { free(b->w); }
static void bigMulAdd(Big *b, uint32_t m, uint32_t a) {
    uint64_t carry = a;
    for (int k = 0; k < b->n; k++) {
        uint64_t x = (uint64_t)b->w[k] * m + carry;
        b->w[k] = (uint32_t)x;
        carry = x >> 32;
    }
    if (carry) {
        if (b->n == b->cap) { b->cap *= 2; b->w = realloc(b->w, (size_t)b->cap * 4); }
        b->w[b->n++] = (uint32_t)carry;
    }
}
static uint32_t bigDiv(Big *b, uint32_t d) {
    uint64_t r = 0;
    for (int k = b->n - 1; k >= 0; k--) {
        uint64_t x = r << 32 | b->w[k];
        b->w[k] = (uint32_t)(x / d);
        r = x % d;
    }
    while (b->n && !b->w[b->n - 1]) b->n--;
    return (uint32_t)r;
}
static BOOL bigToU128(Big *b, sp_u128 *out) {
    if (b->n > 4) return NO;
    sp_u128 v = 0;
    for (int k = b->n - 1; k >= 0; k--) v = v << 32 | b->w[k];
    *out = v;
    return YES;
}

// A JSON number token as sign, significant digits and a power of ten.
typedef struct { BOOL neg; NSString *digits; long long exp10; BOOL plainInt; } Dec;

static Dec decOf(NSString *tok) {
    Dec d = { NO, @"", 0, YES };
    const char *s = tok.UTF8String;
    if (*s == '-') { d.neg = YES; s++; }
    NSMutableString *dg = [NSMutableString string];
    long long frac = 0;
    while (*s >= '0' && *s <= '9') [dg appendFormat:@"%c", *s++];
    if (*s == '.') {
        d.plainInt = NO;
        s++;
        while (*s >= '0' && *s <= '9') { [dg appendFormat:@"%c", *s++]; frac++; }
    }
    long long e = 0;
    if (*s == 'e' || *s == 'E') {
        d.plainInt = NO;
        s++;
        BOOL en = NO;
        if (*s == '+') s++;
        else if (*s == '-') { en = YES; s++; }
        while (*s >= '0' && *s <= '9') {
            if (e < 100000000) e = e * 10 + (*s - '0');
            s++;
        }
        if (en) e = -e;
    }
    // strip leading and trailing zeros
    NSUInteger a = 0, b = dg.length;
    while (a < b && [dg characterAtIndex:a] == '0') a++;
    while (b > a && [dg characterAtIndex:b - 1] == '0') { b--; e++; }
    d.digits = [dg substringWithRange:NSMakeRange(a, b - a)];
    d.exp10 = e - frac;
    return d;
}

// value × 2^shift as an exact integer, or refuse (kind_mismatch when the
// value is finer than the scale, out_of_range when it is too large)
static sp_u128 decScaled(Dec d, int shift, NSString *where) {
    if (!d.digits.length) return 0;
    if (d.exp10 >= 0 && (long long)d.digits.length + d.exp10 > 45) SPRefuse(@"out_of_range: %@", where);
    if (d.exp10 < 0 && -d.exp10 > (long long)d.digits.length + shift + 2) SPRefuse(@"kind_mismatch: %@ finer than its scale", where);
    Big b;
    bigInit(&b, 8);
    const char *s = d.digits.UTF8String;
    for (; *s; s++) bigMulAdd(&b, 10, (uint32_t)(*s - '0'));
    for (long long k = 0; k < d.exp10; k++) bigMulAdd(&b, 10, 0);
    for (int k = 0; k < shift; k++) bigMulAdd(&b, 2, 0);
    for (long long k = 0; k < -d.exp10; k++)
        if (bigDiv(&b, 10)) { bigFree(&b); SPRefuse(@"kind_mismatch: %@ finer than its scale", where); }
    sp_u128 v;
    BOOL ok = bigToU128(&b, &v);
    bigFree(&b);
    if (!ok) SPRefuse(@"out_of_range: %@", where);
    return v;
}

// a magnitude and sign into a type's storage, range held exactly
static SPInt *signedInto(sp_u128 mag, BOOL neg, SPType *t, NSString *where) {
    BOOL isSigned = SPTypeSigned(t);
    if (neg && mag && !isSigned) SPRefuse(@"out_of_range: %@ negative in an unsigned field", where);
    sp_u128 raw;
    if (isSigned) {
        sp_u128 lim = neg ? ((sp_u128)1 << 127) : (((sp_u128)1 << 127) - 1);
        if (mag > lim) SPRefuse(@"out_of_range: %@", where);
        raw = neg ? (sp_u128)0 - mag : mag;
    } else raw = mag;
    SPInt *v = [SPInt u:raw];
    switch (t.kind) {
    case SPKBits:
        if (raw > (sp_u128)t.imax) SPRefuse(@"out_of_range: %@", where);
        break;
    case SPKInt128:
        if (t.hasRange) {
            if (isSigned ? ((sp_i128)raw < t.imin || (sp_i128)raw > t.imax) : (raw < (sp_u128)t.imin || raw > (sp_u128)t.imax))
                SPRefuse(@"out_of_range: %@", where);
        }
        break;
    default:
        if (isSigned ? ((sp_i128)raw < t.imin || (sp_i128)raw > t.imax) : (raw < (sp_u128)t.imin || raw > (sp_u128)t.imax))
            SPRefuse(@"out_of_range: %@", where);
    }
    return v;
}

// the exact integer grammar -?(0|[1-9][0-9]*), never -0
static BOOL intGrammar(NSData *s) {
    const uint8_t *c = s.bytes;
    NSUInteger n = s.length, k = 0;
    if (k < n && c[k] == '-') k++;
    if (k >= n) return NO;
    if (c[k] == '0') { if (k + 1 != n) return NO; return c[0] != '-'; }
    if (c[k] < '1' || c[k] > '9') return NO;
    for (; k < n; k++) if (c[k] < '0' || c[k] > '9') return NO;
    return YES;
}

static SPInt *intFromDigits(NSData *s, SPType *t, NSString *where) {
    if (!intGrammar(s)) SPRefuse(@"kind_mismatch: %@ not an integer spelled as one", where);
    const uint8_t *c = s.bytes;
    NSUInteger n = s.length, k = 0;
    BOOL neg = c[0] == '-';
    if (neg) k++;
    if (n - k > 40) SPRefuse(@"out_of_range: %@", where);
    sp_u128 mag = 0;
    for (; k < n; k++) {
        sp_u128 nx = mag * 10 + (sp_u128)(c[k] - '0');
        if (nx / 10 != mag) SPRefuse(@"out_of_range: %@", where);
        mag = nx;
    }
    return signedInto(mag, neg, t, where);
}

// one RFC 8259 number token, the whole content of a string
static BOOL numberGrammar(NSData *s) {
    const uint8_t *c = s.bytes;
    NSUInteger n = s.length, k = 0;
    if (k < n && c[k] == '-') k++;
    if (k >= n) return NO;
    if (c[k] == '0') k++;
    else if (c[k] >= '1' && c[k] <= '9') { while (k < n && c[k] >= '0' && c[k] <= '9') k++; }
    else return NO;
    if (k < n && c[k] == '.') {
        k++;
        if (k >= n || c[k] < '0' || c[k] > '9') return NO;
        while (k < n && c[k] >= '0' && c[k] <= '9') k++;
    }
    if (k < n && (c[k] == 'e' || c[k] == 'E')) {
        k++;
        if (k < n && (c[k] == '+' || c[k] == '-')) k++;
        if (k >= n || c[k] < '0' || c[k] > '9') return NO;
        while (k < n && c[k] >= '0' && c[k] <= '9') k++;
    }
    return k == n;
}

static int storageBits(SPType *t) {
    switch (t.wireKind) {
    case 1: case 2: case 6: case 20: case 25: return 8;
    case 3: case 7: case 21: case 26: return 16;
    case 4: case 8: case 22: case 27: return 32;
    case 5: case 9: case 23: case 28: return 64;
    case 18: case 19: case 24: case 29: return 128;
    }
    return 64;
}

// ---------------------------------------------------------------------------
// base64 (RFC 4648 §4), canonical both ways (§16.8)

static const char kB64[] = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";

static NSData *b64Encode(NSData *d) {
    const uint8_t *s = d.bytes;
    NSUInteger n = d.length;
    NSMutableData *o = [NSMutableData dataWithCapacity:(n + 2) / 3 * 4];
    for (NSUInteger k = 0; k < n; k += 3) {
        uint32_t v = (uint32_t)s[k] << 16;
        if (k + 1 < n) v |= (uint32_t)s[k + 1] << 8;
        if (k + 2 < n) v |= s[k + 2];
        char q[4] = { kB64[v >> 18 & 63], kB64[v >> 12 & 63], k + 1 < n ? kB64[v >> 6 & 63] : '=', k + 2 < n ? kB64[v & 63] : '=' };
        [o appendBytes:q length:4];
    }
    return o;
}

static NSData *b64DecodeCanonical(NSData *text, NSString *where) {
    const uint8_t *s = text.bytes;
    NSUInteger n = text.length;
    if (n % 4) SPRefuse(@"kind_mismatch: %@ not base64", where);
    NSMutableData *o = [NSMutableData data];
    for (NSUInteger k = 0; k < n; k += 4) {
        int v[4];
        for (int j = 0; j < 4; j++) {
            uint8_t c = s[k + j];
            const char *f = c ? strchr(kB64, c) : NULL;
            if (f) v[j] = (int)(f - kB64);
            else if (c == '=' && k + 4 == n && j >= 2) v[j] = -1;
            else SPRefuse(@"kind_mismatch: %@ not base64", where);
        }
        if (v[2] < 0 && v[3] >= 0) SPRefuse(@"kind_mismatch: %@ misplaced padding", where);
        uint8_t b[3] = { (uint8_t)(v[0] << 2 | v[1] >> 4), (uint8_t)((v[1] & 15) << 4 | (v[2] < 0 ? 0 : v[2] >> 2)), (uint8_t)((v[2] < 0 ? 0 : (v[2] & 3) << 6) | (v[3] < 0 ? 0 : v[3])) };
        [o appendBytes:b length:v[2] < 0 ? 1 : v[3] < 0 ? 2 : 3];
    }
    if (![b64Encode(o) isEqualToData:text]) SPRefuse(@"base64_not_canonical: %@", where);
    return o;
}

// ---------------------------------------------------------------------------
// timestamps (W13, §16.2's row): proleptic Gregorian, UTC

static long long daysFromCivil(long long y, unsigned m, unsigned d) {
    y -= m <= 2;
    long long era = (y >= 0 ? y : y - 399) / 400;
    unsigned yoe = (unsigned)(y - era * 400);
    unsigned doy = (153 * (m > 2 ? m - 3 : m + 9) + 2) / 5 + d - 1;
    unsigned doe = yoe * 365 + yoe / 4 - yoe / 100 + doy;
    return era * 146097 + (long long)doe - 719468;
}

static void civilFromDays(long long z, long long *y, unsigned *m, unsigned *d) {
    z += 719468;
    long long era = (z >= 0 ? z : z - 146096) / 146097;
    unsigned doe = (unsigned)(z - era * 146097);
    unsigned yoe = (doe - doe / 1460 + doe / 36524 - doe / 146096) / 365;
    long long yy = (long long)yoe + era * 400;
    unsigned doy = doe - (365 * yoe + yoe / 4 - yoe / 100);
    unsigned mp = (5 * doy + 2) / 153;
    *d = doy - (153 * mp + 2) / 5 + 1;
    *m = mp < 10 ? mp + 3 : mp - 9;
    *y = yy + (*m <= 2);
}

static NSString *timestampText(sp_i128 ns) {
    sp_i128 sec = ns / 1000000000, rem = ns % 1000000000;
    if (rem < 0) { rem += 1000000000; sec -= 1; }
    sp_i128 days = sec / 86400, sod = sec % 86400;
    if (sod < 0) { sod += 86400; days -= 1; }
    long long y; unsigned m, d;
    civilFromDays((long long)days, &y, &m, &d);
    NSMutableString *s = [NSMutableString stringWithFormat:@"%04lld-%02u-%02uT%02d:%02d:%02d", y, m, d, (int)(sod / 3600), (int)(sod / 60 % 60), (int)(sod % 60)];
    if (rem) {
        NSMutableString *f = [NSMutableString stringWithFormat:@"%09lld", (long long)rem];
        while ([f hasSuffix:@"0"]) [f deleteCharactersInRange:NSMakeRange(f.length - 1, 1)];
        [s appendFormat:@".%@", f];
    }
    [s appendString:@"Z"];
    return s;
}

static SPInt *timestampRead(NSData *str, NSString *where) {
    const uint8_t *c = str.bytes;
    NSUInteger n = str.length;
#define TSBAD() SPRefuse(@"timestamp_not_rfc3339: %@", where)
#define DIG(k) ((k) < n && c[k] >= '0' && c[k] <= '9')
    if (n < 20) TSBAD();
    for (int k = 0; k < 4; k++) if (!DIG(k)) TSBAD();
    if (c[4] != '-' || !DIG(5) || !DIG(6) || c[7] != '-' || !DIG(8) || !DIG(9)) TSBAD();
    if (c[10] != 'T' && c[10] != 't') TSBAD();
    if (!DIG(11) || !DIG(12) || c[13] != ':' || !DIG(14) || !DIG(15) || c[16] != ':' || !DIG(17) || !DIG(18)) TSBAD();
    long long y = (c[0] - '0') * 1000 + (c[1] - '0') * 100 + (c[2] - '0') * 10 + (c[3] - '0');
    unsigned mo = (c[5] - '0') * 10 + (c[6] - '0'), d = (c[8] - '0') * 10 + (c[9] - '0');
    int hh = (c[11] - '0') * 10 + (c[12] - '0'), mi = (c[14] - '0') * 10 + (c[15] - '0'), ss = (c[17] - '0') * 10 + (c[18] - '0');
    if (mo < 1 || mo > 12 || hh > 23 || mi > 59 || ss > 59) TSBAD();
    static const unsigned mdays[] = { 31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31 };
    BOOL leap = (y % 4 == 0 && y % 100 != 0) || y % 400 == 0;
    unsigned dim = mdays[mo - 1] + (mo == 2 && leap);
    if (d < 1 || d > dim) TSBAD();
    NSUInteger k = 19;
    long long frac = 0;
    if (k < n && c[k] == '.') {
        k++;
        if (!DIG(k)) TSBAD();
        int nd = 0;
        while (DIG(k)) {
            if (nd < 9) { frac = frac * 10 + (c[k] - '0'); nd++; }
            k++;
        }
        while (nd < 9) { frac *= 10; nd++; }
    }
    long long off = 0;
    if (k < n && (c[k] == 'Z' || c[k] == 'z')) k++;
    else if (k < n && (c[k] == '+' || c[k] == '-')) {
        BOOL neg = c[k] == '-';
        if (!(DIG(k + 1) && DIG(k + 2) && k + 3 < n && c[k + 3] == ':' && DIG(k + 4) && DIG(k + 5))) TSBAD();
        int oh = (c[k + 1] - '0') * 10 + (c[k + 2] - '0'), om = (c[k + 4] - '0') * 10 + (c[k + 5] - '0');
        if (oh > 23 || om > 59) TSBAD();
        off = (oh * 60 + om) * 60;
        if (neg) off = -off;
        k += 6;
    } else TSBAD();
    if (k != n) TSBAD();
    sp_i128 secs = (sp_i128)daysFromCivil(y, mo, d) * 86400 + hh * 3600 + mi * 60 + ss - off;
    sp_i128 ns = secs * 1000000000 + frac;
    if (ns < INT64_MIN || ns > INT64_MAX) TSBAD();
    return [SPInt i:ns];
#undef TSBAD
#undef DIG
}

// ---------------------------------------------------------------------------
// Reader

typedef NS_ENUM(int, LState) { LOpen = 1, LDefined };

@interface JLabel : NSObject
@property (nonatomic) LState state;
@property (nonatomic, strong) id node; // SPTableVal, or NSNull for a skipped definition
@end
@implementation JLabel
@end

static BOOL isNodeKey(NSData *k) { return k.length == 5 && memcmp(k.bytes, "&node", 5) == 0; }
static BOOL isAmpKey(NSData *k) { return k.length && ((const uint8_t *)k.bytes)[0] == '&'; }
static NSString *keyText(NSData *k) { return [[NSString alloc] initWithData:k encoding:NSUTF8StringEncoding] ?: @"?"; }

@interface JReader : NSObject
@property (nonatomic, strong) NSMutableDictionary<NSNumber *, JLabel *> *labels;
@property (nonatomic) NSInteger unknown;
@property (nonatomic) BOOL skipLabels; // SPEC-GAP 9: a fixed root's read
@end

@implementation JReader

- (void)nameCap:(NSData *)k {
    if (k.length > kNameCap) SPRefuse(@"out_of_range: a schema name past 255 bytes");
}

- (uint64_t)label:(JNode *)v {
    if (v.kind != JNum) JMAL(@"a label that is not a number");
    NSString *t = v.num;
    if (!t.length || [t characterAtIndex:0] < '1' || [t characterAtIndex:0] > '9') JMAL(@"a label that is not a positive integer spelled as one");
    uint64_t x = 0;
    for (NSUInteger k = 0; k < t.length; k++) {
        unichar c = [t characterAtIndex:k];
        if (c < '0' || c > '9') JMAL(@"a label that is not an integer");
        if (x > (UINT64_MAX - (c - '0')) / 10) JMAL(@"a label past 2^64-1");
        x = x * 10 + (c - '0');
    }
    return x;
}

// A value this build does not place, read for its labels at every depth (§16.7).
// SPEC-GAP 4: the 255-byte name cap reaches "every object and inside a skipped
// value", but a skipped value has no schema to say which keys are names and
// which are map keys (which the cap never reaches); every key is capped here.
// SPEC-GAP 5: an `&`-prefixed key other than `&node` inside a skipped value
// is not named by §16.7's rules; it is let through here.
- (void)skip:(JNode *)v {
    if (v.kind == JArr) { for (JNode *x in v.items) [self skip:x]; return; }
    if (v.kind != JObj) return;
    for (NSUInteger k = 0; k < v.keys.count; k++) {
        [self nameCap:v.keys[k]];
        if (k > 0 && isNodeKey(v.keys[k]) && !self.skipLabels) JMAL(@"&node after a field");
    }
    if (v.keys.count && isNodeKey(v.keys[0]) && !self.skipLabels) {
        uint64_t L = [self label:v.items[0]];
        JLabel *lb = self.labels[@(L)];
        if (lb) {
            if (lb.state == LOpen) JMAL(@"a reference inside its own definition");
            if (v.keys.count > 1) JMAL(@"a second definition of a label");
            return;
        }
        if (v.keys.count == 1) JMAL(@"a label never defined");
        lb = [JLabel new];
        lb.state = LOpen;
        self.labels[@(L)] = lb;
        for (NSUInteger k = 1; k < v.items.count; k++) [self skip:v.items[k]];
        lb.state = LDefined;
        lb.node = [NSNull null];
        return;
    }
    for (JNode *x in v.items) [self skip:x];
}

- (SPTableVal *)table:(SPDecl *)decl from:(JNode *)v start:(NSUInteger)start into:(SPTableVal *)t where:(NSString *)where {
    if (decl.jsonArray) {
        // W14: the table's text is its one field's array
        SPField *f = decl.fields[0];
        t.values[0] = [self value:v type:f.type where:[where stringByAppendingFormat:@".%@", f.name]];
        if (f.optional) t.present[0] = @YES;
        return t;
    }
    if (v.kind != JObj) SPRefuse(@"kind_mismatch: %@ is not an object", where);
    NSMutableSet *placed = [NSMutableSet set];
    for (NSUInteger k = start; k < v.keys.count; k++) {
        NSData *key = v.keys[k];
        [self nameCap:key];
        if (isAmpKey(key)) JMAL(@"an & key where a field is placed (%@)", where);
        NSInteger fi = -1;
        for (NSUInteger j = 0; j < decl.fields.count; j++)
            if ([[decl.fields[j].jsonKey dataUsingEncoding:NSUTF8StringEncoding] isEqualToData:key]) { fi = (NSInteger)j; break; }
        if (fi < 0) { [self skip:v.items[k]]; self.unknown++; continue; }
        SPField *f = decl.fields[(NSUInteger)fi];
        NSString *w = [where stringByAppendingFormat:@".%@", f.name];
        JNode *x = v.items[k];
        [placed addObject:@(fi)];
        if (f.optional && x.kind == JNull) {
            t.values[(NSUInteger)fi] = SPCopyValue(f.defaultValue);
            t.present[(NSUInteger)fi] = @NO;
            continue;
        }
        id val;
        if (f.type.kind == SPKEnum) val = [self enumValue:x type:f.type def:f.defaultValue where:w];
        else val = [self value:x type:f.type where:w];
        t.values[(NSUInteger)fi] = val;
        if (f.optional) t.present[(NSUInteger)fi] = @YES;
    }
    for (NSUInteger j = 0; j < decl.fields.count; j++)
        if (decl.fields[j].required && ![placed containsObject:@(j)]) SPRefuse(@"missing_required: %@.%@", where, decl.fields[j].name);
    return t;
}

- (id)pointer:(JNode *)v type:(SPType *)t where:(NSString *)where {
    if (v.kind == JNull) return [NSNull null];
    if (v.kind != JObj) SPRefuse(@"kind_mismatch: %@ is not an object", where);
    for (NSUInteger k = 1; k < v.keys.count; k++)
        if (isNodeKey(v.keys[k])) JMAL(@"&node after a field");
    if (v.keys.count && isNodeKey(v.keys[0])) {
        uint64_t L = [self label:v.items[0]];
        JLabel *lb = self.labels[@(L)];
        if (lb) {
            if (lb.state == LOpen) JMAL(@"a reference inside its own definition");
            if (v.keys.count > 1) JMAL(@"a second definition of a label");
            if (lb.node == [NSNull null]) return [NSNull null];
            if (((SPTableVal *)lb.node).decl != t.decl) SPRefuse(@"kind_mismatch: %@ names a node of another table", where);
            return lb.node;
        }
        if (v.keys.count == 1) JMAL(@"a label never defined");
        lb = [JLabel new];
        lb.state = LOpen;
        self.labels[@(L)] = lb;
        SPTableVal *node = SPNewTable(t.decl);
        [self table:t.decl from:v start:1 into:node where:where];
        lb.state = LDefined;
        lb.node = node;
        return node;
    }
    SPTableVal *node = SPNewTable(t.decl);
    return [self table:t.decl from:v start:0 into:node where:where];
}

- (id)enumValue:(JNode *)v type:(SPType *)t def:(id)def where:(NSString *)where {
    if (v.kind != JStr) SPRefuse(@"kind_mismatch: %@ an enum is its variant's name", where);
    [self nameCap:v.str];
    if (v.str.length == 4 && memcmp(v.str.bytes, "None", 4) == 0) return [SPInt u:0];
    NSArray<SPVariant *> *vs = t.decl.variants;
    for (NSUInteger k = 0; k < vs.count; k++) {
        if ([[vs[k].name dataUsingEncoding:NSUTF8StringEncoding] isEqualToData:v.str]) {
            if (vs[k].deprecated) return def ? SPCopyValue(def) : [SPInt u:0];
            return [SPInt u:k + 1];
        }
    }
    self.unknown++;
    return [SPInt u:0];
}

- (id)value:(JNode *)v type:(SPType *)t where:(NSString *)where {
    switch (t.kind) {
    case SPKBool:
        if (v.kind == JTrue) return [SPInt u:1];
        if (v.kind == JFalse) return [SPInt u:0];
        SPRefuse(@"kind_mismatch: %@ bool", where);
    case SPKInt: case SPKBits: {
        BOOL wide = t.kind == SPKBits ? t.bits > 32 : t.bits > 32;
        if (wide) {
            if (v.kind != JStr) SPRefuse(@"kind_mismatch: %@ a wide integer is a string", where);
            return intFromDigits(v.str, t, where);
        }
        if (v.kind != JNum) SPRefuse(@"kind_mismatch: %@ a number", where);
        return intFromDigits([v.num dataUsingEncoding:NSASCIIStringEncoding], t, where);
    }
    case SPKInt128:
        if (v.kind != JStr) SPRefuse(@"kind_mismatch: %@ a 128-bit integer is a string", where);
        return intFromDigits(v.str, t, where);
    case SPKTimestamp:
        if (v.kind != JStr) SPRefuse(@"kind_mismatch: %@ a timestamp is a string", where);
        return timestampRead(v.str, where);
    case SPKFixed: {
        NSString *tok;
        if (storageBits(t) > 32) {
            if (v.kind != JStr) SPRefuse(@"kind_mismatch: %@ a wide fixed is a string", where);
            if (!numberGrammar(v.str)) SPRefuse(@"kind_mismatch: %@ not a number spelled as one", where);
            tok = [[NSString alloc] initWithData:v.str encoding:NSASCIIStringEncoding];
        } else {
            if (v.kind != JNum) SPRefuse(@"kind_mismatch: %@ a number", where);
            tok = v.num;
        }
        Dec d = decOf(tok);
        sp_u128 mag = decScaled(d, t.fixedF, where);
        return signedInto(mag, d.neg, t, where);
    }
    case SPKFloat: {
        if (v.kind != JNum) SPRefuse(@"kind_mismatch: %@ a number", where);
        double x;
        if (t.bits == 32) {
            float f = strtof(v.num.UTF8String, NULL);
            if (isinf(f)) SPRefuse(@"out_of_range: %@ past float32's range", where);
            x = f;
        } else {
            x = strtod(v.num.UTF8String, NULL);
            if (isinf(x)) SPRefuse(@"out_of_range: %@ past float64's range", where);
        }
        if (t.hasRange) {
            if (x < t.fmin || x > t.fmax) SPRefuse(@"out_of_range: %@", where);
            if (x == 0) x = 0.0;
        }
        return [SPFloat d:x];
    }
    case SPKString:
        if (v.kind != JStr) SPRefuse(@"kind_mismatch: %@ a string", where);
        if (t.cap >= 0 && (long long)v.str.length > t.cap) SPRefuse(@"length_over_bound: %@", where);
        return v.str;
    case SPKBytes: {
        if (v.kind != JStr) SPRefuse(@"kind_mismatch: %@ base64", where);
        NSData *b = b64DecodeCanonical(v.str, where);
        if ((long long)b.length > t.cap) SPRefuse(@"length_over_bound: %@", where);
        return b;
    }
    case SPKEnum:
        return [self enumValue:v type:t def:nil where:where];
    case SPKFlags: {
        if (v.kind != JArr) SPRefuse(@"kind_mismatch: %@ flags are a name array", where);
        sp_u128 m = 0;
        for (JNode *x in v.items) {
            if (x.kind != JStr) SPRefuse(@"kind_mismatch: %@ a flags element", where);
            [self nameCap:x.str];
            BOOL found = NO;
            NSArray<SPVariant *> *vs = t.decl.variants;
            for (NSUInteger k = 0; k < vs.count; k++) {
                if ([[vs[k].name dataUsingEncoding:NSUTF8StringEncoding] isEqualToData:x.str]) {
                    if (m >> k & 1) JMAL(@"a flags name twice");
                    m |= (sp_u128)1 << k;
                    found = YES;
                }
            }
            if (!found) self.unknown++;
        }
        return [SPInt u:m];
    }
    case SPKStruct: {
        if (!t.decl.jsonArray && v.kind == JObj)
            for (NSData *k in v.keys) if (isAmpKey(k)) JMAL(@"& key in a by-value nesting");
        return [self table:t.decl from:v start:0 into:SPNewTable(t.decl) where:where];
    }
    case SPKUnion: {
        SPUnionVal *u = [SPUnionVal new];
        u.value = [NSNull null];
        if (v.kind != JObj) SPRefuse(@"kind_mismatch: %@ a union is an object", where);
        if (v.keys.count == 0) return u;
        if (v.keys.count > 1) JMAL(@"a union with two keys");
        NSData *key = v.keys[0];
        [self nameCap:key];
        if (isAmpKey(key)) JMAL(@"& key in a union");
        NSInteger ai = -1;
        for (NSUInteger k = 0; k < t.decl.fields.count; k++)
            if ([[t.decl.fields[k].jsonKey dataUsingEncoding:NSUTF8StringEncoding] isEqualToData:key]) { ai = (NSInteger)k; break; }
        if (ai < 0) { [self skip:v.items[0]]; self.unknown++; return u; }
        SPField *arm = t.decl.fields[(NSUInteger)ai];
        JNode *x = v.items[0];
        NSString *w = [where stringByAppendingFormat:@".%@", arm.name];
        u.arm = ai + 1;
        if (!arm.type) {
            if (x.kind != JNull) SPRefuse(@"kind_mismatch: %@ a payload-free arm is null", w);
            return u;
        }
        u.value = arm.type.kind == SPKEnum ? [self enumValue:x type:arm.type def:arm.defaultValue where:w] : [self value:x type:arm.type where:w];
        return u;
    }
    case SPKPointer:
        return [self pointer:v type:t where:where];
    case SPKBlobBytes: case SPKBlobString: {
        if (v.kind == JNull) return [NSNull null];
        if (v.kind != JStr) SPRefuse(@"kind_mismatch: %@ a blob", where);
        SPBlob *b = [SPBlob new];
        b.isString = t.kind == SPKBlobString;
        b.data = b.isString ? v.str : b64DecodeCanonical(v.str, where);
        return b;
    }
    case SPKArray: {
        if (v.kind != JArr) SPRefuse(@"kind_mismatch: %@ an array", where);
        NSUInteger n = v.items.count;
        if (t.shape != SPArrayUnbounded && (long long)n > t.maxCount) SPRefuse(@"count_over_bound: %@", where);
        if (t.shape == SPArrayCounted && (long long)n < t.minCount) SPRefuse(@"out_of_range: %@ below its minimum", where);
        NSMutableArray *a = [NSMutableArray arrayWithCapacity:n];
        long long sum = 0;
        for (NSUInteger k = 0; k < n; k++) {
            JNode *x = v.items[k];
            NSString *w = [where stringByAppendingFormat:@"[%lu]", (unsigned long)k];
            if (t.elemOptional && x.kind == JNull) { [a addObject:[NSNull null]]; continue; }
            id e = t.elem.kind == SPKEnum ? [self enumValue:x type:t.elem def:nil where:w] : [self value:x type:t.elem where:w];
            if (t.elem.kind == SPKString) sum += (long long)((NSData *)e).length;
            [a addObject:e];
        }
        if (t.total >= 0 && sum > t.total) SPRefuse(@"length_over_bound: %@ past its total", where);
        if (t.shape == SPArrayFixed)
            while ((long long)a.count < t.maxCount) [a addObject:SPNewValue(t.elem)];
        return a;
    }
    case SPKKeyed: {
        if (v.kind != JObj) SPRefuse(@"kind_mismatch: %@ an enum-keyed array is an object", where);
        NSMutableArray *a = SPNewValue(t);
        for (NSUInteger k = 0; k < v.keys.count; k++) {
            NSData *key = v.keys[k];
            [self nameCap:key];
            if (isAmpKey(key)) JMAL(@"& key in an enum-keyed array");
            NSInteger vi = -1;
            NSArray<SPVariant *> *vs = t.keyEnum.variants;
            for (NSUInteger j = 0; j < vs.count; j++)
                if (!vs[j].deprecated && [[vs[j].name dataUsingEncoding:NSUTF8StringEncoding] isEqualToData:key]) vi = (NSInteger)j;
            if (vi < 0) { [self skip:v.items[k]]; self.unknown++; continue; }
            NSString *w = [where stringByAppendingFormat:@"[%@]", vs[(NSUInteger)vi].name];
            a[(NSUInteger)vi] = t.elem.kind == SPKEnum ? [self enumValue:v.items[k] type:t.elem def:nil where:w] : [self value:v.items[k] type:t.elem where:w];
        }
        return a;
    }
    case SPKMap: {
        if (v.kind != JObj) SPRefuse(@"kind_mismatch: %@ a map is an object", where);
        NSMutableArray *es = [NSMutableArray array];
        for (NSUInteger k = 0; k < v.keys.count; k++) {
            NSData *key = v.keys[k];
            SPMapEntry *e = [SPMapEntry new];
            if (t.key.kind == SPKString) {
                if (t.key.cap >= 0 && (long long)key.length > t.key.cap) SPRefuse(@"length_over_bound: %@ key", where);
                e.key = key;
            } else {
                e.key = intFromDigits(key, t.key, [where stringByAppendingString:@" key"]);
            }
            JNode *x = v.items[k];
            NSString *w = [where stringByAppendingFormat:@"[%@]", keyText(key)];
            if (t.elemOptional && x.kind == JNull) e.value = [NSNull null];
            else e.value = t.elem.kind == SPKEnum ? [self enumValue:x type:t.elem def:nil where:w] : [self value:x type:t.elem where:w];
            [es addObject:e];
        }
        [es sortUsingComparator:^NSComparisonResult(SPMapEntry *a, SPMapEntry *b) {
            return (NSComparisonResult)SPCompareKeys(a.key, b.key, t.key);
        }];
        for (NSUInteger k = 1; k < es.count; k++)
            if (SPCompareKeys([es[k - 1] key], [es[k] key], t.key) == 0) JMAL(@"a repeated map key");
        return es;
    }
    }
    SPRefuse(@"kind_mismatch: %@", where);
}
@end

SPTableVal *SPFromJson(NSData *text, SPDecl *root, SPUnit *unit) {
    JNode *doc = [JParser parse:text];
    JReader *r = [JReader new];
    r.labels = [NSMutableDictionary dictionary];
    // SPEC-GAP 9: §16.7 holds every `&node` inside a skipped value to the
    // label rules, but the corpus row reserved-ampersand-in-skipped-value
    // (`{ "mystery": { "&node": 1 } }` at the FIXED root RootConfig) owes
    // `read`. Following the corpus: a fixed root's read, whose closure holds
    // no pointer to name, does not police labels inside a value it skips.
    r.skipLabels = root.fixed;
    if (doc.kind == JObj)
        for (NSData *k in doc.keys) if (isAmpKey(k)) JMAL(@"& key at the root");
    if (!root.jsonArray && doc.kind != JObj) SPRefuse(@"kind_mismatch: the root is not an object");
    return [r table:root from:doc start:0 into:SPNewTable(root) where:root.name];
}

// ---------------------------------------------------------------------------
// Writer (§16.14)

@interface JWriter : NSObject
@property (nonatomic, strong) NSMutableData *out;
@property (nonatomic, strong) NSMapTable *namings; // node -> NSNumber count
@property (nonatomic, strong) NSMapTable *open;
@property (nonatomic, strong) NSMapTable *labelOf; // node -> NSNumber label
@property (nonatomic) uint64_t nextLabel;
@property (nonatomic, strong) SPTableVal *root;
@end

@implementation JWriter

- (void)s:(const char *)c { [self.out appendBytes:c length:strlen(c)]; }
- (void)ns:(NSString *)s { NSData *d = [s dataUsingEncoding:NSUTF8StringEncoding]; [self.out appendData:d]; }
- (void)nl:(int)d {
    [self s:"\n"];
    for (int k = 0; k < d; k++) [self s:"  "];
}

// W1: count every slot that names each node; refuse a cycle and a shared blob
- (void)countTable:(SPTableVal *)t {
    for (NSUInteger i = 0; i < t.decl.fields.count; i++) {
        SPField *f = t.decl.fields[i];
        if (!SPGuardsHold(t, f)) continue;
        if (f.optional && ![t.present[i] boolValue]) continue;
        [self count:t.values[i] type:f.type];
        if (t.decl.jsonArray) break;
    }
}
- (void)count:(id)v type:(SPType *)t {
    switch (t.kind) {
    case SPKStruct: [self countTable:v]; return;
    case SPKUnion: {
        SPUnionVal *u = v;
        if (u.arm <= 0 || u.arm > (NSInteger)t.decl.fields.count) return;
        SPField *a = t.decl.fields[(NSUInteger)u.arm - 1];
        if (a.type) [self count:u.value type:a.type];
        return;
    }
    case SPKPointer: case SPKBlobBytes: case SPKBlobString: {
        if (v == [NSNull null]) return;
        if (v == self.root) SPRefuse(@"data_cycle: a pointer names the root");
        NSNumber *c = [self.namings objectForKey:v];
        if (c) {
            if (t.kind != SPKPointer) SPRefuse(@"blob_shared");
            if ([self.open objectForKey:v]) SPRefuse(@"data_cycle");
            [self.namings setObject:@(c.integerValue + 1) forKey:v];
            return;
        }
        [self.namings setObject:@1 forKey:v];
        if (t.kind == SPKPointer) {
            [self.open setObject:@YES forKey:v];
            [self countTable:v];
            [self.open removeObjectForKey:v];
        }
        return;
    }
    case SPKArray: case SPKKeyed:
        for (id x in (NSArray *)v) if (x != [NSNull null] || !(t.kind == SPKArray && t.elemOptional)) [self count:x type:t.elem];
        return;
    case SPKMap:
        for (SPMapEntry *e in (NSArray *)v) if (!(t.elemOptional && e.value == [NSNull null])) [self count:e.value type:t.elem];
        return;
    default: return;
    }
}

- (void)str:(NSData *)s where:(NSString *)where {
    if (!SPValidUTF8(s, NO)) SPRefuse(@"invalid_value: %@ is not text", where);
    [self s:"\""];
    const uint8_t *p = s.bytes;
    NSUInteger n = s.length, run = 0;
    for (NSUInteger k = 0; k < n; k++) {
        uint8_t c = p[k];
        const char *esc = NULL;
        char ubuf[8];
        if (c == '"') esc = "\\\"";
        else if (c == '\\') esc = "\\\\";
        else if (c == 8) esc = "\\b";
        else if (c == 12) esc = "\\f";
        else if (c == 10) esc = "\\n";
        else if (c == 13) esc = "\\r";
        else if (c == 9) esc = "\\t";
        else if (c < 0x20) { snprintf(ubuf, sizeof ubuf, "\\u%04x", c); esc = ubuf; }
        if (esc) {
            [self.out appendBytes:p + run length:k - run];
            [self s:esc];
            run = k + 1;
        }
    }
    [self.out appendBytes:p + run length:n - run];
    [self s:"\""];
}

- (void)floatValue:(double)v bits:(int)bits {
    int L = bits == 32 ? 6 : 15, H = bits == 32 ? 9 : 17;
    char buf[64];
    for (int p = L; p <= H; p++) {
        snprintf(buf, sizeof buf, "%.*g", p, v);
        if (p == H) break;
        if (bits == 32) { if (strtof(buf, NULL) == (float)v) break; }
        else if (strtod(buf, NULL) == v) break;
    }
    [self s:buf];
}

- (NSString *)fixedText:(sp_u128)raw type:(SPType *)t {
    BOOL neg = SPTypeSigned(t) && (sp_i128)raw < 0;
    sp_u128 mag = neg ? (sp_u128)0 - raw : raw;
    int F = t.fixedF;
    sp_u128 whole = F >= 128 ? 0 : mag >> F;
    sp_u128 frac = F == 0 ? 0 : mag & ((((sp_u128)1) << F) - 1);
    NSMutableString *s = [NSMutableString string];
    if (neg) [s appendString:@"-"];
    [s appendString:SPU128Decimal(whole, NO)];
    [s appendString:@"."];
    if (!frac) [s appendString:@"0"];
    else {
        // digits one at a time; frac < 2^F, ×10 may pass 128 bits for F > 124
        Big b;
        bigInit(&b, 8);
        for (int k = 3; k >= 0; k--) {
            bigMulAdd(&b, 1u << 16, 0);
            bigMulAdd(&b, 1u << 16, (uint32_t)(frac >> (32 * k)));
        }
        // b = frac; digit = (b*10) >> F
        for (int guard = 0; guard < F + 1; guard++) {
            bigMulAdd(&b, 10, 0);
            // integer part = b >> F, remainder = b mod 2^F
            int limb = F / 32, bit = F % 32;
            uint64_t digit = 0;
            for (int k = b.n - 1; k >= limb; k--) {
                uint64_t w = b.w[k];
                if (k == limb) w >>= bit;
                else w <<= (32 * (k - limb) - bit);
                digit |= w;
            }
            [s appendFormat:@"%d", (int)digit];
            for (int k = limb + 1; k < b.n; k++) b.w[k] = 0;
            if (limb < b.n) b.w[limb] &= bit ? ((1u << bit) - 1) : 0;
            while (b.n && !b.w[b.n - 1]) b.n--;
            if (!b.n) break;
        }
        bigFree(&b);
    }
    return s;
}

- (void)value:(id)v type:(SPType *)t depth:(int)d where:(NSString *)where {
    SPCheckScalar(v, t, where);
    switch (t.kind) {
    case SPKBool: [self s:((SPInt *)v)->raw ? "true" : "false"]; return;
    case SPKInt: case SPKBits: case SPKInt128: {
        NSString *txt = intText(((SPInt *)v)->raw, SPTypeSigned(t));
        BOOL wide = t.kind == SPKInt128 || t.bits > 32;
        if (wide) [self str:[txt dataUsingEncoding:NSASCIIStringEncoding] where:where];
        else [self ns:txt];
        return;
    }
    case SPKFixed: {
        NSString *txt = [self fixedText:((SPInt *)v)->raw type:t];
        if (storageBits(t) > 32) [self str:[txt dataUsingEncoding:NSASCIIStringEncoding] where:where];
        else [self ns:txt];
        return;
    }
    case SPKTimestamp:
        [self str:[timestampText((sp_i128)((SPInt *)v)->raw) dataUsingEncoding:NSASCIIStringEncoding] where:where];
        return;
    case SPKFloat: {
        double x = ((SPFloat *)v)->d;
        if (t.bits == 32) x = (float)x;
        [self floatValue:x bits:t.bits];
        return;
    }
    case SPKString: [self str:v where:where]; return;
    case SPKBytes: [self.out appendData:[@"\"" dataUsingEncoding:NSASCIIStringEncoding]]; [self.out appendData:b64Encode(v)]; [self s:"\""]; return;
    case SPKEnum: {
        sp_u128 k = ((SPInt *)v)->raw;
        NSString *nm = k ? t.decl.variants[(NSUInteger)k - 1].name : @"None";
        [self str:[nm dataUsingEncoding:NSUTF8StringEncoding] where:where];
        return;
    }
    case SPKFlags: {
        sp_u128 m = ((SPInt *)v)->raw;
        NSUInteger nv = t.decl.variants.count;
        if (nv < 128 && (m >> nv)) SPRefuse(@"invalid_value: %@ a flags bit no variant names", where);
        if (!m) { [self s:"[]"]; return; }
        [self s:"["];
        BOOL first = YES;
        for (NSUInteger k = 0; k < nv; k++) {
            if (!(m >> k & 1)) continue;
            if (!first) [self s:","];
            first = NO;
            [self nl:d + 1];
            [self str:[t.decl.variants[k].name dataUsingEncoding:NSUTF8StringEncoding] where:where];
        }
        [self nl:d];
        [self s:"]"];
        return;
    }
    case SPKStruct: [self table:v depth:d where:where label:0 definition:NO]; return;
    case SPKPointer: case SPKBlobBytes: case SPKBlobString: {
        if (v == [NSNull null]) { [self s:"null"]; return; }
        if (t.kind == SPKBlobBytes) {
            SPBlob *b = v;
            [self s:"\""]; [self.out appendData:b64Encode(b.data)]; [self s:"\""];
            return;
        }
        if (t.kind == SPKBlobString) { [self str:((SPBlob *)v).data where:where]; return; }
        NSInteger c = [[self.namings objectForKey:v] integerValue];
        if (c > 1) {
            NSNumber *lb = [self.labelOf objectForKey:v];
            if (lb) {
                if (d > kJsonMaxDepth) SPRefuse(@"invalid_value: %@ past the depth cap", where);
                [self s:"{"]; [self nl:d + 1];
                [self ns:[NSString stringWithFormat:@"\"&node\": %llu", lb.unsignedLongLongValue]];
                [self nl:d]; [self s:"}"];
                return;
            }
            uint64_t L = ++self.nextLabel;
            [self.labelOf setObject:@(L) forKey:v];
            [self table:v depth:d where:where label:L definition:YES];
            return;
        }
        [self table:v depth:d where:where label:0 definition:NO];
        return;
    }
    case SPKUnion: {
        SPUnionVal *u = v;
        if (u.arm < 0 || u.arm > (NSInteger)t.decl.fields.count) SPRefuse(@"invalid_value: %@ union tag outside its arms", where);
        if (u.arm == 0) { [self s:"{}"]; return; }
        SPField *arm = t.decl.fields[(NSUInteger)u.arm - 1];
        [self s:"{"]; [self nl:d + 1];
        [self str:[arm.jsonKey dataUsingEncoding:NSUTF8StringEncoding] where:where];
        [self s:": "];
        if (!arm.type) [self s:"null"];
        else [self value:u.value type:arm.type depth:d + 1 where:[where stringByAppendingFormat:@".%@", arm.name]];
        [self nl:d]; [self s:"}"];
        return;
    }
    case SPKArray: {
        NSArray *a = v;
        if (t.shape == SPArrayFixed && (long long)a.count != t.maxCount) SPRefuse(@"invalid_value: %@ fixed array count", where);
        if (t.shape == SPArrayCounted && ((long long)a.count > t.maxCount || (long long)a.count < t.minCount)) SPRefuse(@"invalid_value: %@ count off its bound", where);
        if (t.total >= 0) {
            long long sum = 0;
            for (id x in a) if (x != [NSNull null]) sum += (long long)((NSData *)x).length;
            if (sum > t.total) SPRefuse(@"invalid_value: %@ past its total", where);
        }
        if (!a.count) { [self s:"[]"]; return; }
        [self s:"["];
        for (NSUInteger k = 0; k < a.count; k++) {
            if (k) [self s:","];
            [self nl:d + 1];
            NSString *w = [where stringByAppendingFormat:@"[%lu]", (unsigned long)k];
            if (t.elemOptional && a[k] == [NSNull null]) [self s:"null"];
            else [self value:a[k] type:t.elem depth:d + 1 where:w];
        }
        [self nl:d]; [self s:"]"];
        return;
    }
    case SPKKeyed: {
        NSArray *a = v;
        NSArray<SPVariant *> *vs = t.keyEnum.variants;
        if (!vs.count) { [self s:"{}"]; return; }
        [self s:"{"];
        for (NSUInteger k = 0; k < vs.count; k++) {
            if (k) [self s:","];
            [self nl:d + 1];
            [self str:[vs[k].name dataUsingEncoding:NSUTF8StringEncoding] where:where];
            [self s:": "];
            [self value:a[k] type:t.elem depth:d + 1 where:[where stringByAppendingFormat:@"[%@]", vs[k].name]];
        }
        [self nl:d]; [self s:"}"];
        return;
    }
    case SPKMap: {
        NSArray<SPMapEntry *> *es = [(NSArray *)v sortedArrayUsingComparator:^NSComparisonResult(SPMapEntry *a, SPMapEntry *b) {
            return (NSComparisonResult)SPCompareKeys(a.key, b.key, t.key);
        }];
        for (NSUInteger k = 1; k < es.count; k++)
            if (SPCompareKeys(es[k - 1].key, es[k].key, t.key) == 0) SPRefuse(@"invalid_value: %@ a key twice", where);
        if (!es.count) { [self s:"{}"]; return; }
        [self s:"{"];
        for (NSUInteger k = 0; k < es.count; k++) {
            if (k) [self s:","];
            [self nl:d + 1];
            SPMapEntry *e = es[k];
            SPCheckScalar(e.key, t.key, where);
            if (t.key.kind == SPKString) [self str:e.key where:where];
            else [self str:[intText(((SPInt *)e.key)->raw, SPTypeSigned(t.key)) dataUsingEncoding:NSASCIIStringEncoding] where:where];
            [self s:": "];
            if (t.elemOptional && e.value == [NSNull null]) [self s:"null"];
            else [self value:e.value type:t.elem depth:d + 1 where:where];
        }
        [self nl:d]; [self s:"}"];
        return;
    }
    }
}

- (void)table:(SPTableVal *)t depth:(int)d where:(NSString *)where label:(uint64_t)label definition:(BOOL)def {
    if (d > kJsonMaxDepth) SPRefuse(@"invalid_value: %@ past the depth cap", where);
    SPDecl *decl = t.decl;
    if (decl.jsonArray) {
        SPField *f = decl.fields[0];
        [self value:t.values[0] type:f.type depth:d where:[where stringByAppendingFormat:@".%@", f.name]];
        return;
    }
    NSMutableArray<NSNumber *> *keep = [NSMutableArray array];
    for (NSUInteger i = 0; i < decl.fields.count; i++) {
        SPField *f = decl.fields[i];
        if (!SPGuardsHold(t, f)) continue;
        if (f.optional && ![t.present[i] boolValue]) continue;
        [keep addObject:@(i)];
    }
    if (def && !keep.count) SPRefuse(@"invalid_value: %@ a shared node with no field to define it", where);
    if (!keep.count) { [self s:"{}"]; return; }
    [self s:"{"];
    BOOL first = YES;
    if (def) {
        [self nl:d + 1];
        [self ns:[NSString stringWithFormat:@"\"&node\": %llu", label]];
        first = NO;
    }
    for (NSNumber *n in keep) {
        SPField *f = decl.fields[n.unsignedIntegerValue];
        if (!first) [self s:","];
        first = NO;
        [self nl:d + 1];
        [self str:[f.jsonKey dataUsingEncoding:NSUTF8StringEncoding] where:where];
        [self s:": "];
        [self value:t.values[n.unsignedIntegerValue] type:f.type depth:d + 1 where:[where stringByAppendingFormat:@".%@", f.name]];
    }
    [self nl:d];
    [self s:"}"];
}
@end

NSData *SPToJson(SPTableVal *root) {
    JWriter *w = [JWriter new];
    w.out = [NSMutableData data];
    w.namings = [NSMapTable mapTableWithKeyOptions:NSPointerFunctionsObjectPointerPersonality valueOptions:NSPointerFunctionsStrongMemory];
    w.open = [NSMapTable mapTableWithKeyOptions:NSPointerFunctionsObjectPointerPersonality valueOptions:NSPointerFunctionsStrongMemory];
    w.labelOf = [NSMapTable mapTableWithKeyOptions:NSPointerFunctionsObjectPointerPersonality valueOptions:NSPointerFunctionsStrongMemory];
    w.root = root;
    [w countTable:root];
    [w table:root depth:0 where:root.decl.name label:0 definition:NO];
    [w s:"\n"];
    return w.out;
}
