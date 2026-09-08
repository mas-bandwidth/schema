#include "ListTable.h"
#include <cassert>
#include <cstdio>
#include <fstream>
#include <string>
#include <vector>
#include <cstring>
using namespace rustlist;
static void bytes(FILE *out, const void *data, size_t size) {
    assert(size <= UINT32_MAX);
    uint32_t n = uint32_t(size);
    for (int i=0; i<4; ++i) assert(std::fputc((n >> (8*i)) & 255, out) != EOF);
    assert(std::fwrite(data,1,size,out)==size);
}
int main(int argc,char **argv) {
    assert(argc==3);
    std::ifstream input(argv[1]); assert(input);
    FILE *out=std::fopen(argv[2],"wb"); assert(out);
    std::string line;
    while(std::getline(input,line)) {
        RootBuilder value; TableReport report;
        assert(RootFromJson(value,line.data(),line.size(),&report));
        std::vector<uint8_t> wire(RootMeasure(value));
        assert(RootSave(value,wire.data(),wire.size())==int64_t(wire.size()));
        bytes(out,wire.data(),wire.size());
        assert(value.Lock());
        const Root *root=value.AsConst();
        std::vector<char> json(RootToJsonMeasure(root));
        assert(RootToJson(root,json.data(),json.size())==int64_t(json.size()));
        bytes(out,json.data(),json.size());
        uint32_t counters[]={uint32_t(report.unknown),uint32_t(report.kind_mismatch),uint32_t(report.clamped),uint32_t(report.duplicate)};
        for(auto count:counters) { uint8_t raw[4]; for(int i=0;i<4;++i) raw[i]=uint8_t(count>>(8*i)); bytes(out,raw,4); }
    }
    assert(std::fclose(out)==0);
}
