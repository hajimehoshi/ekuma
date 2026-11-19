// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "locals of eval code that a closure assigns while they are in use"
---*/

var indirect = eval;
var sources = [
	["let o = {}; const orig = o; let f = () => { o = {}; }; o.x = (f(), 1); orig.x", 1],
	["let o = {v: 1}; const orig = o; let f = () => { o = {v: 10}; }; o.v += (f(), 1); orig.v", 2],
	["let o = {}; let k = 'a'; let f = () => { k = 'b'; }; o[k] = (f(), 1); Object.keys(o).join()", "a"],
	["let o; const f = () => { o = {y: 10}; }; o = {get y() { f(); return 1; }, set y(v) { this.z = v; }}; const orig = o; o.y++; orig.z", 2],
	["let b = 1; let g = () => { b = 10; return 0; }; b + g()", 1],
];
for (var [src, want] of sources) {
	assert.sameValue(eval(src), want, "direct: " + src);
	assert.sameValue(indirect(src), want, "indirect: " + src);
}
