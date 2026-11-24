// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Direct eval reads mapped arguments from a resumed generator's registers"
flags: [noStrict]
---*/

// The generator's parameter registers can extend past the stack of the caller of next().
const params = Array.from({length: 256}, (_, i) => "p" + i);
const generate = eval("(function*(" + params.join() + ") { yield; p0++; return eval('arguments[0]'); })");
const it = generate(41);
it.next();
assert.sameValue(it.next().value, 42);
