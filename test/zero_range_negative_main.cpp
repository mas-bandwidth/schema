// The NEGATIVE CONTROL for the zero-range gate (SPEC §4.6): a field whose
// declared range excludes zero and declares no default.
//
// The checker refuses that shape. This main is compiled against the emission
// of a compiler with the gate REMOVED (`go build -overlay`, so no tracked file
// is written) and against NDEBUG, where the write-side range assert is gone —
// the shipping configuration in which the defect is silent. It demands that a
// FRESHLY CONSTRUCTED value fail to survive its own wire: the fresh `x` is 0,
// outside the declared [1, 255], so either the write cannot spell it or the
// read cannot accept it. A clean round trip here means the gate guards
// nothing, and the control fails.
//
// `tail` rides behind `x` for the same reason it does in the ludicrous corpus:
// a wire that mis-spells the field ahead of it also moves this byte.

#include <cstdio>
#include <cstring>

#include "RangeZeroWire.h"

int main()
{
    rangezero::Probe fresh;
    if ( fresh.x != 0 || fresh.tail != 0 )
    {
        printf( "NEGATIVE CONTROL FAILED: a freshly constructed value is not zero-initialized\n" );
        return 1;
    }

    uint8_t buffer[rangezero::ProbeMaxBytes + 8];
    memset( buffer, 0, sizeof( buffer ) );

    serialize::WriteStream ws( buffer, (int) rangezero::ProbeMaxBytes );
    const bool wrote = WriteProbe( ws, fresh );
    ws.Flush();

    rangezero::Probe out;
    bool read = false;
    if ( wrote )
    {
        serialize::ReadStream rs( buffer, ws.GetBytesProcessed() );
        read = ReadProbe( rs, out );
    }

    if ( wrote && read && out.x == fresh.x && out.tail == fresh.tail )
    {
        printf( "NEGATIVE CONTROL FAILED: the fresh value round-tripped through a range that excludes it\n" );
        return 1;
    }

    printf( "negative control: a defaultless [1, 255] field is born unwritable — wrote=%s read=%s x=%u tail=%u\n",
            wrote ? "true" : "false", read ? "true" : "false",
            (unsigned) out.x, (unsigned) out.tail );
    return 0;
}
