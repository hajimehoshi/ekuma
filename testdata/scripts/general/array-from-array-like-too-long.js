// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Array.from rejects an array-like too long for an array"
flags: [noStrict]
negative:
  phase: runtime
  type: RangeError
---*/

Array.from({ length: 2 ** 32 });
