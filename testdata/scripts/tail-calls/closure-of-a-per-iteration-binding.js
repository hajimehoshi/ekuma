// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "closure of a per-iteration binding"
flags: [onlyStrict]
---*/

var fns = [];
function loop() {
	for (let i = 0; i < 3; i++) {
		fns.push(() => i);
		if (i === 1) return done(7, 8, 9);
	}
}
function done(a, b, c) { var x = a, y = b, z = c; return fns.map(f => f()).join() + x + y + z; }
assert.sameValue(String(loop()), "0,1789");
