// The tutorial's Part 6 load.cpp around the one printf a later copy of the
// unit changes (docs/TUTORIAL.md, review finding F-173): the example lines
// are held to the members the generated ShipConfig and TableReport of the
// unit they name really have. `loaded` and `report` stand for the program's.
#include "ConfigTable.h"
#include <cstdio>

int main()
{
    using namespace starlight;
    TableReport report;
    ShipConfig value;
    const ShipConfig * loaded = &value;
    bool ok = loaded != NULL;
    printf( "load: %s\n", ok ? "ok" : "refused" );
    // EXAMPLE
    printf( "  report: unknown=%d widened=%d\n", report.unknown, report.widened );
    return 0;
}
