# frozen_string_literal: true

# The schema runtime of one generated unit (docs/PORT-RUBY.md, ERRORS). The
# compiler emits this file as <package>/_schema.rb with the module line naming
# the unit's own module, so every unit defines its own pair of errors and two
# units never share or shadow each other's.
module SchemaRuntimeUnit
  # A read refused its input. No value comes with it, and the caller's
  # objects are untouched. reason is the refusal's name, a diagnostic that is
  # never compared across languages.
  class Refused < StandardError
    attr_reader :reason

    def initialize(reason, detail = nil)
      @reason = reason
      super(detail.nil? ? reason : "#{reason}: #{detail}")
    end
  end

  # A write refused a value outside its contract, before writing a byte. path
  # names the field and rule the broken rule.
  class InvalidValue < StandardError
    attr_reader :path, :rule

    def initialize(path, rule)
      @path = path
      @rule = rule
      super("#{path}: #{rule}")
    end
  end
end
