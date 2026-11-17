// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "keys are strings"
flags: [noStrict]
---*/

var a = [];
for (var i = 0; i < 1100; i++) a.push(i);
var n = 0, bad = 0;
for (var round = 0; round < 2; round++) {
	for (var k in a) {
		n++;
		if (typeof k !== "string" || k !== "" + a[k]) bad++;
	}
}
var o = {7: 0, 2000: 0};
var cat = [];
for (var k in o) cat.push(k + 1);
assert.sameValue(String([n, bad, cat.join()].join()), "2200,0,71,20001");
