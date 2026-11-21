// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Map and Set methods called as methods of other receivers throw a TypeError"
---*/

function throwsTypeError(f) {
	try {
		f();
	} catch (e) {
		return e instanceof TypeError;
	}
	return false;
}

const map = new Map([[1, 2]]);
const set = new Set([1]);

// A Set that borrows the methods of Map, and an object that inherits them from a Map.
set.mapGet = Map.prototype.get;
set.mapHas = Map.prototype.has;
set.mapSet = Map.prototype.set;
set.mapDelete = Map.prototype.delete;
assert.sameValue(throwsTypeError(() => set.mapGet(1)), true);
assert.sameValue(throwsTypeError(() => set.mapHas(1)), true);
assert.sameValue(throwsTypeError(() => set.mapSet(1, 2)), true);
assert.sameValue(throwsTypeError(() => set.mapDelete(1)), true);
const fromMap = Object.create(map);
assert.sameValue(throwsTypeError(() => fromMap.get(1)), true);
assert.sameValue(throwsTypeError(() => fromMap.size), true);

// A Map that borrows the methods of Set, and an object that inherits them from a Set.
map.setAdd = Set.prototype.add;
map.setHas = Set.prototype.has;
map.setDelete = Set.prototype.delete;
assert.sameValue(throwsTypeError(() => map.setAdd(1)), true);
assert.sameValue(throwsTypeError(() => map.setHas(1)), true);
assert.sameValue(throwsTypeError(() => map.setDelete(1)), true);
const fromSet = Object.create(set);
assert.sameValue(throwsTypeError(() => fromSet.add(1)), true);
assert.sameValue(throwsTypeError(() => fromSet.size), true);
