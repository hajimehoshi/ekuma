// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "bigint"
flags: [noStrict]
---*/

assert.sameValue(String((2n ** 64n + 1n).toString === undefined ? "" : String(2n ** 64n + 1n) + typeof 1n), "18446744073709551617bigint");
