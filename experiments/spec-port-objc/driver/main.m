// The conformance driver (test/conformance/README.md, the driver contract):
//
//   driver <manifest> list
//   driver <manifest> <surface> <outdir>
//
// The working directory is the repository root. One file per case; a case
// this port cannot answer is `<case>.absent`, never a guess.

#import "SPJson.h"
#import "SPFixed.h"

static NSArray<NSString *> *kSurfaces;

static NSString *gOut;
static NSMutableDictionary<NSString *, NSArray<NSString *> *> *gUnitPaths;
static NSMutableDictionary<NSString *, id> *gUnits; // SPUnit or NSString error

static void put(NSString *name, NSData *d) {
    [d writeToFile:[gOut stringByAppendingPathComponent:name] atomically:NO];
}
static void putText(NSString *name, NSString *s) { put(name, [s dataUsingEncoding:NSUTF8StringEncoding]); }
static void absent(NSString *name, NSString *why) {
    putText([name stringByAppendingString:@".absent"], @"");
    fprintf(stderr, "absent %s: %s\n", name.UTF8String, why.UTF8String);
}

static SPUnit *unitFor(NSString *key, NSString **err) {
    id u = gUnits[key];
    if (!u) {
        NSArray *paths = gUnitPaths[key];
        if (!paths) u = [NSString stringWithFormat:@"no unit %@", key];
        else {
            @try { u = [SPUnit loadPaths:paths]; }
            @catch (NSException *e) { u = [NSString stringWithFormat:@"%@: %@", e.name, e.reason]; }
        }
        gUnits[key] = u;
    }
    if ([u isKindOfClass:[NSString class]]) { *err = u; return nil; }
    return u;
}

// the subject of a case: a unit, its root decl; nil + reason when this port
// does not carry it (an absence)
static NSString *gUnitKey;
static SPDecl *rootFor(NSString *unitKey, NSString *root, SPUnit **unitOut, NSString **why) {
    gUnitKey = unitKey;
    SPUnit *u = unitFor(unitKey, why);
    if (!u) return nil;
    SPDecl *d = [u declNamed:root];
    if (!d) { *why = [NSString stringWithFormat:@"no root %@", root]; return nil; }
    *unitOut = u;
    return d;
}

static NSData *readFile(NSString *path) {
    NSData *d = [NSData dataWithContentsOfFile:path];
    if (!d) [NSException raise:@"DriverIO" format:@"cannot read %@", path];
    return d;
}

// The lineage a fixed table's lock records (FIXED-FORM-ALGORITHM §5.2): the
// `lineage wire=0x...` lines under `fixed table <Root>` in the unit's
// schema.lock. Only the HASHES are read here, to tell an older writer's file
// (an absence: the plan compiler is not carried) from a stranger's (refused).
static NSMutableDictionary<NSString *, NSArray *> *gLocks;
static NSArray<NSString *> *lockLines(NSString *unitKey) {
    if (!gLocks) gLocks = [NSMutableDictionary dictionary];
    NSArray *have = gLocks[unitKey];
    if (have) return have;
    NSMutableArray *lines = [NSMutableArray array];
    for (NSString *path in gUnitPaths[unitKey]) {
        BOOL dir = NO;
        [[NSFileManager defaultManager] fileExistsAtPath:path isDirectory:&dir];
        NSString *lock = [(dir ? path : [path stringByDeletingLastPathComponent]) stringByAppendingPathComponent:@"schema.lock"];
        NSString *text = [NSString stringWithContentsOfFile:lock encoding:NSUTF8StringEncoding error:nil];
        if (text) [lines addObjectsFromArray:[text componentsSeparatedByString:@"\n"]];
    }
    gLocks[unitKey] = lines;
    return lines;
}
static NSArray<NSNumber *> *lineageOf(NSString *unitKey, NSString *root) {
    NSMutableArray *out = [NSMutableArray array];
    BOOL in = NO;
    for (NSString *l in lockLines(unitKey)) {
        if (![l hasPrefix:@" "] && l.length) {
            NSArray *w = [l componentsSeparatedByString:@" "];
            in = w.count > 2 && [w[0] isEqualToString:@"fixed"] && [w[2] isEqualToString:root];
            continue;
        }
        if (!in) continue;
        NSRange r = [l rangeOfString:@"lineage wire=0x"];
        if (r.location == NSNotFound) continue;
        unsigned long long h = strtoull([l substringFromIndex:r.location + r.length].UTF8String, NULL, 16);
        [out addObject:@(h)];
    }
    return out;
}

