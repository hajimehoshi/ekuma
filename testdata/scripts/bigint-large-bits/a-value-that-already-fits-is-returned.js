// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "a value that already fits is returned"
flags: [noStrict]
---*/

assert.sameValue(String([BigInt.asIntN(2 ** 53 - 1, -5n), BigInt.asUintN(2 ** 53 - 1, 5n)].join()), "-5,5");
