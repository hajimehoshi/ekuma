// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "a key of two weak collections keeps its entries when the first collection deletes it first"
flags: [noStrict]
---*/

var a = new WeakMap(), b = new WeakMap(), k = {};
a.set(k, 1);
b.set(k, 2);
assert.sameValue(a.delete(k), true);
assert.sameValue(a.has(k), false);
assert.sameValue(b.get(k), 2);
assert.sameValue(b.delete(k), true);
assert.sameValue(b.has(k), false);
assert.sameValue(a.delete(k), false);
assert.sameValue(b.delete(k), false);
b.set(k, 3);
a.set(k, 4);
assert.sameValue(a.get(k), 4);
assert.sameValue(b.get(k), 3);
