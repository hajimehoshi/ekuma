// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "BigInt.asUintN and asIntN of at most 64 bits on values beyond 64 bits"
flags: [noStrict]
---*/

var xs = [2n ** 64n + 5n, -(2n ** 64n) - 5n, 2n ** 100n - 1n, -(2n ** 70n) + 3n], bs = [1, 32, 63, 64];
assert.sameValue(String(xs.map(x => bs.map(b => BigInt.asUintN(b, x) + "/" + BigInt.asIntN(b, x)).join(" ")).join("|")), "1/-1 5/5 5/5 5/5|1/-1 4294967291/-5 9223372036854775803/-5 18446744073709551611/-5|1/-1 4294967295/-1 9223372036854775807/-1 18446744073709551615/-1|1/-1 3/3 3/3 3/3");
