// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "a Map or Set made from another Map or Set has its entries in order"
---*/

var m = new Map(), s = new Set();
for (var i = 0; i < 100; i++) {
	m.set("k" + i, i);
	s.add(i);
}
for (var i = 0; i < 100; i += 3) {
	m.delete("k" + i);
	s.delete(i);
}
var mc = new Map(m), sc = new Set(s);
assert.sameValue([...mc].join(), [...m].join(), "the entries of the Map");
assert.sameValue([...sc].join(), [...s].join(), "the values of the Set");
assert.sameValue(mc.size + ":" + sc.size, "66:66", "the sizes");
assert.sameValue(mc.get("k1"), 1, "a kept key of the Map");
assert.sameValue(mc.has("k3"), false, "a deleted key of the Map");
assert.sameValue(sc.has(98), true, "a kept value of the Set");
assert.sameValue(sc.has(99), false, "a deleted value of the Set");
mc.set("new", 1);
sc.add("new");
assert.sameValue(m.has("new") || s.has("new"), false, "the copies are separate");
