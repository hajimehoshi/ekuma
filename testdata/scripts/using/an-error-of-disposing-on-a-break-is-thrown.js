// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "an error of disposing on a break is thrown"
flags: [noStrict]
---*/

var log = [];
function res(name, error) {
	return { [Symbol.dispose]() { log.push(name); if (error) throw error; } };
}
var r;
		try { for (;;) { using a = res("a", "boom"); break; } } catch (e) { r = e; }
		log.push(r);
		assert.sameValue(String(log.join()), "a,boom");
