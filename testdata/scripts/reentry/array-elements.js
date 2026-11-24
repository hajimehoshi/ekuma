// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Array algorithms see an array that a getter or a callback changes"
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

check("concat with a getter that lengthens both arrays", "[1,2,3,4,5]", function () {
	const a = [1, 2];
	const b = [3, 4];
	Object.defineProperty(b, Symbol.isConcatSpreadable, {
		get() { a.push(9); b.push(5); return true; },
	});
	return a.concat(b);
});
check("concat into an array that the arguments reach", "[[1,2,3],3]", function () {
	const held = [];
	class A extends Array {
		static get [Symbol.species]() { return function () { return held; }; }
	}
	const b = [3];
	Object.defineProperty(b, Symbol.isConcatSpreadable, {
		get() { held.push("held"); return true; },
	});
	return [A.from([1, 2]).concat(b), held.length];
});
check("concat of the result with itself", "[1,2,1,2,1,2,1,2]", function () {
	const held = [];
	class A extends Array {
		static get [Symbol.species]() { return function () { return held; }; }
	}
	return A.from([1, 2]).concat(held, held);
});
check("sort with a comparator that empties the array", "[[1,2,3],3]", function () {
	const a = [3, 1, 2];
	a.sort((x, y) => { a.length = 0; return x - y; });
	return [a, a.length];
});
check("sort with a conversion that lengthens the array", "3", function () {
	const a = [{ toString() { a.push(1); return "b"; } }, { toString() { return "a"; } }];
	a.sort();
	return a.length;
});
check("toSorted with a comparator that empties the array", '[1,3,"undefined"]', function () {
	const a = [3, , 1];
	return a.toSorted((x, y) => { a.length = 0; return x - y; });
});
check("flat with a getter that lengthens the array", "[3,3]", function () {
	const a = [[1, 2], 3];
	Object.defineProperty(a, 0, { get() { a.push(8); return [a.length]; } });
	return a.flat();
});
check("reduce with a callback that shortens the array", "3", function () {
	const a = [1, 2, 3, 4];
	return a.reduce((sum, x) => { a.length = 2; return sum + x; });
});
check("map with a callback that defines an accessor", '["g","g","g"]', function () {
	const a = [1, 2, 3];
	return a.map((x, i) => {
		if (i === 0) {
			Object.defineProperty(a, 1, { get() { return "g"; } });
		}
		return a[1];
	});
});
check("unshift over an accessor", "4", function () {
	const a = [1, 2, 3];
	Object.defineProperty(a, 1, { get() { a.length = 2; return "g"; }, set() {}, configurable: true });
	a.unshift(0);
	return a.length;
});
check("spread with a getter that shortens the array", '[1,"g"]', function () {
	const a = [1, 2, 3];
	Object.defineProperty(a, 1, { get() { a.length = 2; return "g"; }, configurable: true });
	return [...a];
});
check("destructuring with a getter that shortens the array", '[0,"undefined",[]]', function () {
	const a = [1, 2, 3];
	Object.defineProperty(a, 0, { get() { a.length = 1; return 0; } });
	const [x, y, ...z] = a;
	return [x, y, z];
});
check("for-of with a replaced next", "[1,2,3]", function () {
	const proto = Object.getPrototypeOf([][Symbol.iterator]());
	const next = proto.next;
	const seen = [];
	try {
		for (const x of [1, 2, 3]) {
			seen.push(x);
			proto.next = function () { return { done: true }; };
		}
		for (const x of [1]) {
			seen.push(x);
		}
	} finally {
		proto.next = next;
	}
	return seen;
});
check("next of an array iterator during its length getter", '[{"value":"a","done":false},{"value":"b","done":false},{"value":"undefined","done":true}]', function () {
	let reads = 0;
	const it = Array.prototype.values.call({
		get length() {
			if (reads++ === 0) {
				it.next();
			}
			return 2;
		},
		0: "a",
		1: "b",
	});
	return [it.next(), it.next(), it.next()];
});
check("spread of an array that a later argument and the constructor change", "[1,2,4]", function () {
	const a = [1, 2];
	class B {
		constructor(...args) { this.args = args; }
	}
	const P = new Proxy(B, {
		get(target, key) {
			if (key === "prototype") {
				a.push(9);
			}
			return target[key];
		},
	});
	return new P(...a, (a.push(3), 4)).args;
});