static BOOL gFramingReadsForm1;
static SPTableVal *loadAny(NSData *wire, SPDecl *d, SPUnit *u) {
    // SPEC-GAP 8: SPEC-TABLES "The three wires" refuses form 1 at a declared
    // fixed root (`previous_form`), and the report rows previous_form_* owe
    // exactly that; but fifteen FRAMING rows at the same fixed root (tblr1
    // Cfg) hand a form-1 file and owe `read` and a form-3 write-back
    // (frame_empty_table beside previous_form_r1_cfg). The corpus contradicts
    // itself across surfaces; following each row: the framing surface reads
    // form 1 at a fixed root, every other read refuses it.
    if (gFramingReadsForm1 && d.fixed && wire.length && ((const uint8_t *)wire.bytes)[0] == 1) return SPLoadForm1(wire, d, u);
    if (d.fixed) return SPLoadFixed(wire, d, [NSSet setWithArray:lineageOf(gUnitKey, d.name)]);
    return SPLoadVariable(wire, d, u);
}
static NSData *saveAny(SPTableVal *v) { return v.decl.fixed ? SPSaveFixed(v) : SPSaveVariable(v); }

static BOOL isRefusal(NSException *e) { return [e.name isEqualToString:SPRefusal]; }
// an exception that is not a refusal is a defect in this port: the case gets
// no answer (a failure the matrix shows), and the surface goes on
static void bug(NSException *e) { fprintf(stderr, "BUG %s: %s\n", e.name.UTF8String, e.reason.UTF8String); }
static BOOL isAbsent(NSException *e) { return [e.name isEqualToString:SPAbsent]; }

// ---------------------------------------------------------------------------
// The write-refusal patcher: set ONE field at a dotted path (FORMAT.md)

static NSData *hexBytes(NSString *h) {
    NSMutableData *d = [NSMutableData data];
    for (NSUInteger i = 0; i + 1 < h.length; i += 2) {
        unsigned v;
        [[NSScanner scannerWithString:[h substringWithRange:NSMakeRange(i, 2)]] scanHexInt:&v];
        uint8_t b = (uint8_t)v;
        [d appendBytes:&b length:1];
    }
    return d;
}

// UTF-16 units to bytes, a lone surrogate kept as its three-byte pattern
// (the nearest value that is not text in this leg: FORMAT.md lets a leg
// whose storage cannot hold the units hold that)
static NSData *unitsBytes(NSString *h) {
    NSMutableArray<NSNumber *> *u = [NSMutableArray array];
    for (NSUInteger i = 0; i + 3 < h.length; i += 4) {
        unsigned v;
        [[NSScanner scannerWithString:[h substringWithRange:NSMakeRange(i, 4)]] scanHexInt:&v];
        [u addObject:@(v)];
    }
    NSMutableData *d = [NSMutableData data];
    for (NSUInteger i = 0; i < u.count; i++) {
        uint32_t c = u[i].unsignedIntValue;
        if (c >= 0xD800 && c <= 0xDBFF && i + 1 < u.count && u[i + 1].unsignedIntValue >= 0xDC00 && u[i + 1].unsignedIntValue <= 0xDFFF) {
            c = 0x10000 + ((c - 0xD800) << 10) + (u[i + 1].unsignedIntValue - 0xDC00);
            i++;
        }
        uint8_t b[4]; int n;
        if (c < 0x80) { b[0] = c; n = 1; }
        else if (c < 0x800) { b[0] = 0xC0 | c >> 6; b[1] = 0x80 | (c & 63); n = 2; }
        else if (c < 0x10000) { b[0] = 0xE0 | c >> 12; b[1] = 0x80 | (c >> 6 & 63); b[2] = 0x80 | (c & 63); n = 3; }
        else { b[0] = 0xF0 | c >> 18; b[1] = 0x80 | (c >> 12 & 63); b[2] = 0x80 | (c >> 6 & 63); b[3] = 0x80 | (c & 63); n = 4; }
        [d appendBytes:b length:n];
    }
    return d;
}

