# frozen_string_literal: true

# A small test harness on the standard library alone: a TestCase whose
# test_ methods the runner calls, the assertions they use, and subtests that
# report every failing case of a table rather than the first.
module Harness
  # an assertion that did not hold
  class Failure < StandardError; end

  CASES = [] # rubocop:disable Style/MutableConstant -- filled as test files load

  # the base of every test class; each test_ method is one test
  class TestCase
    def self.inherited(subclass)
      super
      CASES << subclass
    end

    def self.tests
      public_instance_methods(true).map(&:to_s).grep(/\Atest_/).sort
    end

    attr_reader :failures

    def initialize
      @failures = []
      @label = nil
    end

    # runs the block as one case of a table: a failure is recorded with its
    # label and the test goes on to the next case
    def subtest(label)
      outer = @label
      @label = outer ? "#{outer} / #{label}" : label.to_s
      yield
    rescue StandardError => e
      @failures << "[#{@label}] #{describe(e)}"
    ensure
      @label = outer
    end

    def describe(error)
      return error.message if error.is_a?(Failure)

      "#{error.class}: #{error.message}\n      #{(error.backtrace || []).first(4).join("\n      ")}"
    end

    def assert(condition, message = 'expected true')
      raise Failure, message unless condition

      true
    end

    def refute(condition, message = 'expected false')
      assert(!condition, message)
    end

    def assert_equal(want, got, message = nil)
      return true if want == got

      raise Failure, [message, "want #{want.inspect}, got #{got.inspect}"].compact.join(': ')
    end

    def assert_same(want, got, message = nil)
      return true if want.equal?(got)

      raise Failure, [message, "want the object #{want.inspect}, got #{got.inspect}"].compact.join(': ')
    end

    def assert_operator(left, operator, right, message = nil)
      return true if left.public_send(operator, right)

      raise Failure, [message, "want #{left.inspect} #{operator} #{right.inspect}"].compact.join(': ')
    end

    # the block raises klass (or a subclass), with a message matching pattern
    def assert_raises(klass, pattern = nil)
      yield
    rescue klass => e
      if pattern && !pattern.match?(e.message)
        raise Failure, "#{klass} raised, but #{e.message.inspect} does not match #{pattern.inspect}"
      end

      e
    else
      raise Failure, "#{klass} was not raised"
    end
  end
end
