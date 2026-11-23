// SPFixed.h — the fixed form, form byte 3 (docs/SPEC-TABLES.md §3.4,
// docs/FIXED-FORM-ALGORITHM.md §1–§4, §5.2's hash and definitions digest).
//
// This port reads a file under its OWN layout hash. A file under another
// hash of the table's lineage (an older writer, §5) raises SPAbsent: the
// lineage plan compiler of §5.2 is not carried here, and a case it would
// answer is an absence, never a guess.

#import "SPWire.h"

extern NSString *const SPAbsent;

NSData *SPFixedLayout(SPDecl *root);           // §1: the layout bytes, count first
NSData *SPFixedDigest(SPDecl *root);           // §5.2: the v2 definitions digest
uint64_t SPFixedHash(SPDecl *root);            // fnv1a64 over layout then digest
NSUInteger SPFixedRecordSize(SPDecl *root);    // C(root)

NSData *SPSaveFixed(SPTableVal *root);
// lineage: every wire hash the table's lock records (NSNumber u64), or nil
SPTableVal *SPLoadFixed(NSData *file, SPDecl *root, NSSet<NSNumber *> *lineage);
