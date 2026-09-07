#include "ArmsTable.h"
#include <cassert>
#include <cstdio>
#include <fstream>
#include <string>
#include <vector>
#include <cstring>
using namespace rustarms;
static void bytes(FILE *out, const void *data, size_t size) {
    assert(size <= UINT32_MAX);
    uint32_t n = uint32_t(size);
    for (int i=0; i<4; ++i) assert(std::fputc((n >> (8*i)) & 255, out) != EOF);
    assert(std::fwrite(data,1,size,out)==size);
}
int main(int argc,char **argv) {
    if (argc==4 && std::strcmp(argv[1],"--wire")==0) {
        FILE *in=std::fopen(argv[2],"rb"), *out=std::fopen(argv[3],"wb"); assert(in && out);
        for (;;) {
            uint8_t head[4]; const size_t got=std::fread(head,1,4,in); if (!got) break; assert(got==4);
            uint32_t n=0; for(int i=0;i<4;++i) n|=uint32_t(head[i])<<(8*i);
            std::vector<uint8_t> input(n); assert(std::fread(input.data(),1,n,in)==n);
            Root value; TableReport report;
            const bool ok=RootLoad(value,input.data(),input.size(),&report);
            uint32_t counters[]={uint32_t(report.unknown),uint32_t(report.kind_mismatch),uint32_t(report.widened),uint32_t(report.clamped),uint32_t(report.duplicate),uint32_t(report.malformed),uint32_t(ok)};
            for(auto count:counters) { uint8_t raw[4]; for(int i=0;i<4;++i) raw[i]=uint8_t(count>>(8*i)); bytes(out,raw,4); }
            const auto size=RootMeasure(value); assert(size>=0);
            std::vector<uint8_t> wire(size); assert(RootSave(value,wire.data(),size)==size);
            bytes(out,wire.data(),wire.size());
        }
        assert(std::fclose(in)==0 && std::fclose(out)==0); return 0;
    }
    assert(argc==3);
    std::ifstream input(argv[1]); assert(input);
    FILE *out=std::fopen(argv[2],"wb"); assert(out);
    std::string line;
    while(std::getline(input,line)) {
        Root value; TableReport report;
        assert(RootFromJson(value,line.data(),line.size(),&report));
        std::vector<uint8_t> wire(RootMeasure(value));
        assert(RootSave(value,wire.data(),wire.size())==int64_t(wire.size()));
        bytes(out,wire.data(),wire.size());
        std::vector<char> json(RootToJsonMeasure(value));
        assert(RootToJson(value,json.data(),json.size())==int64_t(json.size()));
        bytes(out,json.data(),json.size());
        uint32_t counters[]={uint32_t(report.unknown),uint32_t(report.kind_mismatch),uint32_t(report.clamped),uint32_t(report.duplicate)};
        for(auto count:counters) { uint8_t raw[4]; for(int i=0;i<4;++i) raw[i]=uint8_t(count>>(8*i)); bytes(out,raw,4); }
    }
    assert(std::fclose(out)==0);
}
