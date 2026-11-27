# Third-party notices

project-box is MIT-licensed (see `LICENSE`). Two kinds of third-party
material reach what this repository distributes:

- **In the source tree:** `web/src/icons.js` inlines SVG glyph data from
  `@jack-henry/jh-icons`, under the Apache License 2.0.
- **In every `boxd` binary:** `go generate ./web` bundles the control
  room's npm dependencies into `web/dist`, and `go:embed` compiles that
  bundle into the daemon. Anyone distributing a built `boxd` distributes
  these too.

| Component | Version | License | Copyright |
|---|---|---|---|
| `@jack-henry/jh-icons` | 1.2.1 | Apache-2.0 | 2025 Jack Henry |
| `@jack-henry/jh-ui` | 1.15.5 | Apache-2.0 | 2025 Jack Henry |
| `@jack-henry/jh-core` | 1.6.1 | Apache-2.0 | 2025 Jack Henry |
| `lit`, `lit-html`, `lit-element`, `@lit/reactive-element` | 3.3.3 / 3.3.3 / 4.2.2 / 2.1.2 | BSD-3-Clause | 2017 Google LLC |
| `@lit/task` | 1.0.3 | BSD-3-Clause | 2017 Google LLC |
| `@lit-labs/router` | 0.1.4 | BSD-3-Clause | 2021 Google LLC |

The full license text of each ships in its package under
`web/node_modules/<package>/LICENSE` after `npm ci`, and at
<https://www.apache.org/licenses/LICENSE-2.0> for Apache-2.0. Go module
dependencies are listed in `go.mod`, each with its own license in the
module cache.

## NOTICE: Jack Henry Design System

Apache-2.0 §4(d) requires the NOTICE of `@jack-henry/jh-icons`, `jh-ui`
and `jh-core` to travel with derivative works. The three packages ship
the same file, reproduced verbatim:

```text
===========================================================
Forge Design System
Copyright 2025 Jack Henry
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
===========================================================

This product includes software derived from other projects, the licensing
and copyright for which are noted below.

-----------------------------------------------------------
1. Lit (lit)
-----------------------------------------------------------
This component is licensed under the **BSD 3-Clause License**.
The original copyright is as follows:

Copyright (c) 2017 Google LLC. All rights reserved.
```

## BSD-3-Clause: Lit

```text
BSD 3-Clause License

Copyright (c) 2017 Google LLC. All rights reserved.

Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are met:

1. Redistributions of source code must retain the above copyright notice, this
   list of conditions and the following disclaimer.

2. Redistributions in binary form must reproduce the above copyright notice,
   this list of conditions and the following disclaimer in the documentation
   and/or other materials provided with the distribution.

3. Neither the name of the copyright holder nor the names of its
   contributors may be used to endorse or promote products derived from
   this software without specific prior written permission.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS"
AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE
DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT HOLDER OR CONTRIBUTORS BE LIABLE
FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR
SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER
CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY,
OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.```
