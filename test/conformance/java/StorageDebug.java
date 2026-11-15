// A count or length past its declared storage is a programmer error. Java
// catches it with assert when -ea is enabled (docs/SPEC.md §5.1).

import storagerelease.RootFixed;
import storagerelease.RootMessage;
import storagerelease.TableVocabulary;

public final class StorageDebug {
    private StorageDebug() {}

    private static void refused(String row, Runnable write) {
        try {
            write.run();
        } catch (AssertionError expected) {
            System.out.println("refused " + row);
            return;
        } catch (RuntimeException wrong) {
            throw new IllegalStateException("FAIL storage debug: " + row + " threw "
                    + wrong.getClass().getSimpleName() + " instead of asserting", wrong);
        }
        throw new IllegalStateException("FAIL storage debug: " + row + " did not assert");
    }

    public static void main(String[] args) {
        RootFixed.Value count = new RootFixed.Value();
        count.itemsCount = 9; // declared storage has eight slots
        RootFixed.Value[] counted = { count };
        refused("count measure", () -> RootMessage.measureMessages(counted, new TableVocabulary.Report()));
        refused("count save", () -> RootMessage.saveMessages(counted, new byte[4096], new TableVocabulary.Report()));

        RootFixed.Value length = new RootFixed.Value();
        length.wideLength = 9; // declared storage has eight bytes
        RootFixed.Value[] longText = { length };
        refused("length measure", () -> RootMessage.measureMessages(longText, new TableVocabulary.Report()));
        refused("length save", () -> RootMessage.saveMessages(longText, new byte[4096], new TableVocabulary.Report()));

        System.out.println("storage debug gate: count and length assert under -ea");
    }
}
