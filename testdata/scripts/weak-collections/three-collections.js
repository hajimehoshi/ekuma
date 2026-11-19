// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "a key of three weak collections keeps its entries through deletions and additions in different orders"
flags: [noStrict]
---*/

var a = new WeakMap(), b = new WeakMap(), c = new WeakMap(), k = {};
a.set(k, 1);
b.set(k, 2);
c.set(k, 3);
assert.sameValue(b.delete(k), true);
assert.sameValue(a.get(k), 1);
assert.sameValue(b.has(k), false);
assert.sameValue(c.get(k), 3);
assert.sameValue(a.delete(k), true);
assert.sameValue(a.has(k), false);
assert.sameValue(b.has(k), false);
assert.sameValue(c.get(k), 3);
b.set(k, 4);
a.set(k, 5);
assert.sameValue(a.get(k), 5);
assert.sameValue(b.get(k), 4);
assert.sameValue(c.get(k), 3);
c.set(k, 6);
a.set(k, 7);
assert.sameValue(a.get(k), 7);
assert.sameValue(b.get(k), 4);
assert.sameValue(c.get(k), 6);
assert.sameValue(c.delete(k), true);
assert.sameValue(b.delete(k), true);
assert.sameValue(a.delete(k), true);
assert.sameValue(a.has(k), false);
assert.sameValue(b.has(k), false);
assert.sameValue(c.has(k), false);
