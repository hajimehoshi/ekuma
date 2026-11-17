// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "BigInt exponentiation by a huge exponent"
flags: [noStrict]
---*/

var r = [0n ** (2n ** 64n), 1n ** (2n ** 64n), (-1n) ** (2n ** 64n + 1n)];
try { (2n ** 256n) ** (2n ** 24n); r.push("no error"); } catch (e) { r.push(e instanceof RangeError); }
assert.sameValue(String(r.join()), "0,1,-1,true");
