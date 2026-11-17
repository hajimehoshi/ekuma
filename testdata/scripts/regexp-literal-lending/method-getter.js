// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "method getter"
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
var test = RegExp.prototype.test;
		Object.defineProperty(RegExp.prototype, "test", {get() { seen.push(this); return test; }});
		var r = [];
		for (var i = 0; i < 2; i++) r.push(/b/.test("ab"));
		assert.sameValue(String(r.join() + " " + check()), "true,true 2 true");
