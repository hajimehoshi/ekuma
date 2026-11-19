// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "a key of two weak collections keeps its entries when the second collection deletes it first"
flags: [noStrict]
---*/

var a = new WeakMap(), b = new WeakSet(), k = {};
a.set(k, 1);
b.add(k);
assert.sameValue(b.delete(k), true);
assert.sameValue(b.has(k), false);
assert.sameValue(a.get(k), 1);
b.add(k);
assert.sameValue(b.has(k), true);
assert.sameValue(a.get(k), 1);
assert.sameValue(a.delete(k), true);
assert.sameValue(a.get(k), undefined);
assert.sameValue(b.has(k), true);
a.set(k, 5);
assert.sameValue(a.get(k), 5);
assert.sameValue(b.has(k), true);
