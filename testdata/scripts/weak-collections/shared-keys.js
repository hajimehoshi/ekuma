// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Weak collections with the same object or symbol key keep independent entries"
flags: [noStrict]
---*/

var r = [];
[{}, Symbol("s")].forEach(function(k) {
	var m1 = new WeakMap(), m2 = new WeakMap(), s = new WeakSet();
	m1.set(k, 1);
	m2.set(k, 2);
	s.add(k);
	m1.delete(k);
	r.push(m1.has(k), m2.get(k), s.has(k));
	s.delete(k);
	r.push(m2.get(k), s.has(k));
});
assert.sameValue(r.join(), "false,2,true,2,false,false,2,true,2,false");
