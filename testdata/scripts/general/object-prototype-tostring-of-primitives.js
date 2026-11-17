// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Object.prototype.toString of primitives"
flags: [noStrict]
---*/

var ts = Object.prototype.toString;
Object.defineProperty(String.prototype, Symbol.toStringTag, { value: "Str" });
assert.sameValue(String([ts.call(1), ts.call("s"), ts.call(true), ts.call(Symbol()), ts.call(1n)].join()), "[object Number],[object Str],[object Boolean],[object Symbol],[object BigInt]");
