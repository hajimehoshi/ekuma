// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "A negative value for asUintN is too large"
flags: [noStrict]
negative:
  phase: runtime
  type: RangeError
---*/

BigInt.asUintN(2 ** 53 - 1, -1n);
