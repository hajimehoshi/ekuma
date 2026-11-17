// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "sparse array walk sees changes made by the callback"
flags: [noStrict]
---*/

var a = []; a[10] = 1; a[100000] = 2; var seen = [], back = [];
a.forEach(function (v, i) { seen.push(i); if (i === 10) { a[50000] = 3; Array.prototype[70000] = 4; } });
delete Array.prototype[70000];
a.reduceRight(function (acc, v, i) { back.push(i); if (i === 100000) a[20] = 5; return acc; }, 0);
assert.sameValue(String([seen.join(), back.join(), a.lastIndexOf(1)].join("|")), "10,50000,70000,100000|100000,50000,20,10|10");
