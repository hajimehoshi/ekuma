// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Object.assign skips the keys that a getter of the source deletes and adds"
flags: [noStrict]
---*/

const src = {a: 1, get b() { delete src.c; src.d = 4; return 2; }, c: 3};
assert.sameValue(JSON.stringify(Object.assign({}, src)), '{"a":1,"b":2}');
