// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "assigned exec"
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
RegExp.prototype.exec = function (s) { seen.push(this); return null; };
for (var i = 0; i < 2; i++) {
	"ab".replace(/b/g, "x"); "ab".replace(/b/, function () { return "x"; });
	"ab".match(/b/); "ab".search(/b/);
}
assert.sameValue(String(check()), "8 true");
