# Fixed array growth on the variable table wire

A fixed array read accepts an older writer's shorter array and fills the new
slots with value-initialized elements. A larger array refuses by name. Scalar
slots are zero; nested table slots are distinct new values at their own defaults.
The generated writer requires the full current length.
