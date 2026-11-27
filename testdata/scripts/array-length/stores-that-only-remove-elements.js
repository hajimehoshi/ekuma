// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Stores of number lengths no greater than the length of a plain array remove the elements from that index on, on every iteration"
---*/

function setLength(a, n) {
	a.length = n;
}

for (var round = 0; round < 3; round++) {
	// The literal shares its elements with the constants of the function.
	var a = [0, 1, 2, 3, 4];
	setLength(a, 3);
	assert.sameValue(a.length, 3);
	assert.sameValue(a.join(), "0,1,2");
	assert.sameValue(3 in a, false);
	setLength(a, 3);
	assert.sameValue(a.join(), "0,1,2");
	setLength(a, -0);
	assert.sameValue(a.length, 0);
	a.push(5);
	assert.sameValue(a.join(), "5");

	// Holes past the new length go, and the elements of prototypes show through the others.
	Object.prototype[3] = "proto";
	var holes = [0, , 2, , 4];
	setLength(holes, 4);
	assert.sameValue(holes.length, 4);
	assert.sameValue(holes.hasOwnProperty(3), false);
	assert.sameValue(holes[3], "proto");
	setLength(holes, 2);
	assert.sameValue(holes[1], undefined);
	assert.sameValue(Object.keys(holes).join(), "0");
	delete Object.prototype[3];

	// A copy shares its elements with the original until it changes.
	var original = [];
	for (var i = 0; i < 100; i++) {
		original[i] = i;
	}
	var copy = original.slice();
	for (var i = 100; i-- > 0;) {
		copy.length = i;
	}
	assert.sameValue(copy.length, 0);
	assert.sameValue(original.length, 100);
	assert.sameValue(original[99], 99);

	// A store to another name adds a property.
	var other = [0, 1, 2];
	other.x = 1;
	assert.sameValue(other.length, 3);
	assert.sameValue(other.x, 1);

	var nonExtensible = Object.preventExtensions([0, 1, 2]);
	setLength(nonExtensible, 1);
	assert.sameValue(nonExtensible.join(), "0");

	// A length that only grew has no elements to remove.
	var grown = [0, 1];
	grown.length = 2 ** 32 - 1;
	setLength(grown, 1);
	assert.sameValue(grown.length, 1);
	assert.sameValue(grown[0], 0);
}