static BOOL parseBigInt(NSString *s, sp_u128 *raw) {
    BOOL neg = [s hasPrefix:@"-"];
    NSString *dg = neg ? [s substringFromIndex:1] : s;
    if (!dg.length) return NO;
    sp_u128 m = 0;
    for (NSUInteger i = 0; i < dg.length; i++) {
        unichar c = [dg characterAtIndex:i];
        if (c < '0' || c > '9') return NO;
        sp_u128 nx = m * 10 + (c - '0');
        if (nx / 10 != m) return NO;
        m = nx;
    }
    if (neg && m > ((sp_u128)1 << 127)) return NO;
    *raw = neg ? (sp_u128)0 - m : m;
    return YES;
}

typedef NS_ENUM(int, PatchResult) { PatchOK, PatchUnrepresentable };

// a slot: a container (NSMutableArray or SPTableVal), an index, and its type
static PatchResult applyPatch(SPTableVal *root, NSString *path, NSString *kind, NSString *value) {
    // walk to the parent of the last step
    NSMutableArray *steps = [NSMutableArray array];
    for (NSString *part in [path componentsSeparatedByString:@"."]) {
        NSRange br = [part rangeOfString:@"["];
        NSString *name = br.location == NSNotFound ? part : [part substringToIndex:br.location];
        [steps addObject:name];
        NSString *rest = br.location == NSNotFound ? @"" : [part substringFromIndex:br.location];
        while (rest.length) {
            NSRange close = [rest rangeOfString:@"]"];
            [steps addObject:@([[rest substringWithRange:NSMakeRange(1, close.location - 1)] integerValue])];
            rest = [rest substringFromIndex:close.location + 1];
        }
    }
    id container = root;   // SPTableVal, NSMutableArray, SPMapEntry or SPUnionVal
    NSUInteger slot = 0;
    id cur = root;
    SPType *curType = nil;
    for (NSUInteger s = 0; s < steps.count; s++) {
        id step = steps[s];
        if ([step isKindOfClass:[NSString class]]) {
            if ([cur isKindOfClass:[SPMapEntry class]]) {
                SPMapEntry *e = cur;
                container = e;
                BOOL key = [step isEqualToString:@"key"];
                slot = key ? 0 : 1;
                cur = key ? e.key : e.value;
                curType = key ? curType.key : curType.elem;
                continue;
            }
            if ([cur isKindOfClass:[SPUnionVal class]]) {
                SPUnionVal *u = cur;
                NSInteger ai = -1;
                for (NSUInteger j = 0; j < curType.decl.fields.count; j++)
                    if ([curType.decl.fields[j].name isEqualToString:step]) ai = (NSInteger)j;
                if (ai >= 0) {
                    SPField *arm = curType.decl.fields[(NSUInteger)ai];
                    if (u.arm != ai + 1) { u.arm = ai + 1; u.value = arm.type ? SPCopyValue(arm.defaultValue) : [NSNull null]; }
                    container = u;
                    cur = u.value;
                    curType = arm.type;
                    continue;
                }
                cur = u.value;
            }
            SPTableVal *t = cur;
            NSInteger fi = -1;
            for (NSUInteger j = 0; j < t.decl.fields.count; j++)
                if ([t.decl.fields[j].name isEqualToString:step]) fi = (NSInteger)j;
            if (fi < 0) [NSException raise:@"DriverPath" format:@"no field %@ in %@", step, path];
            SPField *field = t.decl.fields[(NSUInteger)fi];
            container = t; slot = (NSUInteger)fi;
            cur = t.values[(NSUInteger)fi];
            curType = field.type;
            if (field.optional) t.present[(NSUInteger)fi] = @YES;
        } else {
            NSInteger k = [step integerValue];
            if (curType.kind != SPKArray && curType.kind != SPKMap && curType.kind != SPKKeyed)
                [NSException raise:@"DriverPath" format:@"index into a non-array in %@", path];
            container = cur; slot = (NSUInteger)k;
            cur = ((NSArray *)cur)[(NSUInteger)k];
            if (curType.kind != SPKMap) curType = curType.elem;
        }
    }
    void (^set)(id) = ^(id x) {
        if ([container isKindOfClass:[SPTableVal class]]) ((SPTableVal *)container).values[slot] = x;
        else if ([container isKindOfClass:[SPMapEntry class]]) { if (slot) ((SPMapEntry *)container).value = x; else ((SPMapEntry *)container).key = x; }
        else if ([container isKindOfClass:[SPUnionVal class]]) ((SPUnionVal *)container).value = x;
        else ((NSMutableArray *)container)[slot] = x;
    };
    SPType *t = curType;
    if ([kind isEqualToString:@"int"]) {
        sp_u128 raw;
        if (!parseBigInt(value, &raw)) return PatchUnrepresentable;
        if (t.kind == SPKFloat) set([SPFloat d:value.doubleValue]);
        else set([SPInt u:raw]);
    } else if ([kind isEqualToString:@"ordinal"]) {
        sp_u128 raw;
        if (!parseBigInt(value, &raw)) return PatchUnrepresentable;
        set([SPInt u:raw]);
    } else if ([kind isEqualToString:@"float"]) {
        double d = [value isEqualToString:@"nan"] ? NAN : [value isEqualToString:@"inf"] ? INFINITY : [value isEqualToString:@"-inf"] ? -INFINITY : strtod(value.UTF8String, NULL);
        set([SPFloat d:d]);
    } else if ([kind isEqualToString:@"count"]) {
        NSInteger n = value.integerValue;
        if (n < 0) return PatchUnrepresentable;
        NSMutableArray *a = cur;
        while ((NSInteger)a.count > n) [a removeLastObject];
        while ((NSInteger)a.count < n) [a addObject:a.count ? SPCopyValue(a[0]) : SPNewValue(t.elem)];
    } else if ([kind isEqualToString:@"length"]) {
        NSInteger n = value.integerValue;
        if (n < 0) return PatchUnrepresentable;
        NSMutableData *d = [NSMutableData dataWithLength:(NSUInteger)n];
        memset(d.mutableBytes, 'a', (NSUInteger)n);
        set(d);
    } else if ([kind isEqualToString:@"bytes"]) {
        set(hexBytes(value));
    } else if ([kind isEqualToString:@"units"]) {
        set(unitsBytes(value));
    } else if ([kind isEqualToString:@"tag"]) {
        NSInteger tag = value.integerValue;
        SPUnionVal *u = [SPUnionVal new];
        u.arm = tag;
        u.value = [NSNull null];
        if (tag > 0 && tag <= (NSInteger)t.decl.fields.count) {
            SPField *arm = t.decl.fields[(NSUInteger)tag - 1];
            if (arm.type) u.value = SPCopyValue(arm.defaultValue);
        }
        set(u);
    } else {
        [NSException raise:@"DriverPath" format:@"unknown kind %@", kind];
    }
    return PatchOK;
}

