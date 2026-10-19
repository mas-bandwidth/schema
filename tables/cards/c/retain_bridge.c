/* retain_bridge.c — the retained path's C bridge (DESIGN L3, docs/SPEC-TABLES.md
   §6.6). The generated Card codec keeps its retained load, measure and save
   file-local (static), so this translation unit includes the generated source
   and re-publishes those three entry points for the facade. It replaces
   CardTable.c in the card builds; it is the only definition of the generated
   symbols. The wrappers add nothing: they exist so Upgrade can reach the same
   retained round trip the generator already wrote. */
#include "cards.h"

#include "CardTable.c"

const Card *cards_retain_load(uint8_t *region, int64_t region_bytes,
                              const uint8_t *wire, int64_t wire_bytes,
                              TableRetain *retain, TableReport *report) {
    return card_load_retain(region, region_bytes, wire, wire_bytes, retain,
                            report);
}

int64_t cards_retain_measure(const Card *root, TableRetain *retain) {
    return card_measure_retain(root, retain);
}

int64_t cards_retain_save(const Card *root, TableRetain *retain, uint8_t *buffer,
                          int64_t capacity, TableReport *report) {
    return card_save_retain(root, retain, buffer, capacity, report);
}
