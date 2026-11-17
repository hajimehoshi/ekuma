// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "assigned String.prototype.replace"
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
String.prototype.replace = function (re) { seen.push(re); return re.lastIndex++; };
var r = [];
for (var i = 0; i < 2; i++) r.push("ab".replace(/b/y, "x"));
assert.sameValue(String(r.join() + " " + check()), "0,0 2 true");