// ---------------------------------------------------------------------------

static NSArray<NSArray<NSString *> *> *gLines;

static NSArray *linesOf(NSString *kind) {
    NSMutableArray *a = [NSMutableArray array];
    for (NSArray *l in gLines) if ([l[0] isEqualToString:kind]) [a addObject:l];
    return a;
}

static void readAndWriteBack(NSString *name, NSData *(^read)(SPDecl *, SPUnit *), NSString *unitKey, NSString *root) {
    NSString *why;
    SPUnit *u;
    SPDecl *d = rootFor(unitKey, root, &u, &why);
    if (!d) { absent(name, why); return; }
    SPTableVal *v;
    @try {
        v = (SPTableVal *)read(d, u);
    } @catch (NSException *e) {
        if (isAbsent(e)) { absent(name, e.reason); return; }
        if (!isRefusal(e)) { bug(e); return; }
        putText(name, @"refused\n");
        return;
    }
    @try {
        NSData *text = SPToJson(v);
        NSData *wire = saveAny(v);
        NSMutableData *o = [[@"read\n" dataUsingEncoding:NSASCIIStringEncoding] mutableCopy];
        [o appendData:text];
        put(name, o);
        put([name stringByAppendingString:@".wire"], wire);
    } @catch (NSException *e) {
        if (isAbsent(e)) { absent(name, e.reason); return; }
        if (!isRefusal(e)) { bug(e); return; }
        absent(name, [NSString stringWithFormat:@"accepted, but the write-back refused: %@", e.reason]);
    }
}

