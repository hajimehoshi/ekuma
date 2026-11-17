// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "properties"
flags: [noStrict]
---*/

// The context gets metadata with CreateDataPropertyOrThrow after addInitializer,
// and the class gets it as a writable, non-enumerable, configurable property.
var ctx;
@((v, c) => { ctx = c; })
class C {}
var m = C[Symbol.metadata], d = Object.getOwnPropertyDescriptor(C, Symbol.metadata);
var cd = Object.getOwnPropertyDescriptor(ctx, "metadata");
assert.sameValue(String([Object.keys(ctx).join(" "), cd.value === m, cd.writable, cd.enumerable, cd.configurable,
	d.writable, d.enumerable, d.configurable, Object.getPrototypeOf(m), Reflect.ownKeys(m).length].join()), "kind name addInitializer metadata,true,true,true,true,true,false,true,,0");
