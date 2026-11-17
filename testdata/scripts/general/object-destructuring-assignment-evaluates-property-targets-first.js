// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "object destructuring assignment evaluates property targets first"
flags: [noStrict]
---*/

var log = [], o = {};
var src = { get a() { log.push("get a"); }, get b() { log.push("get b"); return 2; } };
({ a: (log.push("target a"), o).x = (log.push("default"), 1), ...(log.push("target rest"), o).r } = src);
assert.sameValue(String(log.join() + "|" + o.x + o.r.b), "target a,get a,default,target rest,get b|12");
