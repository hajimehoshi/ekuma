// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "receiver used by script code"
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
var s = {toString() { patchExec(); return "ab"; }};
		var r = [];
		for (var i = 0; i < 2; i++) r.push(/b/.test(s), /b/.test((patchExec(), "ab")));
		assert.sameValue(String(r.join() + " " + check()), "true,true,true,true 4 true");
