// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "replacer"
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
function f(s, depth) {
			return s.replace(/(b)/g, function (m, p1, pos) {
				"use strict";
				var inner = depth > 0 ? f("abab", depth - 1) : "";
				return "[" + (this === undefined) + arguments.length + pos + inner + "]";
			});
		}
		assert.sameValue(String(f("abab", 1)), "a[true41a[true41]a[true43]]a[true43a[true41]a[true43]]");
