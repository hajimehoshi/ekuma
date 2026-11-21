// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "objects that lose properties to delete read, write and list the others correctly, at sites that saw other objects before"
---*/

function read(o) {
	return [o.a, o.b, o.c, o.d].join();
}
function write(o, v) {
	o.c = v;
}

for (let round = 0; round < 3; round++) {
	const o = { a: 1, b: 2, c: 3, d: 4 };
	assert.sameValue(read(o), "1,2,3,4");
	delete o.b;
	assert.sameValue(read(o), "1,,3,4");
	write(o, 30);
	assert.sameValue(read(o), "1,,30,4");
	delete o.d;
	assert.sameValue(read(o), "1,,30,");
	o.b = 5;
	assert.sameValue(Object.keys(o).join(), "a,c,b");
	assert.sameValue(read(o), "1,5,30,");
}

// Another property deleted from an object like the ones above.
const other = { a: 1, b: 2, c: 3, d: 4 };
delete other.c;
assert.sameValue(read(other), "1,2,,4");
assert.sameValue(Object.keys(other).join(), "a,b,d");

// An object made with the properties that remain after a deletion.
assert.sameValue(read({ a: 7, c: 8 }), "7,,8,");

const accessor = {
	x: 1,
	get y() {
		return this.x + 1;
	},
	z: 3,
};
delete accessor.x;
assert.sameValue(accessor.y, NaN);
assert.sameValue(accessor.z, 3);
