#include "VoidWire.h"
#include <stdio.h>
#include <string.h>

#define CHECK(test, message) do { if (!(test)) { printf("FAILED: %s\n", message); return 1; } } while (0)

int main(int argc, char **argv)
{
    const int read_only = argc == 2 && strcmp(argv[1], "read") == 0;
    const int write_only = argc == 2 && strcmp(argv[1], "write") == 0;
    /* Each logical one-byte input has a padded, aligned allocation. */
    uint64_t buffer_words[8] = {0}, oracle_words[2] = {0};
    unsigned char *buffer = (unsigned char *) buffer_words;
    unsigned char *oracle = (unsigned char *) oracle_words;
    serialize_write_stream_t w;
    serialize_read_stream_t r;
    packetvoid_mixed_t value;
    packetvoid_tags_t tags;
    packetvoid_single_t single;
    packetvoid_empty_t empty;
    packetvoid_frame_t frame;
    packetvoid_batch_t batch;
    int repeat;
    CHECK(argc == 1 || read_only || write_only, "packet-void test mode");
    CHECK(PACKETVOID_MIXED_TYPE_MAX == 2, "mixed tag extent");
    CHECK(PACKETVOID_TAGS_TYPE_MAX == 2 && PACKETVOID_SINGLE_TYPE_MAX == 1 && PACKETVOID_EMPTY_TYPE_MAX == 0, "void tag extents");
    CHECK(strcmp(packetvoid_enum_name_mixed_type(PACKETVOID_MIXED_TYPE_PING), "Ping") == 0 &&
          strcmp(packetvoid_enum_name_tags_type(PACKETVOID_TAGS_TYPE_PONG), "Pong") == 0, "void debug names");
    CHECK(PACKETVOID_MIXED_MAX_BITS == 23 && PACKETVOID_TAGS_MAX_BITS == 2 && PACKETVOID_SINGLE_MAX_BITS == 1 && PACKETVOID_EMPTY_MAX_BITS == 0, "void maximum bits");
    memset(&value, 0, sizeof(value));
    memset(&tags, 0, sizeof(tags));
    memset(&single, 0, sizeof(single));
    memset(&empty, 0, sizeof(empty));
    memset(&frame, 0, sizeof(frame));
    memset(&batch, 0, sizeof(batch));

    if (!read_only)
    {
        CHECK(value.type == PACKETVOID_MIXED_TYPE_NONE, "mixed starts at None");
        value.type = PACKETVOID_MIXED_TYPE_PING;
        serialize_write_stream_init(&w, buffer, sizeof(buffer_words));
        CHECK(packetvoid_write_mixed(&w, &value), "packet-void write succeeds");
        CHECK(serialize_write_bits_processed(&w) == 2, "packet-void write has tag bits only");
        serialize_write_flush(&w);
        CHECK(serialize_write_bytes_processed(&w) == 1 && buffer[0] == 0x01, "mixed ping byte");

        value.type = PACKETVOID_MIXED_TYPE_NONE;
        serialize_write_stream_init(&w, buffer, sizeof(buffer_words));
        CHECK(packetvoid_write_mixed(&w, &value) && serialize_write_bits_processed(&w) == 2, "None writes tag bits");
        serialize_write_flush(&w); CHECK(buffer[0] == 0x00, "None byte");

        value.as.payload = packetvoid_new_default_arm();
        value.type = PACKETVOID_MIXED_TYPE_PAYLOAD;
        CHECK(value.as.payload.entries_count == 0 && value.as.payload.marker == 5 &&
              value.as.payload.entries[0].retries == -1 && value.as.payload.entries[1].retries == -1,
              "payload construction defaults");
        serialize_write_stream_init(&w, buffer, sizeof(buffer_words));
        CHECK(packetvoid_write_mixed(&w, &value) && serialize_write_bits_processed(&w) == 7, "payload writes seven bits");
        serialize_write_flush(&w); CHECK(buffer[0] == 0x52, "payload byte");

        frame.lead = 5; frame.choice.type = PACKETVOID_MIXED_TYPE_PING; frame.tail = 6;
        serialize_write_stream_init(&w, buffer, sizeof(buffer_words));
        CHECK(packetvoid_write_frame(&w, &frame) && serialize_write_bits_processed(&w) == 8, "tag between fields writes eight bits");
        serialize_write_flush(&w); CHECK(buffer[0] == 0xCD, "tag between fields byte");
        batch.values_count = 1; batch.values[0].type = PACKETVOID_MIXED_TYPE_PING;
        serialize_write_stream_init(&w, buffer, sizeof(buffer_words));
        CHECK(packetvoid_write_batch(&w, &batch) && serialize_write_bits_processed(&w) == 4, "short union array writes four bits");
        serialize_write_flush(&w); CHECK(buffer[0] == 0x05, "short union array byte");

        tags.type = PACKETVOID_TAGS_TYPE_PONG;
        serialize_write_stream_init(&w, buffer, sizeof(buffer_words));
        CHECK(packetvoid_write_tags(&w, &tags) && serialize_write_bits_processed(&w) == 2, "all-void writes tag");
        serialize_write_flush(&w); CHECK(buffer[0] == 0x02, "all-void byte");
        single.type = PACKETVOID_SINGLE_TYPE_ACK;
        serialize_write_stream_init(&w, buffer, sizeof(buffer_words));
        CHECK(packetvoid_write_single(&w, &single) && serialize_write_bits_processed(&w) == 1, "single void writes tag");
        serialize_write_flush(&w); CHECK(buffer[0] == 0x01, "single void byte");
        serialize_write_stream_init(&w, buffer, sizeof(buffer_words));
        CHECK(packetvoid_write_empty(&w, &empty) && serialize_write_bits_processed(&w) == 0, "empty union writes nothing");

        /* NO out-of-set tag case on the WRITE: the tag is a caller-error
           contract like every other write-side value, a serialize_assert that
           would abort this (assert-live) binary and that is gone under NDEBUG
           (SPEC §4.8, §5). The read side below refuses one in every build. */
    }

    if (!write_only)
    {
        value.as.payload = packetvoid_new_default_arm(); value.type = PACKETVOID_MIXED_TYPE_PAYLOAD;
        oracle[0] = 0x01;
        serialize_read_stream_init(&r, oracle, 1);
        CHECK(packetvoid_read_mixed(&r, &value), "packet-void read succeeds");
        CHECK(value.type == PACKETVOID_MIXED_TYPE_PING, "packet-void read selects Ping");
        CHECK(serialize_read_bits_processed(&r) == 2, "packet-void read has tag bits only");
        /* Do not inspect inactive payload bytes: they are unspecified. */
        oracle[0] = 0x00;
        serialize_read_stream_init(&r, oracle, 1);
        CHECK(packetvoid_read_mixed(&r, &value) && value.type == PACKETVOID_MIXED_TYPE_NONE && serialize_read_bits_processed(&r) == 2, "read None");
        value.as.payload = packetvoid_new_default_arm(); value.type = PACKETVOID_MIXED_TYPE_PAYLOAD;
        for (repeat = 0; repeat < 2; ++repeat)
        {
            value.as.payload.entries_count = 2;
            value.as.payload.entries[0].retries = 99; value.as.payload.entries[1].retries = 99;
            oracle[0] = 0x52;
            serialize_read_stream_init(&r, oracle, 1);
            CHECK(packetvoid_read_mixed(&r, &value), "payload oracle read succeeds");
            CHECK(value.type == PACKETVOID_MIXED_TYPE_PAYLOAD && serialize_read_bits_processed(&r) == 7 &&
                  value.as.payload.entries_count == 0 && value.as.payload.marker == 5, "payload oracle shape");
            CHECK(value.as.payload.entries[0].retries == -1 && value.as.payload.entries[1].retries == -1,
                  "payload defaults on repeated selection");
        }
        oracle[0] = 0xCD;
        serialize_read_stream_init(&r, oracle, 1);
        CHECK(packetvoid_read_frame(&r, &frame), "framed oracle read succeeds");
        CHECK(serialize_read_bits_processed(&r) == 8 && frame.lead == 5 && frame.choice.type == PACKETVOID_MIXED_TYPE_PING && frame.tail == 6,
              "tag between fields reads without payload or alignment");
        oracle[0] = 0x05;
        serialize_read_stream_init(&r, oracle, 1);
        CHECK(packetvoid_read_batch(&r, &batch) && serialize_read_bits_processed(&r) == 4 && batch.values_count == 1 &&
              batch.values[0].type == PACKETVOID_MIXED_TYPE_PING, "short union array oracle");
        oracle[0] = 0x02;
        serialize_read_stream_init(&r, oracle, 1);
        CHECK(packetvoid_read_tags(&r, &tags) && tags.type == PACKETVOID_TAGS_TYPE_PONG && serialize_read_bits_processed(&r) == 2, "all-void oracle");
        oracle[0] = 0x01;
        serialize_read_stream_init(&r, oracle, 1);
        CHECK(packetvoid_read_single(&r, &single) && single.type == PACKETVOID_SINGLE_TYPE_ACK && serialize_read_bits_processed(&r) == 1, "single void oracle");
        serialize_read_stream_init(&r, oracle, 0);
        CHECK(packetvoid_read_empty(&r, &empty) && empty.type == PACKETVOID_EMPTY_TYPE_NONE && serialize_read_bits_processed(&r) == 0, "empty union reads no bits");
        oracle[0] = 0x03;
        serialize_read_stream_init(&r, oracle, 1);
        CHECK(!packetvoid_read_mixed(&r, &value), "invalid tag read rejects");
        serialize_read_stream_init(&r, oracle, 0);
        CHECK(!packetvoid_read_mixed(&r, &value), "missing tag read rejects");
    }
    puts("packet-void C: independent bytes and bit counts passed");
    return 0;
}
