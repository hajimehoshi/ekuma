// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "exec on a string that is not ASCII returns its captures and their code unit indices"
---*/

var u = "the quick brown ᶠᵒˣ jumped over the lazy ᵈᵒᵍ";
var m = /the quick brown ᶠᵒˣ/.exec(u);
assert.sameValue(m[0], "the quick brown ᶠᵒˣ");
assert.sameValue(m.index, 0);

m = /(ᵒ)(z)?(ˣ)/.exec(u);
assert.sameValue([m[0], m[1], m[2], m[3], m.index].join("|"), "ᵒˣ|ᵒ||ˣ|17");
assert.sameValue(m[2], undefined);

m = /(ᵍ)()$/.exec(u);
assert.sameValue([m[1], m[2], m[2].length, m.index].join("|"), "ᵍ||0|43");

m = /[^]*/.exec(u);
assert.sameValue(m[0], u);

// Code unit indices count a supplementary character as two.
var t = "ab😀cd😀efghij";
m = /(c)(d😀e)/.exec(t);
assert.sameValue([m[1], m[2], m.index].join("|"), "c|d😀e|4");

var g = /ᵒ/g;
assert.sameValue(g.exec(u).index, 17);
assert.sameValue(g.lastIndex, 18);
assert.sameValue(g.exec(u).index, 42);
assert.sameValue(g.lastIndex, 43);
assert.sameValue(g.exec(u), null);
assert.sameValue(g.lastIndex, 0);
