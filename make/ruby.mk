# make/ruby.mk — the RUBY leg (docs/CONTRIBUTING.md, "Adding a language";
# docs/PORT-RUBY.md is the port's design). Included by the Makefile's
# wildcard include; the Makefile names no language. The leg registers itself
# at the end of this file.
#
# Scaffolded by `schema new-leg ruby`. The pinned interpreters and analyzers
# are make/ruby-toolchain.mk's. The target is behind the schema_leg_ruby build
# tag until the packet layer (compiler/target_ruby.go), so its tests build the
# compiler with the tag.

# test-ruby: the leg's make test half. The toolchain check is a prerequisite
# from its own fragment; here the emitter's package, which generates into temp
# directories and runs the generated namespace under both pinned interpreters
# (internal/codegen/ruby/rubyrun), and the target through the compiler's
# driver in the tagged build. Both go tests run through the slow-gate proof,
# which sets SCHEMA_SLOW=1 and refuses a gate that skipped.
.PHONY: test-ruby update-goldens-ruby
test-ruby: ruby-toolchain-check
	sh test/slowgate/proof test-ruby-skeleton 'TestRubySkeleton' -count=1 -timeout 600s -tags schema_leg_ruby ./internal/codegen/ruby/...
	sh test/slowgate/proof test-ruby-compiler 'TestRubyTargetIsRegistered TestRubyGeneratesAnEmptyUnitThroughTheDriver TestRubyRefusesADeclarationThroughTheDriver TestNewLegRubyRecipe' -count=1 -tags schema_leg_ruby -run 'Ruby' ./compiler
update-goldens-ruby: ;

# ruby-skeleton-negative-control: the skeleton's controls, each planting its
# fault in a copy of what its gate covers (a generated file, the runtime, the
# driver, the keyword record) and going red by name: TestRubySkeleton's
# *-negative-control subtests under both pinned interpreters
# (make/negative-controls.json, ruby-skeleton).
.PHONY: ruby-skeleton-negative-control
ruby-skeleton-negative-control: ruby-toolchain-check
	sh test/slowgate/proof ruby-skeleton-negative-control 'TestRubySkeleton' -count=1 -timeout 600s -tags schema_leg_ruby -run 'TestRubySkeleton/negative-control' ./internal/codegen/ruby/

# The conformance driver answers every surface ABSENT until a layer carries
# one (test/conformance/ruby/driver), so the leg builds nothing for it.
build/conformance-ruby: ;

# The leg joins the aggregate toolchain gate with the skeleton
# (docs/PORT-RUBY.md, THE PIN): TEST_LEGS now knows test-ruby, so
# SCHEMA_SKIP_LEGS=ruby is a skip the aggregate control can name.
TEST_LEGS           += test-ruby
TOOLCHAIN_LEGS      += ruby
TOOLCHAIN_PINS_ruby := RUBY RUBY_FLOOR RUBOCOP STEEP RBS
CONFORMANCE_LEGS    += $(call unless_skipped,ruby,build/conformance-ruby)
GOLDENS_LEGS        += update-goldens-ruby
