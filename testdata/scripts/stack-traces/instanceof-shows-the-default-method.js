// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "The default @@hasInstance method shows a frame where instanceof throws or runs code"
flags: [noStrict]
---*/

let seen;
function frames(error) {
	return error.stack.split("\n").filter((line) => line.includes("[Symbol.hasInstance]")).join("\n");
}
function observer() {
	seen = observer.caller + frames(new Error());
	return null;
}
function outcome(f) {
	seen = undefined;
	try {
		f();
	} catch (error) {
		assert.sameValue(error.constructor, TypeError);
		return frames(error);
	}
	return seen;
}

function C() {}
const getter = () => 0;
Object.defineProperty(getter, "prototype", { get: observer });
function bad() {}
bad.prototype = 1;
const proxied = new Proxy(C, {
	get(target, key) {
		return key === "prototype" ? observer() : target[key];
	},
});
const object = {};
const proxy = new Proxy({}, { getPrototypeOf: observer });
const inheritsProxy = Object.create(proxy);

for (const [v, f] of [[proxy, C], [inheritsProxy, C], [object, getter], [object, bad], [object, proxied]]) {
	const want = outcome(() => Function.prototype[Symbol.hasInstance].call(f, v));
	assert.sameValue(want === "" || want === undefined, false);
	// The second evaluation at the site finds its cache filled.
	for (let i = 0; i < 2; i++) {
		assert.sameValue(outcome(() => v instanceof f), want);
	}
	// A bound function shows the method once more.
	const bound = f.bind();
	assert.sameValue(
		outcome(() => v instanceof bound),
		outcome(() => Function.prototype[Symbol.hasInstance].call(bound, v)));
}

// A method of the target of a bound function runs inside the method of the bound function.
class D {
	static [Symbol.hasInstance]() {
		observer();
		return true;
	}
}
const boundD = D.bind();
assert.sameValue(outcome(() => object instanceof boundD), outcome(() => Function.prototype[Symbol.hasInstance].call(boundD, object)));
assert.sameValue(outcome(() => object instanceof boundD) === outcome(() => object instanceof D), false);

// A missing method falls back to OrdinaryHasInstance without a call.
const fallback = () => {};
Object.defineProperty(fallback, Symbol.hasInstance, {value: null});
assert.sameValue(outcome(() => object instanceof fallback), "");

// The target still calls its own method when the bound function has none.
Object.defineProperty(boundD, Symbol.hasInstance, {value: undefined});
assert.sameValue(outcome(() => object instanceof boundD), outcome(() => object instanceof D));

// The frames end with the operation.
assert.sameValue(frames(new Error()), "");
