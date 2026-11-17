// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "sticky and global literals in loops"
flags: [noStrict]
---*/

var seen = [];
function check() {
	return new Set(seen).size + " " + seen.every(function (r) { return r instanceof RegExp; });
}
var exec = RegExp.prototype.exec;
// patchExec makes the next call of exec record its RegExp object.
function patchExec() {
	RegExp.prototype.exec = function (s) { seen.push(this); RegExp.prototype.exec = exec; return exec.call(this, s); };
}
var r = [];
for (var i = 0; i < 3; i++) r.push("aaa".replace(/a/y, "b"), "aXaX".match(/X/g).length, "aXa".search(/a/y), "aXa".split(/a/y).length);
assert.sameValue(String(r.join()), "baa,2,0,3,baa,2,0,3,baa,2,0,3");
