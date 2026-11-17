// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "constructor storage grows beyond its hint and survives a thrown construction"
flags: [noStrict]
---*/

let escaped;
function C(n, fail) {
	for (let i = 0; i < n; i++) this["p" + i] = i;
	if (fail) { escaped = this; throw 1; }
}
function make(n, fail) { return new C(n, fail); }
make(4, false);
let large = make(40, false);
try { make(20, true); } catch (_) {}
let empty = make(0, false), small = make(1, false);
small.p0 = 100;
assert.sameValue(String([Object.keys(large).length, large.p39, Object.keys(escaped).length,
	escaped.p0, Object.keys(empty).length, small.p0].join()), "40,39,20,0,0,100");
