// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Local ASCII appends and prepends keep byte order before, at, and after a builder's capacity"
---*/

const seed = "0123456789abcdefghijklmnopqrstuvwxyz";

function grow(word, short, exact, long) {
	let tail = seed;
	let head = seed;
	for (let i = 0; i < 4; i++) {
		tail += "x";
		head = "x" + head;
	}
	tail += "12\0abcde";
	head = "12\0abcde" + head;
	tail += word;
	head = word + head;
	tail += short;
	head = short + head;
	// Only three bytes of the initial 64-byte buffer remain.
	tail += exact;
	head = exact + head;
	const length = tail.length;
	tail += "y";
	head = "y" + head;
	tail += long;
	head = long + head;
	return [tail, head, length];
}

const long = "longer ASCII";
const [tail, head, length] = grow("fghijklm", "nopqr", "stu", long);
assert.sameValue(length, 64);
assert.sameValue(tail, seed + "xxxx12\0abcdefghijklmnopqrstuy" + long);
assert.sameValue(head, long + "ystunopqrfghijklm12\0abcdexxxx" + seed);
