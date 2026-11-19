// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "the target of Object.assign gets the keys in the order of the own keys of the source"
flags: [noStrict]
---*/

const s = Symbol("s");
const src = {b: 1, 2: 2, [s]: 3, a: 4, 1: 5};
assert.sameValue(Reflect.ownKeys(Object.assign({}, src)).map(String).join(), "1,2,b,a,Symbol(s)");
assert.sameValue(Object.keys(src).join(), "1,2,b,a");
