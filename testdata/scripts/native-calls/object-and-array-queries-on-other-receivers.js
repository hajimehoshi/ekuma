// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "hasOwnProperty, Array.isArray, indexOf and includes called as methods see proxies, converted keys, holes and receivers other than arrays"
---*/

const log = [];
const proxy = new Proxy({}, {
	getOwnPropertyDescriptor() {
		log.push("trap");
		return undefined;
	},
});
assert.sameValue(proxy.hasOwnProperty("x"), false);
const key = {
	toString() {
		log.push("toString");
		return "a";
	},
};
assert.sameValue({ a: 1 }.hasOwnProperty(key), true);
assert.sameValue(log.join(), "trap,toString");
let line;
try {
	({}).hasOwnProperty({
		toString() {
			throw new Error("m");
		},
	});
} catch (e) {
	line = e.stack.split("\n")[2];
}
assert.sameValue(line, "    at Object.hasOwnProperty (<anonymous>)");
const throwingTrap = new Proxy({}, {
	getOwnPropertyDescriptor() {
		throw new Error("m");
	},
});
line = undefined;
try {
	throwingTrap.hasOwnProperty("x");
} catch (e) {
	line = e.stack.split("\n")[2];
}
assert.sameValue(line, "    at Object.hasOwnProperty (<anonymous>)");
Number.prototype.own = Object.prototype.hasOwnProperty;
assert.sameValue((1).own("toString"), false);
delete Number.prototype.own;

assert.sameValue(Array.isArray(new Proxy([], {})), true);
const revocable = Proxy.revocable([], {});
revocable.revoke();
let caught;
try {
	Array.isArray(revocable.proxy);
} catch (e) {
	caught = e;
}
assert.sameValue(caught instanceof TypeError, true);

assert.sameValue([1, NaN].indexOf(NaN), -1);
assert.sameValue([1, NaN].includes(NaN), true);
Array.prototype[1] = 9;
assert.sameValue([1, , 3].indexOf(9), 1);
assert.sameValue([1, , 3].includes(9), true);
delete Array.prototype[1];
const withGetter = [1, 2, 3];
Object.defineProperty(withGetter, 1, {
	get() {
		return 7;
	},
});
assert.sameValue(withGetter.indexOf(7), 1);
const like = { length: 2, 0: 5, 1: 6, indexOf: Array.prototype.indexOf, includes: Array.prototype.includes };
assert.sameValue(like.indexOf(6), 1);
assert.sameValue(like.includes(5), true);
