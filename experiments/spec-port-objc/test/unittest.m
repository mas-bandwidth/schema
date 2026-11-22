// Unit tests: no real time, no sockets, no network, nothing outside the
// working directory. Schemas are written inline and loaded from a temporary
// file under ./build.

#import "SPJson.h"

static int gFail, gRun;
#define CHECK(c) do { gRun++; if (!(c)) { gFail++; fprintf(stderr, "FAIL %s:%d: %s\n", __FILE__, __LINE__, #c); } } while (0)

static BOOL refuses(void (^b)(void)) {
    @try { b(); } @catch (NSException *e) { return [e.name isEqualToString:SPRefusal]; }
    return NO;
}

static SPUnit *unitOf(NSString *src) {
    NSString *dir = @"build/unittest-schema";
    [[NSFileManager defaultManager] removeItemAtPath:dir error:nil];
    [[NSFileManager defaultManager] createDirectoryAtPath:dir withIntermediateDirectories:YES attributes:nil error:nil];
    NSString *p = [dir stringByAppendingPathComponent:@"T.schema"];
    [src writeToFile:p atomically:NO encoding:NSUTF8StringEncoding error:nil];
    return [SPUnit loadPaths:@[ p ]];
}

static NSData *bytes(const char *s) { return [NSData dataWithBytes:s length:strlen(s)]; }

int main(void) {
    @autoreleasepool {
        // CRC-32C check value (RFC 3720 B.4)
        CHECK(SPCrc32c((const uint8_t *)"123456789", 9) == 0xE3069283u);
        // fnv1a64 of the empty string is the offset basis
        CHECK(SPFnv1a64Str(@"") == 0xcbf29ce484222325ULL);
        // UTF-8: overlong, surrogate, past U+10FFFF and zero
        CHECK(SPValidUTF8(bytes("a\xc3\xa9"), NO));
        CHECK(!SPValidUTF8(bytes("\xc0\x80"), YES));
        CHECK(!SPValidUTF8(bytes("\xed\xa0\x80"), YES));
        CHECK(!SPValidUTF8(bytes("\xf4\x90\x80\x80"), YES));
        CHECK(!SPValidUTF8([NSData dataWithBytes:"a\0" length:2], NO));

        SPUnit *u = unitOf(@"package t\n\n"
                           "enum Kind\n{\n    Red\n    Blue\n}\n\n"
                           "table T\n{\n    a int32\n    b uint64\n    s string(8)\n    k Kind\n    xs [..3]uint8\n    f float32\n}\n");
        SPDecl *T = [u declNamed:@"T"];
        CHECK(T != nil);
        SPTableVal *v = SPNewTable(T);
        v.values[0] = [SPInt i:-5];
        v.values[1] = [SPInt u:UINT64_MAX];
        v.values[2] = bytes("hi");
        v.values[3] = [SPInt u:2];
        [v.values[4] addObject:[SPInt u:7]];
        v.values[5] = [SPFloat d:0.1f];
        NSData *wire = SPSaveVariable(v);
        SPTableVal *back = SPLoadVariable(wire, T, u);
        CHECK([SPSaveVariable(back) isEqualToData:wire]);
        NSData *text = SPToJson(back);
        NSString *expect = @"{\n  \"a\": -5,\n  \"b\": \"18446744073709551615\",\n  \"s\": \"hi\",\n  \"k\": \"Blue\",\n  \"xs\": [\n    7\n  ],\n  \"f\": 0.1\n}\n";
        CHECK([[[NSString alloc] initWithData:text encoding:NSUTF8StringEncoding] isEqualToString:expect]);
        CHECK([SPSaveVariable(SPFromJson(text, T, u)) isEqualToData:wire]);

        // the write contract refuses in every build
        SPTableVal *bad = SPCopyValue(v);
        bad.values[2] = bytes("123456789");
        CHECK(refuses(^{ SPSaveVariable(bad); }));
        CHECK(refuses(^{ SPToJson(bad); }));
        bad = SPCopyValue(v);
        bad.values[3] = [SPInt u:3];
        CHECK(refuses(^{ SPSaveVariable(bad); }));
        bad = SPCopyValue(v);
        bad.values[0] = [SPInt i:(sp_i128)INT32_MAX + 1];
        CHECK(refuses(^{ SPSaveVariable(bad); }));
        bad = SPCopyValue(v);
        bad.values[5] = [SPFloat d:1e39];
        CHECK(refuses(^{ SPSaveVariable(bad); }));

        // reads refuse, never return a partial value
        CHECK(refuses(^{ SPLoadVariable([NSData data], T, u); }));
        CHECK(refuses(^{ SPFromJson(bytes("{\"a\": 1, \"a\": 2}"), T, u); }));
        CHECK(refuses(^{ SPFromJson(bytes("{\"s\": \"\\u0000\"}"), T, u); }));
        CHECK(refuses(^{ SPFromJson(bytes("{\"b\": 5}"), T, u); }));
        CHECK(refuses(^{ SPFromJson(bytes("{\"a\": 1.0}"), T, u); }));
        CHECK(refuses(^{ SPFromJson(bytes("{\"xs\": [1,2,3,4]}"), T, u); }));
        CHECK(!refuses(^{ SPFromJson(bytes("\xef\xbb\xbf{ /* c */ \"a\": 1, // x\n}"), T, u); }));

        fprintf(stderr, "%d checks, %d failed\n", gRun, gFail);
    }
    return gFail ? 1 : 0;
}
