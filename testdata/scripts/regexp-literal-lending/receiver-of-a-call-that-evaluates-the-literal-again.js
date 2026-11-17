// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "receiver of a call that evaluates the literal again"
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
var out = [];
function f(depth) { out.push(/a/g.test(depth > 0 ? (f(depth - 1), "a") : "a"), /a/y.exec("aa").index); }
f(2);
assert.sameValue(String(out.join()), "true,0,true,0,true,0");
