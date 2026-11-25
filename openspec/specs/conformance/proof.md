# Proof: conformance

Which test proves each requirement. `make spec` checks that every requirement is listed here and that every test named exists.

| Requirement | Proven by |
| --- | --- |
| A conformance case is data | `conformance/conformance_test.go:TestConformance` (every case under `conformance/cases/` is found and run. That a field the runner does not know fails the case is not tested: the decoder is set to refuse one, and nothing asserts it) |
| A run's event log matches its case | `conformance/conformance_test.go:TestConformance` (the eight cases pass. That a differing log fails is not tested by a test of its own; it was seen while the cases were written, when each case failed against an empty log) |
| A case's log can be recorded | Not tested: recording was used by hand to write the eight `events.txt` files, and no test runs it |
