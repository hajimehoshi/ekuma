// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Numeric loads and loop steps preserve the order and results of their consumers"
---*/

var numericLoad = 1;

function total(o, a, typed, text, count) {
	let sum = 0;
	for (let i = 0; i < count; ++i) {
		sum += numericLoad;
		sum += o.x;
		sum += text.length;
		sum += a[i];
		sum += typed[0];
		sum += i * o.x;
		sum = i * 2 + sum;
	}
	for (const value of a) {
		sum += value;
	}
	return sum;
}

const numbers = [2, 3, 4];
const typed = new Int16Array([5]);
const object = {x: 2};
assert.sameValue(total(object, numbers, typed, "abcdefgh".slice(0, 4), 3), 66);
assert.sameValue(total(object, numbers, typed, "\u00e9\ud83d\ude00".slice(0), 3), 63);

function productAndAdd(a, b) {
	let sum = -1;
	sum = a * b + sum;
	return sum;
}
assert.sameValue(productAndAdd(1 + 2 ** -27, 1 - 2 ** -27), 0);

function keepLoadedValue(a) {
	let sum = 5;
	const value = a[0];
	sum += value;
	return sum * 10 + value;
}
assert.sameValue(keepLoadedValue(numbers), 72);

function addEither(flag, o) {
	let sum = 10;
	sum += flag ? 3 : o.x;
	return sum;
}
assert.sameValue(addEither(false, object), 12);
assert.sameValue(addEither(true, object), 13);

function sumProperty(o) {
	let sum = 0;
	for (let i = 0; i < 3; i++) sum += o.x;
	return sum;
}
assert.sameValue(sumProperty(object), 6);
object.x = "x";
assert.sameValue(sumProperty(object), "0xxx");

function steps(limit) {
	let i = 3;
	let count = 0;
	do {
		count++;
	} while (--i > limit);
	return count * 10 + i;
}
assert.sameValue(steps(0), 30);
assert.sameValue(steps(NaN), 12);
