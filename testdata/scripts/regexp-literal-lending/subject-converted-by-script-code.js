// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "subject converted by script code"
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
var s = new String("ab");
s.toString = function () { patchExec(); return "ab"; };
for (var i = 0; i < 2; i++) { s.replace(/b/g, "x"); s.match(/b/); }
assert.sameValue(String(check()), "4 true");
