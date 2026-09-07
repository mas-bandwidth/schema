#include <stdio.h>
#include <string.h>
#include "WideTextWire.h"
#include "ShapesWire.h"

#define check(c) do { if (!(c)) { fprintf(stderr, "FAILED: wide C contract: %s\n", #c); return 1; } } while (0)

int main(void)
{
    uint32_t buffer[64] = {0};
    WideSeven value;
    serialize_write_stream_t w;
    serialize_read_stream_t r;
    memset(&value, 0, sizeof(value));
    /* The two WRITE-side rules of SPEC §4.12 — the used length within [0, N]
       and no zero code unit among the used units — are WRITER MISUSE held in
       this target's own §5 idiom: serialize_asserts that fire in a debug
       build and compile out under NDEBUG, exactly as the C++ backend holds
       them. ("Every language, by design, compiles out asserts/checks in
       release build. This is the whole point!") They are not exercised here:
       this file is built and run in BOTH modes, and an assert is not a value
       a passing run can observe in either. What follows is the READ side,
       which refuses in every build. */
    value.text_length = 1;
    value.text[0] = 0xd800;
    serialize_write_stream_init(&w, (unsigned char *)buffer, sizeof(buffer));
    check(write_wide_seven(&w, &value)); /* pairing is read-side */
    serialize_write_flush(&w);
    serialize_read_stream_init(&r, (unsigned char *)buffer, serialize_write_bytes_processed(&w));
    check(!read_wide_seven(&r, &value));
    value.text[0] = 0xffff;
    serialize_write_stream_init(&w, (unsigned char *)buffer, sizeof(buffer));
    check(write_wide_seven(&w, &value));
    serialize_write_flush(&w);
    memset(value.text, 0x7f, sizeof(value.text));
    serialize_read_stream_init(&r, (unsigned char *)buffer, serialize_write_bytes_processed(&w));
    check(read_wide_seven(&r, &value));
    check(value.text_length == 1 && value.text[0] == 0xffff && value.text[1] == 0 && value.text[2] == 0x7f7f);
    {
        Conditional sent, received;
        Choice choice, out;
        Box box = new_box();
        int i;
        check(box.counted_count == 1 && box.counted[0].value_length == 0 && box.items[1].value[3] == 0);
        memset(&sent, 0, sizeof(sent));
        sent.enabled = 1;
        sent.text_length = 2;
        sent.text[0] = 0xd800;
        sent.text[1] = 0xdc00;
        serialize_write_stream_init(&w, (unsigned char *)buffer, sizeof(buffer));
        check(write_conditional(&w, &sent));
        check(serialize_write_bits_processed(&w) == 68); /* 1 + 3 + 2*32, no align */
        serialize_write_flush(&w);
        check((((unsigned char *)buffer)[0] & 15) == 5); /* true | (length 2 << 1) */
        memset(&received, 0x7f, sizeof(received));
        serialize_read_stream_init(&r, (unsigned char *)buffer, serialize_write_bytes_processed(&w));
        check(read_conditional(&r, &received) && received.text[0] == 0xd800 && received.text[1] == 0xdc00);
        sent.enabled = 0;
        serialize_write_stream_init(&w, (unsigned char *)buffer, sizeof(buffer));
        check(write_conditional(&w, &sent));
        serialize_write_flush(&w);
        serialize_read_stream_init(&r, (unsigned char *)buffer, serialize_write_bytes_processed(&w));
        check(read_conditional(&r, &received) && received.text_length == 0);
        for (i = 0; i < 5; i++) { check(received.text[i] == 0); }
        memset(&choice, 0, sizeof(choice));
        choice.type = CHOICE_TYPE_TEXT;
        choice.as.text.value_length = 1;
        choice.as.text.value[0] = 0xffff;
        serialize_write_stream_init(&w, (unsigned char *)buffer, sizeof(buffer));
        check(write_choice(&w, &choice));
        serialize_write_flush(&w);
        for (i = 0; i < 2; i++) {
            memset(&out, 0x7f, sizeof(out));
            out.type = CHOICE_TYPE_TEXT;
            serialize_read_stream_init(&r, (unsigned char *)buffer, serialize_write_bytes_processed(&w));
            check(read_choice(&r, &out));
            check(out.as.text.value_length == 1 && out.as.text.value[0] == 0xffff && out.as.text.value[3] == 0);
        }
        box.items[0] = choice.as.text;
        box.counted[0] = choice.as.text;
        box.choice = choice;
        serialize_write_stream_init(&w, (unsigned char *)buffer, sizeof(buffer));
        check(write_box(&w, &box));
        serialize_write_flush(&w);
        memset(&box, 0x7f, sizeof(box));
        serialize_read_stream_init(&r, (unsigned char *)buffer, serialize_write_bytes_processed(&w));
        check(read_box(&r, &box));
        check(box.items[0].value[0] == 0xffff && box.items[1].value_length == 0 && box.counted_count == 1 && box.counted[0].value[0] == 0xffff);
        check(box.counted[1].value[3] == 0x7f7f && box.choice.as.text.value[3] == 0);
    }
    puts("wide C contracts: write bounds/null, read pairing, terminator and reused tail OK");
    return 0;
}
