// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "a subclass of Map or Set made from a Map or Set calls the adder of the subclass"
---*/

const calls = [];

class M extends Map {
	set(k, v) {
		calls.push(k);
		return super.set(k, v);
	}
}
new M(new Map([[1, 2]]));

class S extends Set {
	add(v) {
		calls.push(v);
		return super.add(v);
	}
}
new S(new Set([3]));

assert.sameValue(calls.join(), "1,3");
