// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "numeric arrays retain their indexed properties through storage changes"
---*/

function numbers() {
	const a = [];
	for (let i = 0; i < 20; i++) a[i] = i;
	return a;
}

const a = numbers();
a[0] = -0;
a[1] = NaN;
assert.sameValue(a.includes(NaN), true);
assert.sameValue(a.indexOf(NaN), -1);
assert.sameValue(Object.keys(a).join(), "0,1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19");

const copy = a.slice(0, 3);
copy[2] = 20;
assert.sameValue(a[2], 2);
assert.sameValue(copy[0], -0);

let sum = 0;
for (const value of a.slice(2)) sum += value;
assert.sameValue(sum, 189);
assert.sameValue([...a][1], NaN);
const extended = a.concat(a);
assert.sameValue(extended.length, 40);
assert.sameValue(extended[20], -0);

a.length = 3;
a.length = 6;
assert.sameValue(a.pop(), undefined);
assert.sameValue(a.length, 5);
assert.sameValue(3 in a, false);
a[3] = {x: 1};
assert.sameValue(a[3].x, 1);
assert.sameValue(a[0], -0);

const boxed = [{}, , "tail"];
boxed.length = 2;
boxed.length = 3;
assert.sameValue(2 in boxed, false);

function constants() { return [1, 2, 3]; }
const truncated = constants();
const shared = constants();
truncated.length = 2;
assert.sameValue(shared[2], 3);

const proto = numbers();
const child = Object.create(proto);
assert.sameValue(child[6], 6);
delete proto[6];
assert.sameValue(6 in child, false);
Object.defineProperty(proto, 7, {get() { return this === child; }});
assert.sameValue(child[7], true);

const frozen = Object.freeze(numbers());
assert.sameValue(Reflect.set(frozen, 1, 10), false);
assert.sameValue(frozen[1], 1);

function pushAtSite(array, value) { const n = array.push(value); return n; }
function popAtSite(array) { const value = array.pop(); return value; }
const withInheritedIndex = numbers();
assert.sameValue(pushAtSite(withInheritedIndex, 70), 21);
assert.sameValue(popAtSite(withInheritedIndex), 70);
let stored;
Object.defineProperty(Array.prototype, "20", {
	configurable: true,
	set(value) { stored = value; },
	get() { return 72; },
});
try {
	assert.sameValue(pushAtSite(withInheritedIndex, 71), 21);
	assert.sameValue(stored, 71);
	assert.sameValue(popAtSite(withInheritedIndex), 72);
	assert.sameValue(withInheritedIndex.length, 20);
} finally {
	delete Array.prototype[20];
}
const originalPush = Array.prototype.push;
try {
	Array.prototype.push = function () { return 73; };
	assert.sameValue(pushAtSite(withInheritedIndex, 74), 73);
	assert.sameValue(withInheritedIndex.length, 20);
} finally {
	Array.prototype.push = originalPush;
}

const weakKey = numbers();
const weak = new WeakMap();
weak.set(weakKey, 42);
weakKey[0] = "mixed";
assert.sameValue(weak.get(weakKey), 42);

const species = numbers();
const originalSpecies = Object.getOwnPropertyDescriptor(Array, Symbol.species);
const originalConstructor = Array.prototype.constructor;
assert.sameValue(species.slice(0, 1)[0], 0);
try {
	Object.defineProperty(Array, Symbol.species, {
		configurable: true,
		get() { return class extends Array {}; }
	});
	assert.sameValue(Object.getPrototypeOf(species.slice(0, 1)) === Array.prototype, false);
	Object.defineProperty(Array, Symbol.species, originalSpecies);

	let calls = 0;
	Array.prototype.constructor = {
		[Symbol.species]: function () { calls++; }
	};
	assert.sameValue(species.slice(0, 1)[0], 0);
	assert.sameValue(calls, 1);
} finally {
	Object.defineProperty(Array, Symbol.species, originalSpecies);
	Array.prototype.constructor = originalConstructor;
}
