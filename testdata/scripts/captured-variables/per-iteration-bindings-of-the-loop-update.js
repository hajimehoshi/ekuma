// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "closures and direct evals in the update or test of a for loop see per-iteration bindings"
flags: [noStrict]
---*/

var fs = [];
for (let i = 0; i < 3; i++, fs.push(() => i));
assert.sameValue(fs.map(f => f()).join(), "1,2,3");

fs = [];
for (let i = 0; i < 3; i++, fs.push(class { static get v() { return i; } }));
assert.sameValue(fs.map(c => c.v).join(), "1,2,3");

fs = [];
for (let i = 0; i < 3; i++, fs.push(eval("() => i")));
assert.sameValue(fs.map(f => f()).join(), "1,2,3");

fs = [];
for (let i = 0; fs.push(() => i), i < 2; i++);
assert.sameValue(fs.map(f => f()).join(), "0,1,2");

function* g() {
	for (let i = 0; i < 3; i++) {
		yield i;
	}
}
assert.sameValue([...g()].join(), "0,1,2");
