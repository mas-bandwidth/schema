// SPSchema.m — scanner, parser and resolver for `.schema` text
// (docs/SPEC.md §4.1, §4.2; docs/SPEC-TABLES.md §2, §5).

#import "SPSchema.h"
#import "SPValue.h"

NSString *const SPRefusal = @"SPRefusal";
NSString *const SPSchemaError = @"SPSchemaError";

void SPRefuse(NSString *reason, ...) {
    va_list ap;
    va_start(ap, reason);
    NSString *s = [[NSString alloc] initWithFormat:reason arguments:ap];
    va_end(ap);
    @throw [NSException exceptionWithName:SPRefusal reason:s userInfo:nil];
}

void SPSchemaFail(NSString *reason, ...) {
    va_list ap;
    va_start(ap, reason);
    NSString *s = [[NSString alloc] initWithFormat:reason arguments:ap];
    va_end(ap);
    @throw [NSException exceptionWithName:SPSchemaError reason:s userInfo:nil];
}

uint64_t SPFnv1a64(NSData *bytes) {
    uint64_t h = 0xcbf29ce484222325ULL;
    const uint8_t *p = bytes.bytes;
    for (NSUInteger i = 0; i < bytes.length; i++) {
        h ^= p[i];
        h *= 0x100000001b3ULL;
    }
    return h;
}

uint64_t SPFnv1a64Str(NSString *s) {
    return SPFnv1a64([s dataUsingEncoding:NSUTF8StringEncoding]);
}

// ---------------------------------------------------------------------------
// Model

@implementation SPType
- (instancetype)init {
    if ((self = [super init])) { _cap = -1; _total = -1; }
    return self;
}
- (int)wireKind {
    switch (_kind) {
    case SPKBool: return 1;
    case SPKInt:
        if (_isSigned) return _bits == 8 ? 2 : _bits == 16 ? 3 : _bits == 32 ? 4 : 5;
        return _bits == 8 ? 6 : _bits == 16 ? 7 : _bits == 32 ? 8 : 9;
    case SPKBits: return _bits <= 8 ? 6 : _bits <= 16 ? 7 : _bits <= 32 ? 8 : 9;
    case SPKFloat: return (_bits == 32 || _hasRange) ? 10 : 11;
    case SPKString: return 12;
    case SPKBytes: return 14;
    case SPKFixed: {
        int w = _fixedI + _fixedF;
        int base = _isSigned ? 20 : 25;
        return base + (w == 8 ? 0 : w == 16 ? 1 : w == 32 ? 2 : w == 64 ? 3 : 4);
    }
    case SPKInt128: return _isSigned ? 18 : 19;
    case SPKTimestamp: return 36;
    case SPKEnum: return 30;
    case SPKFlags: return 9;
    case SPKStruct: return 13;
    case SPKUnion: return 15;
    case SPKPointer: case SPKBlobBytes: case SPKBlobString: return 17;
    case SPKArray: return 14;
    case SPKKeyed: return 16;
    case SPKMap: return 14;
    }
    return 0;
}
- (BOOL)isStructural {
    return _kind == SPKStruct || _kind == SPKArray || _kind == SPKKeyed || _kind == SPKMap
        || _kind == SPKBytes;
}
@end

@implementation SPGuard
@end
@implementation SPField
@end
@implementation SPVariant
@end

@implementation SPDecl
- (NSInteger)variantIndexForWireId:(uint64_t)wid {
    for (NSUInteger i = 0; i < _variants.count; i++)
        if (_variants[i].wireId == wid) return (NSInteger)i + 1;
    return 0;
}
- (NSInteger)fieldIndexForWireId:(uint64_t)wid {
    for (NSUInteger i = 0; i < _fields.count; i++)
        if (_fields[i].wireId == wid) return (NSInteger)i;
    return -1;
}
@end

// ---------------------------------------------------------------------------
// Scanner

typedef NS_ENUM(int, SPTok) { TEOF, TNL, TIdent, TInt, TFloat, TString, TPunct };

@interface SPToken : NSObject
@property (nonatomic) SPTok t;
@property (nonatomic, strong) NSString *s;
@property (nonatomic) int line;
@end
@implementation SPToken
@end

