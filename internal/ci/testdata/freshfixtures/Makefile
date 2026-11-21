# The F-194 corpus: a producer that writes fixtures into a directory that
# persists between runs, and a consumer that reads them.
#
#   fresh        the recipe shape the real rules use now (tables-go-module,
#                tables-go-fuzz, goose-accel-fixtures): through
#                test/slowgate/fixtures, which FIXTURES names
#   unnamed      the same with a producer command that does not name
#                --work: refused before anything runs
#   stale-shape  the shape they had: the producer's failure hidden by
#                `|| true`. Kept as the control that shows the defect: after
#                one good run, a failed producer still reads green.
PRODUCER ?= sh producer.sh ok one
FIXTURES ?= fixtures

.PHONY: fresh unnamed stale-shape
fresh:
	sh $(FIXTURES) work $(PRODUCER) --work work
	cat work/fixtures/Scene.cook

unnamed:
	sh $(FIXTURES) work $(PRODUCER)

stale-shape:
	$(PRODUCER) --work work > /dev/null 2>&1 || true
	cat work/fixtures/Scene.cook
