// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "RegExp search restores lastIndex before reading the index"
flags: [noStrict]
---*/

var log = [], n = 0;
var o = {
	get lastIndex() { log.push("get"); return n++; },
	set lastIndex(v) { log.push("set"); },
	exec() { log.push("exec"); return { get index() { log.push("index"); return 3; } }; },
};
assert.sameValue(String(RegExp.prototype[Symbol.search].call(o, "x") + ":" + log.join()), "3:get,exec,get,set,index");