static NSArray<SPToken *> *scan(NSData *src, NSString *file) {
    NSMutableArray *out = [NSMutableArray array];
    const uint8_t *p = src.bytes;
    NSUInteger n = src.length, i = 0;
    int line = 1;
    if (n >= 3 && p[0] == 0xEF && p[1] == 0xBB && p[2] == 0xBF)
        SPSchemaFail(@"%@: a UTF-8 BOM is rejected (SPEC §4.1)", file);
    void (^emit)(SPTok, NSString *) = ^(SPTok t, NSString *s) {
        SPToken *k = [SPToken new];
        k.t = t; k.s = s; k.line = line;
        [out addObject:k];
    };
    while (i < n) {
        uint8_t c = p[i];
        if (c == '\n') {
            SPToken *last = out.lastObject;
            if (last && last.t != TNL) emit(TNL, @"\n");
            line++; i++; continue;
        }
        if (c == ' ' || c == '\t' || c == '\r') { i++; continue; }
        if (c == '/' && i + 1 < n && p[i + 1] == '/') {
            while (i < n && p[i] != '\n') i++;
            continue;
        }
        if (c == '/' && i + 1 < n && p[i + 1] == '*') {
            i += 2;
            while (i + 1 < n && !(p[i] == '*' && p[i + 1] == '/')) { if (p[i] == '\n') line++; i++; }
            if (i + 1 >= n) SPSchemaFail(@"%@:%d: unclosed block comment", file, line);
            i += 2; continue;
        }
        if (isalpha(c) || c == '_') {
            NSUInteger s = i;
            while (i < n && (isalnum(p[i]) || p[i] == '_')) i++;
            emit(TIdent, [[NSString alloc] initWithBytes:p + s length:i - s encoding:NSASCIIStringEncoding]);
            continue;
        }
        if (isdigit(c)) {
            NSUInteger s = i;
            BOOL isFloat = NO;
            if (c == '0' && i + 1 < n && (p[i + 1] == 'x' || p[i + 1] == 'X' || p[i + 1] == 'b' || p[i + 1] == 'B')) {
                i += 2;
                while (i < n && (isxdigit(p[i]) || p[i] == '_')) i++;
            } else {
                while (i < n && isdigit(p[i])) i++;
                if (i + 1 < n && p[i] == '.' && isdigit(p[i + 1])) {
                    isFloat = YES; i++;
                    while (i < n && isdigit(p[i])) i++;
                } else if (i < n && p[i] == '.' && !(i + 1 < n && p[i + 1] == '.')) {
                    isFloat = YES; i++;
                }
                if (i < n && (p[i] == 'e' || p[i] == 'E')) {
                    isFloat = YES; i++;
                    if (i < n && (p[i] == '+' || p[i] == '-')) i++;
                    while (i < n && isdigit(p[i])) i++;
                }
                if (p[s] == '0' && i - s > 1 && isdigit(p[s + 1]))
                    SPSchemaFail(@"%@:%d: a decimal number never starts with 0 followed by a digit (SPEC §4.1)", file, line);
            }
            emit(isFloat ? TFloat : TInt, [[NSString alloc] initWithBytes:p + s length:i - s encoding:NSASCIIStringEncoding]);
            continue;
        }
        if (c == '"') {
            NSUInteger s = ++i;
            while (i < n && p[i] != '"' && p[i] != '\n') i++;
            if (i >= n || p[i] != '"') SPSchemaFail(@"%@:%d: unterminated string", file, line);
            // the literal's bytes, with no escapes (SPEC §4.2)
            NSString *str = [[NSString alloc] initWithBytes:p + s length:i - s encoding:NSUTF8StringEncoding];
            if (!str) SPSchemaFail(@"%@:%d: a string literal is not UTF-8", file, line);
            i++;
            emit(TString, str);
            continue;
        }
        if (c == '.' && i + 1 < n && p[i + 1] == '.') { emit(TPunct, @".."); i += 2; continue; }
        if (c == ':' && i + 1 < n && p[i + 1] == ':') { emit(TPunct, @"::"); i += 2; continue; }
        if (strchr("{}()[],:=!?.|+-*/%", c)) {
            emit(TPunct, [NSString stringWithFormat:@"%c", c]);
            i++; continue;
        }
        SPSchemaFail(@"%@:%d: unexpected byte 0x%02x", file, line, c);
    }
    emit(TNL, @"\n");
    emit(TEOF, @"");
    return out;
}

// ---------------------------------------------------------------------------
// Expressions: integer arithmetic in 128 bits (the corpus never needs more;
// SPEC §4.2 asks for arbitrary precision with a fit check at the use site),
// float arithmetic in float64.

@interface SPExpr : NSObject
@property (nonatomic, strong) NSString *op;   // "int" "float" "name" "neg" "+" "-" "*" "/" "%" "max" "count"
@property (nonatomic, strong) NSString *text;
@property (nonatomic, strong) SPExpr *a, *b;
@end
@implementation SPExpr
@end

@implementation SPNum
- (double)asDouble { return _isFloat ? _d : (double)_i; }
@end

// ---------------------------------------------------------------------------
// Parser

@interface SPParser : NSObject {
    NSArray<SPToken *> *_toks;
    NSUInteger _pos;
    NSString *_file;
}
@property (nonatomic, strong) SPUnit *unit;
@property (nonatomic, strong) NSMutableDictionary<NSString *, SPExpr *> *constExprs;
@end

// raw parse products, resolved after every file is read
@interface SPRawType : NSObject
@property (nonatomic) BOOL optional;
@property (nonatomic) int boundForm; // 0 none, 1 [expr], 2 [..expr], 3 [a..b], 4 []
@property (nonatomic, strong) SPExpr *b1, *b2;
@property (nonatomic) BOOL elemOptional;
@property (nonatomic, strong) NSString *scalar; // a keyword or a declared name
@property (nonatomic, strong) SPExpr *p1, *p2;  // bits(N), string(N), fixed(I,F)
@property (nonatomic) BOOL pointer;
@property (nonatomic, strong) SPRawType *mapKey, *mapValue; // map
@end
@implementation SPRawType
@end