static int runSurface(NSString *surface) {
    if ([surface isEqualToString:@"wire"] || [surface isEqualToString:@"json-read"] || [surface isEqualToString:@"json-write"]) {
        BOOL wire = [surface isEqualToString:@"wire"], jread = [surface isEqualToString:@"json-read"];
        for (NSArray *l in linesOf(@"instance")) {
            NSString *name = l[1];
            BOOL noText = [l containsObject:@"no-text"], refusedLine = [l containsObject:@"wire-refused"];
            NSString *outName = [surface isEqualToString:@"json-write"] ? [name stringByAppendingString:@".json"] : name;
            if (!wire && noText) continue;
            NSString *why;
            SPUnit *u;
            SPDecl *d = rootFor(l[2], l[3], &u, &why);
            if (!d) { absent(outName, why); continue; }
            @try {
                SPTableVal *v;
                if (jread) v = SPFromJson(readFile([NSString stringWithFormat:@"testdata/conformance/tables/json/%@.json", name]), d, u);
                else v = loadAny(readFile(l[4]), d, u);
                put(outName, jread || wire ? saveAny(v) : SPToJson(v));
            } @catch (NSException *e) {
                if (isAbsent(e)) { absent(outName, e.reason); continue; }
                if (!isRefusal(e)) { bug(e); continue; }
                if (!refusedLine) fprintf(stderr, "%s: refused: %s\n", name.UTF8String, e.reason.UTF8String);
                // SPEC-GAP 3: README says a driver "may emit `refused` or the
                // historical `<case>.absent` file"; on these three surfaces the
                // harness reads a `refused` file as value bytes, so a refusal is
                // the `.absent` file, its text the reason (ignored by the harness)
                putText([outName stringByAppendingString:@".absent"], [NSString stringWithFormat:@"refused: %@\n", e.reason]);
            }
        }
        if ([surface isEqualToString:@"json-write"]) {
            for (NSArray *l in linesOf(@"json-write-refusal")) {
                NSString *name = [l[1] stringByAppendingString:@".json"];
                NSString *why;
                SPUnit *u;
                SPDecl *d = rootFor(l[2], l[3], &u, &why);
                if (!d) { absent(name, why); continue; }
                @try {
                    SPTableVal *v = loadAny(readFile(l[4]), d, u);
                    NSString *damage = l[5];
                    if ([damage hasPrefix:@"narrow:"])
                        applyPatch(v, [damage substringFromIndex:7], @"bytes", @"61ff62");
                    else if ([damage hasPrefix:@"wide:"])
                        applyPatch(v, [damage substringFromIndex:5], @"units", @"0061d8000062");
                    put(name, SPToJson(v));
                } @catch (NSException *e) {
                    if (isAbsent(e)) { absent(name, e.reason); continue; }
                    if (!isRefusal(e)) { bug(e); continue; }
                    putText(name, @"refused\n");
                }
            }
        }
        return 0;
    }
    if ([surface isEqualToString:@"report"] || [surface isEqualToString:@"framing"]) {
        gFramingReadsForm1 = [surface isEqualToString:@"framing"];
        for (NSArray *l in linesOf(surface)) {
            NSString *path = l[4];
            readAndWriteBack(l[1], ^NSData *(SPDecl *d, SPUnit *u) {
                return (NSData *)loadAny(readFile(path), d, u);
            }, l[2], l[3]);
        }
        return 0;
    }
    if ([surface isEqualToString:@"json-hostile"]) {
        for (NSArray *l in linesOf(surface)) {
            NSString *path = [NSString stringWithFormat:@"%@/%@.json", l[4], l[3]];
            readAndWriteBack(l[1], ^NSData *(SPDecl *d, SPUnit *u) {
                return (NSData *)SPFromJson(readFile(path), d, u);
            }, l[2], l[3]);
        }
        return 0;
    }
    if ([surface isEqualToString:@"write-refusal"] || [surface isEqualToString:@"write-refusal-fixed"] || [surface isEqualToString:@"write-refusal-json"]) {
        BOOL json = [surface isEqualToString:@"write-refusal-json"];
        for (NSArray *l in linesOf(surface)) {
            NSString *name = l[1];
            NSString *why;
            SPUnit *u;
            SPDecl *d = rootFor(l[2], l[3], &u, &why);
            if (!d) { absent(name, why); continue; }
            SPTableVal *v;
            @try {
                v = loadAny(readFile(l[4]), d, u);
            } @catch (NSException *e) {
                if (isAbsent(e)) { absent(name, e.reason); continue; }
                if (!isRefusal(e)) { bug(e); continue; }
                absent(name, [NSString stringWithFormat:@"the row's wire did not load: %@", e.reason]);
                continue;
            }
            if (applyPatch(v, l[5], l[6], l[7]) == PatchUnrepresentable) { putText(name, @"unrepresentable\n"); continue; }
            @try {
                if (json) SPToJson(v);
                else saveAny(v);
                putText(name, @"wrote\n");
            } @catch (NSException *e) {
                if (isAbsent(e)) { absent(name, e.reason); continue; }
                if (!isRefusal(e)) { bug(e); continue; }
                putText(name, @"refused\n");
            }
        }
        return 0;
    }
    return 2;
}

