# Rules for anything working in this repo

## Run the tests of what you changed

```bash
make check
```

The whole-suite budget is 90 seconds, including the smoke test and its fresh build, confirmed on an otherwise idle Mac at 26.34 seconds on 2026-10-14 (asabirov/code-smells-skill#5).
Each full run lists up to ten slowest tests and files. This report never changes the test result.
File times sum top-level test durations, including their subtests once. They exclude build time and smoke-test time.

This runs the scanner's tests (`make test`, which needs Go and Bun) and a smoke
test of `bin/smells` on a copy of the fixture project (`make smoke`).

## Test the function, not the process

One test per entry point proves the command line is wired. Every other
assertion calls the function directly.

An assertion taken through a subprocess costs about three orders of magnitude
more than the same assertion taken through a function call, and it proves
nothing the one wiring test has not already proved.

## Every suite has a per-test budget

A suite without one degrades where nobody is looking, and the first sign of it
is a timeout on a busy machine. Raising the budget to get a green run treats
the symptom and loses the signal.

## A test earns its place by failing first

Write the code, then the test, in the same change. Test-first is not the rule
here; watching the test fail before trusting it is.

The exception is a bug somebody reported. Write that test first and show it
failing, because that is the only thing proving the fix addresses what they hit
rather than something beside it.

## Prove by running, not by reading

A test asserting that a file contains the right string passes whether or not
the thing that string names actually works.

## The README is the spec

It says what this does today, and it opens by naming the choice this design
makes, the alternatives it turned down, and the fact that decided between them.
A change is finished when that file matches what shipped.