@interface SPRawField : NSObject
@property (nonatomic, strong) NSString *name;
@property (nonatomic, strong) SPRawType *type;
@property (nonatomic, strong) id defaultLit; // SPExpr, NSString ident, NSData string, NSArray brace list
@property (nonatomic, strong) NSArray *attrs; // [key, value-or-NSNull]
@property (nonatomic, strong) NSArray *guards; // [[name, @(want)]]
@property (nonatomic) int line;
@end
@implementation SPRawField
@end

static NSDictionary *gRawFields; // SPDecl -> raw fields, during resolution

@implementation SPParser

- (SPToken *)peek { return _toks[_pos]; }
- (SPToken *)peekAt:(NSUInteger)k { return _pos + k < _toks.count ? _toks[_pos + k] : _toks.lastObject; }
- (SPToken *)next { return _toks[_pos++]; }
- (BOOL)isPunct:(NSString *)s { SPToken *t = self.peek; return t.t == TPunct && [t.s isEqualToString:s]; }
- (BOOL)isIdent:(NSString *)s { SPToken *t = self.peek; return t.t == TIdent && [t.s isEqualToString:s]; }
- (void)skipNL { while (self.peek.t == TNL) _pos++; }
- (void)fail:(NSString *)msg {
    SPSchemaFail(@"%@:%d: %@ (at '%@')", _file, self.peek.line, msg, self.peek.s);
}
- (void)expect:(NSString *)p {
    if (![self isPunct:p]) [self fail:[NSString stringWithFormat:@"expected '%@'", p]];
    _pos++;
}
- (NSString *)ident {
    if (self.peek.t != TIdent) [self fail:@"expected an identifier"];
    return self.next.s;
}
- (void)endLine {
    if (self.peek.t == TNL) { [self skipNL]; return; }
    if ([self isPunct:@"}"] || self.peek.t == TEOF) return;
    [self fail:@"expected end of line"];
}

// --- expressions
- (SPExpr *)expr { return [self addExpr]; }
- (SPExpr *)addExpr {
    SPExpr *l = [self mulExpr];
    while ([self isPunct:@"+"] || [self isPunct:@"-"]) {
        NSString *op = self.next.s;
        SPExpr *e = [SPExpr new]; e.op = op; e.a = l; e.b = [self mulExpr]; l = e;
    }
    return l;
}
- (SPExpr *)mulExpr {
    SPExpr *l = [self unary];
    while ([self isPunct:@"*"] || [self isPunct:@"/"] || [self isPunct:@"%"]) {
        NSString *op = self.next.s;
        SPExpr *e = [SPExpr new]; e.op = op; e.a = l; e.b = [self unary]; l = e;
    }
    return l;
}
- (SPExpr *)unary {
    if ([self isPunct:@"-"]) { _pos++; SPExpr *e = [SPExpr new]; e.op = @"neg"; e.a = [self unary]; return e; }
    if ([self isPunct:@"("]) { _pos++; SPExpr *e = [self expr]; [self expect:@")"]; return e; }
    SPToken *t = self.peek;
    if (t.t == TInt) { _pos++; SPExpr *e = [SPExpr new]; e.op = @"int"; e.text = t.s; return e; }
    if (t.t == TFloat) { _pos++; SPExpr *e = [SPExpr new]; e.op = @"float"; e.text = t.s; return e; }
    if (t.t == TIdent) {
        _pos++;
        if ([self isPunct:@"."] && [self peekAt:1].t == TIdent) {
            NSString *m = [self peekAt:1].s;
            if ([m isEqualToString:@"Max"] || [m isEqualToString:@"Count"]) {
                _pos += 2;
                SPExpr *e = [SPExpr new]; e.op = [m isEqualToString:@"Max"] ? @"max" : @"count"; e.text = t.s;
                return e;
            }
        }
        SPExpr *e = [SPExpr new]; e.op = @"name"; e.text = t.s; return e;
    }
    [self fail:@"expected an expression"];
    return nil;
}

// --- attribute section: runs to the end of the line
- (NSArray *)attrSection {
    NSMutableArray *out = [NSMutableArray array];
    if (![self isPunct:@"|"]) return out;
    _pos++;
    if (self.peek.t == TNL || self.peek.t == TEOF) [self fail:@"empty qualification section"];
    for (;;) {
        NSString *k = [self ident];
        id v = [NSNull null];
        if ([self isPunct:@"="]) {
            _pos++;
            if (self.peek.t == TString) v = self.next.s;
            else if (self.peek.t == TIdent && [self peekAt:1].t == TPunct && [[self peekAt:1].s isEqualToString:@"::"]) {
                // a qualified C++ name (cpp_native); inert in this port
                NSMutableString *w = [self.next.s mutableCopy];
                while ([self isPunct:@"::"]) { _pos++; [w appendFormat:@"::%@", [self ident]]; }
                SPExpr *e = [SPExpr new]; e.op = @"name"; e.text = w; v = e;
            } else v = [self expr];
        }
        [out addObject:@[ k, v ]];
        if ([self isPunct:@","]) { _pos++; continue; }
        break;
    }
    if (!(self.peek.t == TNL || self.peek.t == TEOF || [self isPunct:@"}"]))
        [self fail:@"a qualification section runs to the end of the line"];
    return out;
}

