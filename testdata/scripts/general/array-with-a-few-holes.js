// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "array with a few holes"
flags: [noStrict]
---*/

var a = []; a[40] = 40; for (var i = 0; i < 40; i++) if (i % 10 !== 5) a[i] = i;
var n = 0; a.forEach(function () { n++; });
assert.sameValue(String([a.length, 5 in a, 6 in a, a[40], n, Object.keys(a).slice(4, 7).join(), a.indexOf(undefined), a.includes(undefined),
	a.findIndex(function (v) { return v === undefined; })].join("|")), "41|false|true|40|37|4,6,7|-1|true|5");
