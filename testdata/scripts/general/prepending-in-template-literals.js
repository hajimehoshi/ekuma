// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "prepending in template literals"
flags: [noStrict]
---*/

var out = "";
for (var i = 0; i < 50; i++) out = `${i % 10}-${out}`;
var mid = `<${out}>`;
var dbl = `${out}|${out}`;
var same = `${''}${out}`;
assert.sameValue(String([out.length, out.slice(0, 6), mid.slice(0, 4), mid.slice(-3), dbl.length, same === out,
	out === Array.from({length: 50}, (_, i) => (49 - i) % 10 + "-").join("")].join(" ")), "100 9-8-7- \u003c9-8 0-\u003e 201 true true");
