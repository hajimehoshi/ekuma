// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "A global assignment looks up an inherited binding before evaluating its value"
---*/

const log = [];
Object.setPrototypeOf(globalThis, new Proxy({}, {
	has(target, key) {
		if (key !== "inheritedBinding") return Reflect.has(target, key);
		log.push("has");
		return true;
	},
	get() { log.push("read"); return 1; },
	set(target, key, value) {
		log.push("set " + value);
		return true;
	}
}));
const source = { get value() { log.push("get"); return 2; } };
inheritedBinding = source.value;
assert.sameValue(log.join(), "has,get,has,set 2");

log.length = 0;
inheritedBinding += source.value;
assert.sameValue(log.join(), "has,has,read,get,has,set 3");
