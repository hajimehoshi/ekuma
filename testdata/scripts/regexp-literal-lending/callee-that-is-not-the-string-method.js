// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "callee that is not the String method"
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
function f(x) { return x.replace(/b/g, "x"); }
		var o = {replace(re) { return re; }};
		var a = f(o);
		a.lastIndex = 7;
		var r = [f("abab"), f("abab")];
		seen.push(a, f(o));
		assert.sameValue(String(r.join() + " " + a.lastIndex + " " + seen[1].lastIndex + " " + check()), "axax,axax 7 0 2 true");
