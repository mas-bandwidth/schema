#include "VoidWire.h"
#include <cstdio>
#include <cstring>
#include <new>

#define CHECK(test, message) do { if (!(test)) { std::printf("FAILED: %s\n", message); return 1; } } while (0)

using namespace packetvoid;

// Read from hand-calculated bytes, never from this test's writer. Every
// logical one-byte input has a padded, aligned allocation for the runtime.
int main(int argc, char **argv)
{
    const bool read_only = argc == 2 && std::strcmp(argv[1], "read") == 0;
    const bool write_only = argc == 2 && std::strcmp(argv[1], "write") == 0;
    CHECK(argc == 1 || read_only || write_only, "packet-void test mode");
    alignas(8) uint8_t buffer[64] = {};
    alignas(8) const uint8_t ping[16] = {0x01};
    alignas(8) const uint8_t none[16] = {0x00};
    alignas(8) const uint8_t payload[16] = {0x52};
    alignas(8) const uint8_t framed[16] = {0xCD};
    alignas(8) const uint8_t batch_wire[16] = {0x05};
    alignas(8) const uint8_t pong[16] = {0x02};
    alignas(8) const uint8_t invalid[16] = {0x03};

    CHECK(uint8_t(MixedType::Count) == 2 && uint8_t(MixedType::Max) == 2, "mixed tag extent");
    CHECK(uint8_t(TagsType::Count) == 2 && uint8_t(SingleType::Count) == 1 && uint8_t(EmptyType::Count) == 0, "void tag counts");
    CHECK(std::strcmp(EnumName(MixedType::Ping), "Ping") == 0 &&
          std::strcmp(EnumName(TagsType::Pong), "Pong") == 0, "void debug names");
    CHECK(MixedMaxBits == 23 && TagsMaxBits == 2 && SingleMaxBits == 1 && EmptyMaxBits == 0, "void maximum bits");

    if (!read_only)
    {
        Mixed value;
        CHECK(value.type == MixedType::None, "mixed starts at None");
        value.type = MixedType::Ping;
        serialize::WriteStream w(buffer, sizeof(buffer));
        CHECK(WriteMixed(w, value), "packet-void write succeeds");
        CHECK(w.GetBitsProcessed() == 2, "packet-void write has tag bits only");
        w.Flush();
        CHECK(w.GetBytesProcessed() == 1 && buffer[0] == 0x01, "mixed ping byte");

        value.type = MixedType::None;
        serialize::WriteStream wn(buffer, sizeof(buffer));
        CHECK(WriteMixed(wn, value) && wn.GetBitsProcessed() == 2, "None writes tag bits");
        wn.Flush();
        CHECK(buffer[0] == 0x00, "None byte");

        ::new ((void*) &value.payload) DefaultArm{};
        value.type = MixedType::Payload;
        CHECK(value.payload.entries_count == 0 && value.payload.marker == 5 &&
              value.payload.entries[0].retries == -1 && value.payload.entries[1].retries == -1,
              "payload construction defaults");
        serialize::WriteStream wp(buffer, sizeof(buffer));
        CHECK(WriteMixed(wp, value) && wp.GetBitsProcessed() == 7, "payload writes seven bits");
        wp.Flush();
        CHECK(buffer[0] == 0x52, "payload byte");

        Frame frame;
        frame.lead = 5; frame.choice.type = MixedType::Ping; frame.tail = 6;
        serialize::WriteStream wf(buffer, sizeof(buffer));
        CHECK(WriteFrame(wf, frame) && wf.GetBitsProcessed() == 8, "tag between fields writes eight bits");
        wf.Flush();
        CHECK(buffer[0] == 0xCD, "tag between fields byte");

        Batch batch;
        batch.values_count = 1; batch.values[0].type = MixedType::Ping;
        serialize::WriteStream wb(buffer, sizeof(buffer));
        CHECK(WriteBatch(wb, batch) && wb.GetBitsProcessed() == 4, "short union array writes four bits");
        wb.Flush();
        CHECK(buffer[0] == 0x05, "short union array byte");

        Tags tags; tags.type = TagsType::Pong;
        serialize::WriteStream wt(buffer, sizeof(buffer));
        CHECK(WriteTags(wt, tags) && wt.GetBitsProcessed() == 2, "all-void writes tag");
        wt.Flush(); CHECK(buffer[0] == 0x02, "all-void byte");
        Single single; single.type = SingleType::Ack;
        serialize::WriteStream ws(buffer, sizeof(buffer));
        CHECK(WriteSingle(ws, single) && ws.GetBitsProcessed() == 1, "single void writes tag");
        ws.Flush(); CHECK(buffer[0] == 0x01, "single void byte");
        Empty empty;
        serialize::WriteStream we(buffer, sizeof(buffer));
        CHECK(WriteEmpty(we, empty) && we.GetBitsProcessed() == 0, "empty union writes nothing");

        value.type = MixedType(3);
        serialize::WriteStream wi(buffer, sizeof(buffer));
        CHECK(!WriteMixed(wi, value) && wi.GetBitsProcessed() == 0, "invalid tag writes nothing");
    }

    if (!write_only)
    {
        Mixed value;
        ::new ((void*) &value.payload) DefaultArm{};
        value.type = MixedType::Payload;
        serialize::ReadStream r(ping, 1);
        CHECK(ReadMixed(r, value), "packet-void read succeeds");
        CHECK(value.type == MixedType::Ping, "packet-void read selects Ping");
        CHECK(r.GetBitsProcessed() == 2, "packet-void read has tag bits only");
        // Inactive payload bytes are unspecified. Only inspect the selected tag.
        serialize::ReadStream rn(none, 1);
        CHECK(ReadMixed(rn, value) && value.type == MixedType::None && rn.GetBitsProcessed() == 2, "read None");
        ::new ((void*) &value.payload) DefaultArm{};
        value.type = MixedType::Payload;
        for (int repeat = 0; repeat < 2; ++repeat)
        {
            value.payload.entries_count = 2;
            value.payload.entries[0].retries = 99; value.payload.entries[1].retries = 99;
            serialize::ReadStream rp(payload, 1);
            CHECK(ReadMixed(rp, value), "payload oracle read succeeds");
            CHECK(value.type == MixedType::Payload && rp.GetBitsProcessed() == 7 &&
                  value.payload.entries_count == 0 && value.payload.marker == 5, "payload oracle shape");
            CHECK(value.payload.entries[0].retries == -1 && value.payload.entries[1].retries == -1,
                  "payload defaults on repeated selection");
        }
        Frame frame;
        serialize::ReadStream rf(framed, 1);
        CHECK(ReadFrame(rf, frame), "framed oracle read succeeds");
        CHECK(rf.GetBitsProcessed() == 8 && frame.lead == 5 && frame.choice.type == MixedType::Ping && frame.tail == 6,
              "tag between fields reads without payload or alignment");
        Batch batch;
        serialize::ReadStream rb(batch_wire, 1);
        CHECK(ReadBatch(rb, batch) && rb.GetBitsProcessed() == 4 && batch.values_count == 1 &&
              batch.values[0].type == MixedType::Ping, "short union array oracle");
        Tags tags;
        serialize::ReadStream rt(pong, 1);
        CHECK(ReadTags(rt, tags) && tags.type == TagsType::Pong && rt.GetBitsProcessed() == 2, "all-void oracle");
        Single single;
        serialize::ReadStream rs(ping, 1);
        CHECK(ReadSingle(rs, single) && single.type == SingleType::Ack && rs.GetBitsProcessed() == 1, "single void oracle");
        Empty empty;
        serialize::ReadStream re(none, 0);
        CHECK(ReadEmpty(re, empty) && empty.type == EmptyType::None && re.GetBitsProcessed() == 0, "empty union reads no bits");
        serialize::ReadStream ri(invalid, 1);
        CHECK(!ReadMixed(ri, value), "invalid tag read rejects");
        serialize::ReadStream truncated(ping, 0);
        CHECK(!ReadMixed(truncated, value), "missing tag read rejects");
    }
    std::puts("packet-void C++: independent bytes and bit counts passed");
    return 0;
}
