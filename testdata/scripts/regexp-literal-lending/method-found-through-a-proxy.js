// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "method found through a proxy"
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
delete RegExp.prototype.exec;
Object.setPrototypeOf(RegExp.prototype, new Proxy({}, {get(t, k, r) { seen.push(r); return function () { return 1; }; }}));
var r = [];
for (var i = 0; i < 2; i++) r.push(/b/.exec("ab"));
assert.sameValue(String(r.join() + " " + check()), "1,1 2 true");
