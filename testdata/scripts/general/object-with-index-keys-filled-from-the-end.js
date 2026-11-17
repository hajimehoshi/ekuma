// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "object with index keys filled from the end"
flags: [noStrict]
---*/

var o = { first: 1 }; for (var i = 99; i >= 0; i--) { o[i] = i; if (i === 50) o.mid = 2; }
var k = Object.keys(o);
assert.sameValue(String([k.length, k[0], k[99], k[100], k[101], o[75], JSON.stringify(o).slice(-26)].join("|")), "102|0|99|first|mid|75|\"99\":99,\"first\":1,\"mid\":2}");
