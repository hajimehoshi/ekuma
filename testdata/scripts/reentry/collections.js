// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Map and Set methods see a collection that user code changes"
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

check("Map from entries with a getter that reads the Map", "[[0,0],[1,1]]", function () {
	let made;
	class M extends Map {
		set(k, v) { made = this; return super.set(k, v); }
	}
	const entry = [1, 1];
	Object.defineProperty(entry, 1, { get() { return made ? made.size : -1; } });
	return [...new M([[0, 0], entry])];
});
check("Map.prototype.forEach with a callback that clears the Map", "[1,3]", function () {
	const m = new Map([[1, 1], [2, 2]]);
	const keys = [];
	m.forEach((v, k) => {
		keys.push(k);
		if (k === 1) {
			m.clear();
			m.set(3, 3);
		}
	});
	return keys;
});
check("getOrInsertComputed with a callback that inserts the key", "6", function () {
	const m = new Map();
	m.getOrInsertComputed(1, () => { m.set(1, 5); return 6; });
	return m.get(1);
});
check("intersection with a method that changes the Set", "[1,3,9]", function () {
	const s = new Set([1, 2, 3]);
	return [...s.intersection({ size: 9, has() { s.delete(2); s.add(9); return true; }, keys() {} })];
});
check("difference with a method that clears the Set", "[2,3]", function () {
	const s = new Set([1, 2, 3]);
	return [...s.difference({ size: 9, has(x) { s.clear(); return x === 1; }, keys() {} })];
});
check("isSubsetOf with a method that adds to the Set", "[true,[1,2,3,4,5,6,7]]", function () {
	const s = new Set([1, 2]);
	const subset = s.isSubsetOf({ size: 9, has(x) { if (x < 6) s.add(x + 2); return true; }, keys() {} });
	return [subset, [...s]];
});
check("symmetricDifference with a method that deletes from the Set", "[2,3,1,4]", function () {
	const s = new Set([1, 2, 3]);
	return [...s.symmetricDifference({
		size: 1,
		has() {},
		keys() { s.delete(1); return [1, 4, 4][Symbol.iterator](); },
	})];
});
check("union with a getter and a method that add to the Set", "[1,5,6,7]", function () {
	const s = new Set([1]);
	return [...s.union({
		get size() { s.add(5); return 1; },
		has() {},
		keys() { s.add(6); return [7][Symbol.iterator](); },
	})];
});
