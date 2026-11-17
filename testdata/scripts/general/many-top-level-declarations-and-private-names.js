// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "many top-level declarations and private names"
flags: [noStrict]
---*/

var v0, v1, v2, v3, v4, v5, v6, v7, v8, v9;var v0, v1, v2, v3, v4, v5, v6, v7, v8, v9;var w0, w1, w2, w3, w4, w5, w6, w7, w8, w9;
let lexA = 1; class K {} { function annexF() {} }
var r = [lexA, "lexA" in globalThis, typeof K, typeof annexF];
(function () {
	"use strict";
	w0 = String(1);
	try { undeclaredU = String(1); } catch (e) { r.push(e.constructor.name); }
})();
var names = Array.from({length: 20}, (_, i) => "#p" + i + " = " + i + ";").join("");
r.push(eval("new (class { " + names + " get #q() { return 1; } set #q(v) {} m() { return this.#p19 + this.#q; } })().m()"));
try { eval("(class { " + names + " #p3; })"); } catch (e) { r.push(e.constructor.name); }
assert.sameValue(String(r.join()), "1,false,function,function,ReferenceError,20,SyntaxError");
