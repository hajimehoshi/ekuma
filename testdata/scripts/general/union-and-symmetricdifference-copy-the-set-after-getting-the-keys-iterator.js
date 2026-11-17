// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "union and symmetricDifference copy the set after getting the keys iterator"
flags: [noStrict]
---*/

var s = new Set([1, 2, 3]);
var other = { size: 0, has() {}, keys() { return { get next() { s.clear(); s.add(4); return () => ({ done: true }); } }; } };
assert.sameValue(String([...s.union(other)].join() + "|" + [...s.symmetricDifference(other)].join()), "4|4");
