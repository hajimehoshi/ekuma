// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "the checks after a get trap find the property that the trap defined with a concatenated key"
flags: [noStrict]
---*/

const key = "ab" + "cd";
const p = new Proxy({}, {
	get(target, k) {
		if (k !== key) throw new Error("wrong key");
		Object.defineProperty(target, k, {value: 42, configurable: false, writable: false});
		return 42;
	}
});
assert.sameValue(p[key], 42);
