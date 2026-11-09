// The storage-bound refusal in a release build (docs/SPEC.md §5.1): a counted
// array whose count is 9 indexes past the storage it describes, and the gate is
// run without -ea, so a refusal here cannot be the debug assert.

import storagerelease.RootFixed;
import storagerelease.RootMessage;
import storagerelease.TableVocabulary;

public final class StorageRelease {
    private StorageRelease() {}

    public static void main(String[] args) {
        RootFixed.Value value = new RootFixed.Value();
        value.itemsCount = 9; // the declared bound is 8
        RootFixed.Value[] values = new RootFixed.Value[] { value };

        try {
            TableVocabulary.Report measureReport = new TableVocabulary.Report();
            if (RootMessage.measureMessages(values, measureReport) != -1) {
                System.out.println("FAIL storage release: measureMessages wrote a count past its storage");
                System.exit(1);
            }
            TableVocabulary.Report saveReport = new TableVocabulary.Report();
            if (RootMessage.saveMessages(values, new byte[4096], saveReport) != -1) {
                System.out.println("FAIL storage release: saveMessages wrote a count past its storage");
                System.exit(1);
            }
        } catch (RuntimeException e) {
            System.out.println("FAIL storage release: " + e.getClass().getSimpleName() + " instead of a refusal");
            System.exit(1);
        }

        System.out.println("storage release gate: a count past its storage is refused without -ea");
    }
}