// --- types
- (SPRawType *)type {
    SPRawType *t = [SPRawType new];
    if ([self isPunct:@"?"]) { _pos++; t.optional = YES; }
    if ([self isPunct:@"["]) {
        _pos++;
        if ([self isPunct:@"]"]) { t.boundForm = 4; }
        else if ([self isPunct:@".."]) { _pos++; t.boundForm = 2; t.b1 = [self expr]; }
        else {
            t.b1 = [self expr];
            if ([self isPunct:@".."]) { _pos++; t.boundForm = 3; t.b2 = [self expr]; }
            else t.boundForm = 1;
        }
        [self expect:@"]"];
        if ([self isPunct:@"?"]) { _pos++; t.elemOptional = YES; }
    }
    [self scalarInto:t];
    return t;
}
- (void)scalarInto:(SPRawType *)t {
    if ([self isPunct:@"*"]) { _pos++; t.pointer = YES; t.scalar = [self ident]; return; }
    NSString *w = [self ident];
    t.scalar = w;
    if ([w isEqualToString:@"map"]) {
        [self expect:@"["];
        SPRawType *k = [SPRawType new];
        [self scalarInto:k];
        [self expect:@"]"];
        t.mapKey = k;
        t.mapValue = [self type];
        return;
    }
    if ([w isEqualToString:@"bits"] || [w isEqualToString:@"bytes"]) {
        [self expect:@"("]; t.p1 = [self expr]; [self expect:@")"]; return;
    }
    if ([w isEqualToString:@"string"]) {
        if ([self isPunct:@"("]) { _pos++; t.p1 = [self expr]; [self expect:@")"]; }
        return;
    }
    if ([w isEqualToString:@"fixed"] || [w isEqualToString:@"ufixed"]) {
        [self expect:@"("]; t.p1 = [self expr]; [self expect:@","]; t.p2 = [self expr]; [self expect:@")"];
        return;
    }
}

