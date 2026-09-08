#include "RowsTable.h"
#include <cassert>
#include <cstdio>
#include <cstdlib>
#include <fstream>
#include <string>
#include <vector>
using namespace mapdemo;
static void frame(FILE *out,const void *data,size_t size) {
    assert(size<=UINT32_MAX);
    for(int i=0;i<4;++i) assert(std::fputc((size>>(8*i))&255,out)!=EOF);
    assert(std::fwrite(data,1,size,out)==size);
}
#define CASE(ROOT) \
static void run_##ROOT(FILE *out,const std::string &text) { \
    ROOT##Builder builder; TableReport report; const ROOT *root=nullptr; void *memory=nullptr; bool ok=false; \
    if(text.rfind("wire:",0)==0) { \
        std::vector<uint8_t> wire;for(size_t i=5;i<text.size();i+=2)wire.push_back(uint8_t(std::stoul(text.substr(i,2),nullptr,16))); \
        const int64_t size=ROOT##LoadMeasure(wire.data(),wire.size()); \
        if(size>=0){memory=std::calloc(size?size:1,1);assert(memory);root=ROOT##Load(static_cast<uint8_t *>(memory),size,wire.data(),wire.size(),&report);ok=root!=nullptr;} \
    } else {ok=ROOT##FromJson(builder,text.data(),text.size(),&report);if(ok){assert(builder.Lock());root=builder.AsConst();}} \
    uint32_t counts[]={uint32_t(ok),uint32_t(report.unknown),uint32_t(report.kind_mismatch),uint32_t(report.widened),uint32_t(report.clamped),uint32_t(report.duplicate),uint32_t(report.malformed)}; \
    for(auto n:counts){uint8_t raw[4];for(int i=0;i<4;++i)raw[i]=uint8_t(n>>(8*i));frame(out,raw,4);} \
    std::vector<uint8_t> wire;std::vector<char> json; \
    if(ok){auto n=ROOT##Measure(root);assert(n>=0);wire.resize(n);assert(ROOT##Save(root,wire.data(),n)==n);n=ROOT##ToJsonMeasure(root);assert(n>=0);json.resize(n);assert(ROOT##ToJson(root,json.data(),n)==n);} \
    frame(out,wire.data(),wire.size());frame(out,json.data(),json.size());std::free(memory); \
}
CASE(Row)
CASE(WideRow)
CASE(EdgeRow)
int main(int argc,char **argv){
    assert(argc==3);std::ifstream input(argv[1]);assert(input);FILE *out=std::fopen(argv[2],"wb");assert(out);
    std::string line;while(std::getline(input,line)){auto tab=line.find('\t');assert(tab!=std::string::npos);auto root=line.substr(0,tab),text=line.substr(tab+1);
        if(root=="Row")run_Row(out,text);else if(root=="WideRow")run_WideRow(out,text);else if(root=="EdgeRow")run_EdgeRow(out,text);else assert(false);
    }assert(std::fclose(out)==0);
}