int main(int argc, const char **argv) {
    @autoreleasepool {
        kSurfaces = @[ @"wire", @"json-read", @"json-write", @"report", @"framing", @"json-hostile", @"write-refusal", @"write-refusal-fixed", @"write-refusal-json" ];
        if (argc < 3) { fprintf(stderr, "usage: driver <manifest> list | <surface> <outdir>\n"); return 64; }
        NSString *verb = @(argv[2]);
        if ([verb isEqualToString:@"list"]) {
            for (NSString *s in kSurfaces) printf("%s\n", s.UTF8String);
            return 0;
        }
        if (![kSurfaces containsObject:verb] && ![verb isEqualToString:@"units"]) return 2;
        BOOL diag = [verb isEqualToString:@"units"];
        if (!diag && argc < 4) return 64;
        if (!diag) {
            gOut = @(argv[3]);
            [[NSFileManager defaultManager] createDirectoryAtPath:gOut withIntermediateDirectories:YES attributes:nil error:nil];
        }
        NSString *text = [NSString stringWithContentsOfFile:@(argv[1]) encoding:NSUTF8StringEncoding error:nil];
        if (!text) { fprintf(stderr, "cannot read manifest\n"); return 1; }
        NSMutableArray *lines = [NSMutableArray array];
        gUnitPaths = [NSMutableDictionary dictionary];
        gUnits = [NSMutableDictionary dictionary];
        NSCharacterSet *ws = [NSCharacterSet whitespaceCharacterSet];
        for (NSString *raw in [text componentsSeparatedByString:@"\n"]) {
            NSString *line = [raw stringByTrimmingCharactersInSet:ws];
            if (!line.length || [line hasPrefix:@"#"]) continue;
            NSMutableArray *f = [NSMutableArray array];
            for (NSString *x in [line componentsSeparatedByCharactersInSet:ws]) if (x.length) [f addObject:x];
            [lines addObject:f];
            if ([f[0] isEqualToString:@"unit"]) gUnitPaths[f[1]] = [f subarrayWithRange:NSMakeRange(2, f.count - 2)];
        }
        gLines = lines;
        if ([verb isEqualToString:@"units"]) {
            // a diagnostic, outside the contract: load every unit, say how it went
            for (NSString *k in [gUnitPaths.allKeys sortedArrayUsingSelector:@selector(compare:)]) {
                @try {
                    SPUnit *u = [SPUnit loadPaths:gUnitPaths[k]];
                    printf("ok   %s\n", k.UTF8String);
                    for (NSString *dn in [u.decls.allKeys sortedArrayUsingSelector:@selector(compare:)]) {
                        SPDecl *d = u.decls[dn];
                        if (!d.fixed) continue;
                        NSArray *lin = lineageOf(k, dn);
                        uint64_t h = 0;
                        NSString *err = nil;
                        @try { h = SPFixedHash(d); } @catch (NSException *e) { err = e.reason; }
                        printf("     fixed %-24s mine=%016llx lock-last=%016llx %s%s\n", dn.UTF8String, h,
                               lin.count ? [lin.lastObject unsignedLongLongValue] : 0ULL,
                               err ? err.UTF8String : (lin.count && [lin.lastObject unsignedLongLongValue] == h ? "MATCH" : lin.count ? "DIFFER" : "no lock"),
                               lin.count && [lin containsObject:@(h)] && [lin.lastObject unsignedLongLongValue] != h ? " (in lineage)" : "");
                    }
                } @catch (NSException *e) {
                    printf("FAIL %s: %s: %s\n", k.UTF8String, e.name.UTF8String, e.reason.UTF8String);
                    if (argc > 3) for (NSString *s in e.callStackSymbols) printf("    %s\n", s.UTF8String);
                }
            }
            return 0;
        }
        @try {
            return runSurface(verb);
        } @catch (NSException *e) {
            fprintf(stderr, "driver failed: %s: %s\n", e.name.UTF8String, e.reason.UTF8String);
            return 1;
        }
    }
}