// --- declarations
- (void)parseFile:(NSData *)src name:(NSString *)file {
    _toks = scan(src, file);
    _pos = 0;
    _file = file;
    [self skipNL];
    while (self.peek.t != TEOF) {
        NSString *w = [self ident];
        if ([w isEqualToString:@"package"]) { self.unit.package = [self ident]; [self endLine]; }
        else if ([w isEqualToString:@"const"]) [self parseConst];
        else if ([w isEqualToString:@"enum"]) [self parseEnum:SPDeclEnum];
        else if ([w isEqualToString:@"flags"]) [self parseEnum:SPDeclFlags];
        else if ([w isEqualToString:@"type"]) [self parseStruct:SPDeclType fixed:NO];
        else if ([w isEqualToString:@"table"]) [self parseStruct:SPDeclTable fixed:NO];
        else if ([w isEqualToString:@"fixed"]) {
            if (![[self ident] isEqualToString:@"table"]) [self fail:@"expected `table` after `fixed`"];
            [self parseStruct:SPDeclTable fixed:YES];
        } else if ([w isEqualToString:@"union"]) [self parseUnion];
        else [self fail:[NSString stringWithFormat:@"unexpected declaration '%@'", w]];
        [self skipNL];
    }
}
- (void)parseConst {
    NSString *name = [self ident];
    if (self.peek.t == TIdent) [self ident]; // a ConstType: the kind follows the value here
    [self expect:@"="];
    SPExpr *e = [self expr];
    [self attrSection];
    [self endLine];
    self.constExprs[name] = e;
}
- (SPDecl *)newDecl:(SPDeclKind)k name:(NSString *)name {
    if (self.unit.decls[name]) [self fail:[NSString stringWithFormat:@"'%@' declared twice", name]];
    SPDecl *d = [SPDecl new];
    d.kind = k; d.name = name; d.wireName = name;
    d.fields = [NSMutableArray array];
    d.variants = [NSMutableArray array];
    self.unit.decls[name] = d;
    return d;
}
- (NSDictionary *)attrDict:(NSArray *)attrs {
    NSMutableDictionary *m = [NSMutableDictionary dictionary];
    for (NSArray *a in attrs) m[a[0]] = a[1];
    return m;
}
- (void)parseEnum:(SPDeclKind)k {
    SPDecl *d = [self newDecl:k name:[self ident]];
    d.attrs = [self attrDict:[self attrSection]];
    [self skipNL];
    [self expect:@"{"];
    [self skipNL];
    while (![self isPunct:@"}"]) {
        SPVariant *v = [SPVariant new];
        v.name = [self ident];
        v.wireName = v.name;
        NSDictionary *a = [self attrDict:[self attrSection]];
        if ([a[@"was"] isKindOfClass:[NSString class]]) v.wireName = a[@"was"];
        if (a[@"deprecated"]) v.deprecated = YES;
        [d.variants addObject:v];
        [self skipNL];
        if ([self isPunct:@","]) { _pos++; [self skipNL]; }
    }
    _pos++;
    [self endLine];
}
- (void)parseUnion {
    SPDecl *d = [self newDecl:SPDeclUnion name:[self ident]];
    d.attrs = [self attrDict:[self attrSection]];
    [self skipNL];
    [self expect:@"{"];
    [self skipNL];
    NSMutableArray *raws = [NSMutableArray array];
    while (![self isPunct:@"}"]) {
        SPRawField *f = [SPRawField new];
        f.line = self.peek.line;
        f.name = [self ident];
        if (!(self.peek.t == TNL || [self isPunct:@"|"] || [self isPunct:@"}"])) f.type = [self type];
        f.attrs = [self attrSection];
        f.guards = @[];
        [raws addObject:f];
        [self endLine];
    }
    _pos++;
    [self endLine];
    ((NSMutableDictionary *)gRawFields)[[NSValue valueWithNonretainedObject:d]] = raws;
}
- (void)parseStruct:(SPDeclKind)k fixed:(BOOL)fixed {
    SPDecl *d = [self newDecl:k name:[self ident]];
    d.fixed = fixed;
    d.attrs = [self attrDict:[self attrSection]];
    [self skipNL];
    NSMutableArray *raws = [NSMutableArray array];
    [self block:raws guards:@[]];
    [self endLine];
    ((NSMutableDictionary *)gRawFields)[[NSValue valueWithNonretainedObject:d]] = raws;
}
- (void)block:(NSMutableArray *)raws guards:(NSArray *)guards {
    [self expect:@"{"];
    [self skipNL];
    while (![self isPunct:@"}"]) {
        if ([self isIdent:@"if"]) {
            _pos++;
            BOOL neg = NO;
            if ([self isPunct:@"!"]) { _pos++; neg = YES; }
            NSString *g = [self ident];
            [self skipNL];
            [self block:raws guards:[guards arrayByAddingObject:@[ g, @(!neg) ]]];
            [self skipNL];
            if ([self isIdent:@"else"]) {
                _pos++;
                [self skipNL];
                [self block:raws guards:[guards arrayByAddingObject:@[ g, @(neg) ]]];
            }
            [self skipNL];
            continue;
        }
        if ([self isIdent:@"const"] || [self isIdent:@"reserved"] || [self isIdent:@"align"])
            [self fail:@"const/reserved/align are refused in a table body (SPEC-TABLES §11)"];
        SPRawField *f = [SPRawField new];
        f.line = self.peek.line;
        f.name = [self ident];
        f.type = [self type];
        if ([self isPunct:@"="]) {
            _pos++;
            if (self.peek.t == TString) f.defaultLit = [self.next.s dataUsingEncoding:NSUTF8StringEncoding];
            else if ([self isPunct:@"{"]) {
                _pos++;
                NSMutableArray *names = [NSMutableArray array];
                while (![self isPunct:@"}"]) {
                    [names addObject:[self ident]];
                    if ([self isPunct:@","]) _pos++;
                }
                _pos++;
                f.defaultLit = names;
            } else f.defaultLit = [self expr];
        }
        f.attrs = [self attrSection];
        f.guards = guards;
        [raws addObject:f];
        [self endLine];
    }
    _pos++;
}
@end

// ---------------------------------------------------------------------------
// Resolution

@interface SPResolver : NSObject
@property (nonatomic, strong) SPUnit *unit;
@property (nonatomic, strong) NSDictionary<NSString *, SPExpr *> *constExprs;
@property (nonatomic, strong) NSMutableSet *evaluating;
@end

@implementation SPResolver

