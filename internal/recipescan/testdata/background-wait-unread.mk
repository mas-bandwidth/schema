# A job's pid is kept and waited on, but nothing reads the wait's status:
# the generator failed, the wait said so, and the line went on to exit 0.
build/gen/.stamp: bin/gen
	@./bin/gen --out build/gen/a a.schema & p=$$!; \
	wait "$$p"; \
	echo generated
	@touch $@
