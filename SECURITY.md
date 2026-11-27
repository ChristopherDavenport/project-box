# Security

project-box runs agents that write code and land it with a forge token, so
a flaw here is a flaw in what that token can be made to do. Report one
privately: use **Report a vulnerability** on this repository's Security
tab. Please don't open a public issue, since an issue is also an input the
factory can be pointed at.

Include what an attacker controls, what they reach, and the commit you
tested against. This is an experimental, single-maintainer project: there
is no bounty and no response-time commitment, but reports are read.

Supported versions: only `main`. There are no releases.

## What the design already claims

These are the properties worth testing; a way to break one is a
vulnerability, not a feature request. Each is argued in
`docs/ARCHITECTURE.md` under the heading named.

- Evidence is append-only and hash-chained; a record cannot be altered or
  dropped without the chain showing it ("Evidence").
- Delegation intersects and never unions; an envelope narrows and never
  widens ("Identity and permissioning").
- Landing is fenced by the lease token and an expected-old-value push, and
  an approval binds to a commit, never a pull request ("The loop", "Human
  approval").
- The daemon is socket-only; anything network-shaped is a layer in front
  ("The daemon is a socket").
