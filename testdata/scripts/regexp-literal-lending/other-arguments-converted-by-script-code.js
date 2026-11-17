// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "other arguments converted by script code"
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
var v = {toString() { patchExec(); return "x"; }, valueOf() { patchExec(); return 5; }};
		var r = [];
		for (var i = 0; i < 2; i++) r.push("ab".replace(/b/g, v), "a,b".split(/,/, v));
		assert.sameValue(String(r.join(" ") + " " + check()), "ax a,b ax a,b 4 true");
