# Proof: sampling

Which test proves each requirement. `make spec` checks that every requirement is listed here and that every test named exists.

| Requirement | Proven by |
| --- | --- |
| A sample picks about the stated fraction | `spec/expr_test.go:TestASampleIsAFractionOfRunsAndTheSameOnesEveryTime` |
| The same seed always gets the same answer | `spec/expr_test.go:TestASampleIsAFractionOfRunsAndTheSameOnesEveryTime` (asserted within one process; that two servers agree follows from the pick being a hash of the seed, and is not tested) |
| Each seed picks its own set of runs | `spec/expr_test.go:TestASampleIsAFractionOfRunsAndTheSameOnesEveryTime` |
| A rate of 0 picks nothing and a rate of 1 picks everything | `spec/expr_test.go:TestASampleOfNoneOrAllAndARateThatIsNotOne` |
| A rate outside 0 to 1 is an error when the rule runs | `spec/expr_test.go:TestASampleOfNoneOrAllAndARateThatIsNotOne` (the error and its message are asserted on the expression; a run failing with `expression_failed` is not, and is in `advance` in `engine/transitions.go`) |
| A sample with the wrong kinds of argument is refused when published | `spec/expr_test.go:TestASampleOfNoneOrAllAndARateThatIsNotOne` (the expression's check is asserted; publishing a workflow that holds it is not) |
| A next rule can send a sample of runs to a person | Not tested: no test runs a workflow whose `next` rule calls `sample`; the function is in `spec/expr.go` and rules are worked out in `advance` in `engine/transitions.go` |
