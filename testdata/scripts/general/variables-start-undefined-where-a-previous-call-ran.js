// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "the variables of a function start undefined where the registers of a previous call were"
---*/

function fill(a, b, c, d, e, f, g, h) { return a; }
function plain(a) { var x; return x; }
var named = function self(a) { var x; return [x, typeof self]; };
function lexical(a) { var x; let y = 1; return [x, y]; }
function usesThis(a) { var x; return [x, this === o]; }
var o = { usesThis: usesThis };
for (var n = 0; n < 3; n++) {
	fill(1, 2, 3, 4, 5, 6, 7, 8);
	assert.sameValue(plain(0), undefined);
	fill(1, 2, 3, 4, 5, 6, 7, 8);
	assert.sameValue(named(0).join(), ",function");
	fill(1, 2, 3, 4, 5, 6, 7, 8);
	assert.sameValue(lexical(0).join(), ",1");
	fill(1, 2, 3, 4, 5, 6, 7, 8);
	assert.sameValue(o.usesThis(0).join(), ",true");
	fill(1, 2, 3, 4, 5, 6, 7, 8);
	assert.sameValue(o.usesThis().join(), ",true");
}
