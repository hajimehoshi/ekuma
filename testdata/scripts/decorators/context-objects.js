// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "context objects"
flags: [noStrict]
---*/

var out = [], s = Symbol("s");
function dec(value, ctx) {
	out.push(Object.keys(ctx).join("/") + ":" + ctx.kind + ":" + String(ctx.name) + ":" + ctx.static + ":" + ctx.private +
		":" + Object.keys(ctx.access ?? {}).join("/"));
}
@dec class C {
	@dec static #m() {}
	@dec get g() {}
	@dec set [s](v) {}
	@dec accessor #a;
	@dec 1 = 0;
}
assert.sameValue(String(out.join(" ")), "kind/access/static/private/name/addInitializer/metadata:method:#m:true:true:get/has kind/access/static/private/name/addInitializer/metadata:getter:g:false:false:get/has kind/access/static/private/name/addInitializer/metadata:setter:Symbol(s):false:false:set/has kind/access/static/private/name/addInitializer/metadata:accessor:#a:false:true:get/set/has kind/access/static/private/name/addInitializer/metadata:field:1:false:false:get/set/has kind/name/addInitializer/metadata:class:C:undefined:undefined:");