- (SPNum *)eval:(SPExpr *)e {
    NSString *op = e.op;
    if ([op isEqualToString:@"int"]) {
        SPNum *n = [SPNum new];
        NSString *s = [e.text stringByReplacingOccurrencesOfString:@"_" withString:@""];
        sp_u128 v = 0;
        int base = 10;
        NSUInteger i = 0;
        if ([s hasPrefix:@"0x"] || [s hasPrefix:@"0X"]) { base = 16; i = 2; }
        else if ([s hasPrefix:@"0b"] || [s hasPrefix:@"0B"]) { base = 2; i = 2; }
        for (; i < s.length; i++) {
            unichar c = [s characterAtIndex:i];
            int d = isdigit(c) ? c - '0' : (tolower(c) - 'a' + 10);
            v = v * base + d;
        }
        n.i = (sp_i128)v;
        return n;
    }
    if ([op isEqualToString:@"float"]) {
        SPNum *n = [SPNum new]; n.isFloat = YES; n.d = strtod(e.text.UTF8String, NULL); return n;
    }
    if ([op isEqualToString:@"name"]) {
        id c = self.unit.consts[e.text];
        if (c) return c;
        SPExpr *ce = self.constExprs[e.text];
        if (!ce) SPSchemaFail(@"unknown constant '%@'", e.text);
        if ([self.evaluating containsObject:e.text]) SPSchemaFail(@"constant cycle through '%@'", e.text);
        [self.evaluating addObject:e.text];
        SPNum *v = [self eval:ce];
        [self.evaluating removeObject:e.text];
        self.unit.consts[e.text] = v;
        return v;
    }
    if ([op isEqualToString:@"max"] || [op isEqualToString:@"count"]) {
        SPDecl *d = self.unit.decls[e.text];
        if (!d) SPSchemaFail(@"unknown declaration '%@'", e.text);
        SPNum *n = [SPNum new];
        n.i = (sp_i128)d.variants.count;
        if (d.kind == SPDeclUnion) n.i = (sp_i128)d.fields.count;
        return n;
    }
    if ([op isEqualToString:@"neg"]) {
        SPNum *a = [self eval:e.a];
        SPNum *n = [SPNum new]; n.isFloat = a.isFloat; n.i = -a.i; n.d = -a.d; return n;
    }
    SPNum *a = [self eval:e.a], *b = [self eval:e.b];
    SPNum *n = [SPNum new];
    if (a.isFloat || b.isFloat) {
        n.isFloat = YES;
        double x = [a asDouble], y = [b asDouble];
        if ([op isEqualToString:@"+"]) n.d = x + y;
        else if ([op isEqualToString:@"-"]) n.d = x - y;
        else if ([op isEqualToString:@"*"]) n.d = x * y;
        else if ([op isEqualToString:@"/"]) n.d = x / y;
        else SPSchemaFail(@"%% on a float");
        return n;
    }
    if ([op isEqualToString:@"+"]) n.i = a.i + b.i;
    else if ([op isEqualToString:@"-"]) n.i = a.i - b.i;
    else if ([op isEqualToString:@"*"]) n.i = a.i * b.i;
    else {
        if (b.i == 0) SPSchemaFail(@"division by zero");
        n.i = [op isEqualToString:@"/"] ? a.i / b.i : a.i % b.i;
    }
    return n;
}
- (sp_i128)evalInt:(SPExpr *)e {
    SPNum *n = [self eval:e];
    if (n.isFloat) SPSchemaFail(@"an integer expression was expected");
    return n.i;
}

static sp_i128 intMin(int bits, BOOL sgn) { return sgn ? -(((sp_i128)1) << (bits - 1)) : 0; }
static sp_i128 intMax(int bits, BOOL sgn) { return sgn ? (((sp_i128)1) << (bits - 1)) - 1 : (((sp_i128)1) << bits) - 1; }

