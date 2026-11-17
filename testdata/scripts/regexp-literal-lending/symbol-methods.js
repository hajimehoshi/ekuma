// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "symbol methods"
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
		[Symbol.replace, Symbol.match, Symbol.search, Symbol.split].forEach(function (k) {
			var f = RegExp.prototype[k];
			Object.defineProperty(RegExp.prototype, k, {get() { seen.push(this); return f; }});
			for (var i = 0; i < 2; i++) r.push("ab".replace(/b/g, "x"), "ab".match(/b/)[0], "ab".search(/b/), "a,b".split(/,/));
			Object.defineProperty(RegExp.prototype, k, {value: f});
			r.push(check());
			seen = [];
		});
		assert.sameValue(String(r.join(" ")), "ax b 1 a,b ax b 1 a,b 2 true ax b 1 a,b ax b 1 a,b 4 true ax b 1 a,b ax b 1 a,b 2 true ax b 1 a,b ax b 1 a,b 2 true");
