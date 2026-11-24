// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Object operations see an object that user code changes"
---*/

// check compares what f returns, as JSON with "undefined" for undefined, or the name of the error
// that it throws.
function check(name, expected, f) {
	let actual;
	try {
		actual = JSON.stringify(f(), (key, value) => value === undefined ? "undefined" : value);
	} catch (e) {
		actual = e.name;
	}
	assert.sameValue(actual, expected, name);
}

check("Object.assign", '{"a":1,"b":2}', function () {
	const o = { a: 1, get b() { delete o.c; o.d = 5; return 2; }, c: 3 };
	return Object.assign({}, o);
});
check("object spread", '{"a":1,"b":2}', function () {
	const o = { a: 1, get b() { delete o.c; o.d = 5; return 2; }, c: 3 };
	return { ...o };
});
check("Object.entries", '[["a",1],["b",2]]', function () {
	const o = {
		a: 1,
		get b() { delete o.c; Object.defineProperty(o, "e", { enumerable: false }); return 2; },
		c: 3,
		e: 4,
	};
	return Object.entries(o);
});
check("Object.defineProperties", '["a","c"]', function () {
	const descriptors = { get a() { delete descriptors.b; return { value: 1 }; }, b: { value: 2 }, c: { value: 3 } };
	const o = {};
	Object.defineProperties(o, descriptors);
	return Object.getOwnPropertyNames(o);
});
check("for-in", '["a","c"]', function () {
	const o = { a: 1, b: 2, c: 3 };
	const keys = [];
	for (const key in o) {
		keys.push(key);
		delete o.b;
		o.z = 1;
	}
	return keys;
});
check("JSON.stringify of an object", '{"a":1,"b":2,"d":"1"}', function () {
	const o = { a: 1, get b() { delete o.c; return 2; }, c: 3, d: { toJSON() { o.e = 9; return JSON.stringify(o.a); } } };
	return JSON.parse(JSON.stringify(o));
});
check("JSON.stringify of an array", "[1,0,null]", function () {
	const a = [1, { toJSON() { a.length = 2; return 0; } }, 3];
	return JSON.parse(JSON.stringify(a));
});
check("Reflect.set with a receiver that becomes read-only", "[false,1]", function () {
	const o = {};
	const receiver = new Proxy(o, {
		getOwnPropertyDescriptor(target, key) {
			Object.defineProperty(o, key, { value: 1, writable: false, configurable: true });
			return Reflect.getOwnPropertyDescriptor(target, key);
		},
	});
	return [Reflect.set({}, "x", 5, receiver), o.x];
});
check("instanceof with a getter that defines Symbol.hasInstance", "[false,true,true]", function () {
	const f = () => {};
	const proto = {};
	Object.defineProperty(Function.prototype, "prototype", {
		get() {
			Object.defineProperty(f, "prototype", { value: proto });
			Object.defineProperty(f, Symbol.hasInstance, { value: () => true });
			return proto;
		},
		configurable: true,
	});
	try {
		const results = [];
		for (let i = 0; i < 3; i++) {
			results.push({} instanceof f);
		}
		return results;
	} finally {
		delete Function.prototype.prototype;
	}
});
check("setUTCHours with a conversion that sets the time", "3720000", function () {
	const d = new Date(0);
	return d.setUTCHours({ valueOf() { d.setTime(86400000); return 1; } }, { valueOf() { return 2; } });
});
// HasBinding and GetBindingValue each call HasProperty.
check("typeof in a with scope whose Symbol.unscopables getter deletes the binding", '["undefined",["x","x"]]', function () {
	const log = [];
	const scope = new Proxy({ x: 1 }, {
		has(target, key) { log.push(key); return key === "x" && key in target; },
		get(target, key) {
			if (key === Symbol.unscopables) {
				delete target.x;
				return undefined;
			}
			return target[key];
		},
	});
	return [Function("scope", "with (scope) { return typeof x; }")(scope), log];
});