- (SPType *)resolveRaw:(SPRawType *)r attrs:(NSDictionary *)attrs inTable:(BOOL)inTable {
    SPType *t = [SPType new];
    if (r.boundForm) {
        SPRawType *inner = [SPRawType new];
        inner.scalar = r.scalar; inner.p1 = r.p1; inner.p2 = r.p2; inner.pointer = r.pointer;
        inner.mapKey = r.mapKey; inner.mapValue = r.mapValue;
        SPType *el = [self resolveRaw:inner attrs:attrs inTable:inTable];
        t.elem = el;
        t.elemOptional = r.elemOptional;
        if (r.boundForm == 1 && [r.b1.op isEqualToString:@"name"] && self.unit.decls[r.b1.text]
            && self.unit.decls[r.b1.text].kind == SPDeclEnum) {
            t.kind = SPKKeyed;
            t.keyEnum = self.unit.decls[r.b1.text];
            return t;
        }
        t.kind = SPKArray;
        if (r.boundForm == 1) { t.shape = SPArrayFixed; t.minCount = t.maxCount = (long long)[self evalInt:r.b1]; }
        else if (r.boundForm == 2) { t.shape = SPArrayCounted; t.minCount = 0; t.maxCount = (long long)[self evalInt:r.b1]; }
        else if (r.boundForm == 3) { t.shape = SPArrayCounted; t.minCount = (long long)[self evalInt:r.b1]; t.maxCount = (long long)[self evalInt:r.b2]; }
        else { t.shape = SPArrayUnbounded; t.minCount = 0; t.maxCount = -1; }
        if (attrs[@"total"] && attrs[@"total"] != [NSNull null]) t.total = (long long)[self evalInt:attrs[@"total"]];
        return t;
    }
    NSString *s = r.scalar;
    if (r.mapKey) {
        t.kind = SPKMap;
        t.key = [self resolveRaw:r.mapKey attrs:@{} inTable:inTable];
        t.elemOptional = r.mapValue.optional;
        SPRawType *v = r.mapValue;
        BOOL opt = v.optional;
        v.optional = NO;
        t.elem = [self resolveRaw:v attrs:attrs inTable:inTable];
        v.optional = opt;
        return t;
    }
    if (r.pointer) {
        if ([s isEqualToString:@"bytes"]) { t.kind = SPKBlobBytes; return t; }
        if ([s isEqualToString:@"string"]) { t.kind = SPKBlobString; return t; }
        SPDecl *d = self.unit.decls[s];
        if (!d || d.kind != SPDeclTable) SPSchemaFail(@"a pointer targets a table (SPEC-TABLES §2.1): *%@", s);
        t.kind = SPKPointer; t.decl = d; return t;
    }
    static NSDictionary *ints;
    static dispatch_once_t once;
    dispatch_once(&once, ^{
        ints = @{ @"int8": @[ @8, @YES ], @"int16": @[ @16, @YES ], @"int32": @[ @32, @YES ], @"int64": @[ @64, @YES ],
                  @"uint8": @[ @8, @NO ], @"uint16": @[ @16, @NO ], @"uint32": @[ @32, @NO ], @"uint64": @[ @64, @NO ] };
    });
    id mn = attrs[@"min"], mx = attrs[@"max"], res = attrs[@"resolution"];
    if (mn == [NSNull null]) mn = nil;
    if (mx == [NSNull null]) mx = nil;
    if (res == [NSNull null]) res = nil;
    if (ints[s]) {
        t.kind = SPKInt;
        t.bits = [ints[s][0] intValue];
        t.isSigned = [ints[s][1] boolValue];
        t.imin = intMin(t.bits, t.isSigned);
        t.imax = intMax(t.bits, t.isSigned);
        if (mn || mx) {
            t.hasRange = YES;
            if (mn) t.imin = [self evalInt:mn];
            if (mx) t.imax = [self evalInt:mx];
        }
        return t;
    }
    if ([s isEqualToString:@"int128"] || [s isEqualToString:@"uint128"]) {
        t.kind = SPKInt128; t.bits = 128; t.isSigned = [s isEqualToString:@"int128"];
        if (mn || mx) {
            t.hasRange = YES;
            t.imin = mn ? [self evalInt:mn] : (t.isSigned ? (sp_i128)(((sp_u128)1) << 127) : 0);
            t.imax = mx ? [self evalInt:mx] : (sp_i128)((((sp_u128)1) << 127) - 1);
        }
        return t;
    }
    if ([s isEqualToString:@"bits"]) {
        t.kind = SPKBits; t.bits = (int)[self evalInt:r.p1];
        t.imin = 0; t.imax = (sp_i128)((((sp_u128)1) << t.bits) - 1);
        return t;
    }
    if ([s isEqualToString:@"bool"]) { t.kind = SPKBool; return t; }
    if ([s isEqualToString:@"float32"] || [s isEqualToString:@"float64"]) {
        t.kind = SPKFloat; t.bits = [s isEqualToString:@"float32"] ? 32 : 64;
        if (mn || mx || res) {
            t.hasRange = YES;
            t.fmin = mn ? [[self eval:mn] asDouble] : -INFINITY;
            t.fmax = mx ? [[self eval:mx] asDouble] : INFINITY;
            t.fres = res ? [[self eval:res] asDouble] : 0;
        }
        return t;
    }
    if ([s isEqualToString:@"string"]) {
        t.kind = SPKString; t.cap = r.p1 ? (long long)[self evalInt:r.p1] : -1; return t;
    }
    if ([s isEqualToString:@"bytes"]) { t.kind = SPKBytes; t.cap = (long long)[self evalInt:r.p1]; return t; }
    if ([s isEqualToString:@"timestamp"]) { t.kind = SPKTimestamp; return t; }
    if ([s isEqualToString:@"fixed"] || [s isEqualToString:@"ufixed"]) {
        t.kind = SPKFixed; t.isSigned = [s isEqualToString:@"fixed"];
        t.fixedI = (int)[self evalInt:r.p1]; t.fixedF = (int)[self evalInt:r.p2];
        t.bits = t.fixedI + t.fixedF;
        t.hasRange = YES;
        if (!mn || !mx) SPSchemaFail(@"fixed/ufixed requires min and max (SPEC §4.3)");
        t.imin = [self evalInt:mn] << t.fixedF;
        t.imax = [self evalInt:mx] << t.fixedF;
        return t;
    }
    SPDecl *d = self.unit.decls[s];
    if (!d) SPSchemaFail(@"unknown type '%@'", s);
    t.decl = d;
    switch (d.kind) {
    case SPDeclEnum: t.kind = SPKEnum; break;
    case SPDeclFlags: t.kind = SPKFlags; break;
    case SPDeclUnion: t.kind = SPKUnion; break;
    default: t.kind = SPKStruct; break;
    }
    return t;
}

