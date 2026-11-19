// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Object.assign sets the prototype of the target for an own __proto__ key of a source"
flags: [noStrict]
---*/

const r = Object.assign({}, {a: 1, ["__proto__"]: {z: 3}});
assert.sameValue(Object.getPrototypeOf(r).z, 3);
assert.sameValue(Object.hasOwn(r, "__proto__"), false);
assert.sameValue(Object.keys(r).join(), "a");
