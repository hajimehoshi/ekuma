// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Decorator metadata cannot be defined on a frozen class"
flags: [noStrict]
negative:
  phase: runtime
  type: TypeError
---*/

@(() => Object.freeze(function () {}))
class C {}