- (SPField *)resolveField:(SPRawField *)rf owner:(SPDecl *)d prior:(NSArray<SPField *> *)prior {
    SPField *f = [SPField new];
    f.name = rf.name;
    f.wireName = rf.name;
    f.jsonKey = rf.name;
    NSMutableDictionary *attrs = [NSMutableDictionary dictionary];
    for (NSArray *a in rf.attrs) attrs[a[0]] = a[1];
    f.attrs = attrs;
    if ([attrs[@"was"] isKindOfClass:[NSString class]]) f.wireName = attrs[@"was"];
    if ([attrs[@"json"] isKindOfClass:[NSString class]]) f.jsonKey = attrs[@"json"];
    if (attrs[@"required"]) f.required = YES;
    if (attrs[@"deprecated"]) f.deprecated = YES;
    f.wireId = SPFnv1a64Str(f.wireName);
    if (rf.type) {
        f.optional = rf.type.optional;
        f.type = [self resolveRaw:rf.type attrs:attrs inTable:YES];
    }
    if (rf.defaultLit) {
        id lit = rf.defaultLit;
        if ([lit isKindOfClass:[SPExpr class]]) {
            SPExpr *e = lit;
            if ([e.op isEqualToString:@"name"] && !self.constExprs[e.text] && !self.unit.consts[e.text])
                lit = e.text; // true / false / an enum variant
            else lit = [self eval:e];
        }
        f.defaultLiteral = lit;
    }
    NSMutableArray *gs = [NSMutableArray array];
    for (NSArray *g in rf.guards) {
        SPGuard *sg = [SPGuard new];
        sg.wantTrue = [g[1] boolValue];
        sg.fieldIndex = -1;
        for (NSUInteger i = 0; i < prior.count; i++)
            if ([prior[i].name isEqualToString:g[0]]) sg.fieldIndex = (NSInteger)i;
        if (sg.fieldIndex < 0) SPSchemaFail(@"%@: guard '%@' names no earlier field (SPEC §4.5)", d.name, g[0]);
        [gs addObject:sg];
    }
    f.guards = gs;
    return f;
}

- (void)resolve {
    for (NSString *name in self.constExprs) {
        SPExpr *e = [SPExpr new];
        e.op = @"name"; e.text = name;
        (void)[self eval:e];
    }
    for (SPDecl *d in self.unit.decls.allValues) {
        if ([d.attrs[@"was"] isKindOfClass:[NSString class]]) d.wireName = d.attrs[@"was"];
        d.wireId = SPFnv1a64Str(d.wireName);
        for (SPVariant *v in d.variants) v.wireId = SPFnv1a64Str(v.wireName);
        if (d.kind == SPDeclFlags) {
            d.flagsWidth = (int)d.variants.count;
            id m = d.attrs[@"max"];
            if (m && m != [NSNull null]) d.flagsWidth = (int)[self evalInt:m];
        }
        if ([d.attrs[@"integrity"] isKindOfClass:[NSString class]]) d.integrity = YES;
        if ([d.attrs[@"json"] isKindOfClass:[NSString class]] && [d.attrs[@"json"] isEqualToString:@"array"]) d.jsonArray = YES;
    }
    for (SPDecl *d in self.unit.decls.allValues) {
        NSArray *raws = gRawFields[[NSValue valueWithNonretainedObject:d]];
        for (SPRawField *rf in raws) {
            SPField *f = [self resolveField:rf owner:d prior:d.fields];
            [d.fields addObject:f];
        }
        if (d.kind == SPDeclTable) self.unit.tablesByWireId[@(d.wireId)] = d;
    }
    for (SPDecl *d in self.unit.decls.allValues)
        for (SPField *f in d.fields) SPResolveDefault(f);
}
@end

@implementation SPUnit
+ (SPUnit *)loadPaths:(NSArray<NSString *> *)paths {
    SPUnit *u = [SPUnit new];
    u.decls = [NSMutableDictionary dictionary];
    u.consts = [NSMutableDictionary dictionary];
    u.tablesByWireId = [NSMutableDictionary dictionary];
    gRawFields = [NSMutableDictionary dictionary];
    SPParser *p = [SPParser new];
    p.unit = u;
    p.constExprs = [NSMutableDictionary dictionary];
    NSFileManager *fm = [NSFileManager defaultManager];
    NSMutableArray *files = [NSMutableArray array];
    for (NSString *path in paths) {
        BOOL dir = NO;
        if (![fm fileExistsAtPath:path isDirectory:&dir]) SPSchemaFail(@"no such unit path %@", path);
        if (dir) {
            NSArray *names = [[fm contentsOfDirectoryAtPath:path error:nil] sortedArrayUsingSelector:@selector(compare:)];
            for (NSString *n in names)
                if ([n hasSuffix:@".schema"]) [files addObject:[path stringByAppendingPathComponent:n]];
        } else [files addObject:path];
    }
    for (NSString *f in files) [p parseFile:[NSData dataWithContentsOfFile:f] name:f];
    SPResolver *r = [SPResolver new];
    r.unit = u;
    r.constExprs = p.constExprs;
    r.evaluating = [NSMutableSet set];
    [r resolve];
    gRawFields = nil;
    return u;
}
- (SPDecl *)declNamed:(NSString *)name {
    SPDecl *d = self.decls[name];
    if (!d) SPSchemaFail(@"no declaration named %@", name);
    return d;
}
@end
