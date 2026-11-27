# Contributing

## Issues are the input

This repository builds itself: the box in `hack/dogfood` takes open issues
and runs agents against them. It takes only issues a maintainer has
labelled `box`. Anyone can open an issue, and every issue taken becomes a
builder's prompt, so the label is a person's decision about what the
factory builds. An unlabelled issue is read by a person first.

An issue that gets built well names one finding, the behaviour it expects,
and how a test would assert it.

## Pull requests

Most changes land through pull requests the factory opens. Hand-written
pull requests are welcome and are reviewed by hand. Before opening one:

- Read the section of `docs/ARCHITECTURE.md` your change touches, by
  heading, and check `docs/PLAN.md` §6 "Decisions already settled". A
  change that contradicts a row there must say so in its commit message.
- Run `./hack/check.sh`. It is the check CI runs verbatim: generate, gofmt,
  vet, build, `test -race`, govulncheck. The store's conformance suite also
  runs against the Spanner emulator when `SPANNER_EMULATOR_HOST` is set,
  and CI requires it.
- Test the behaviour you change. A property that is not tested is a
  comment.
- Write the commit subject as the finding, in a sentence ("A token is not a
  unit of money"), and use the body to say why.

`CLAUDE.md` holds the same rules in the form agents read; it is the
fuller list of what a change most often breaks.

By contributing you agree your contribution is licensed under the MIT
license in `LICENSE`.
