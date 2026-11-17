// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "store to length after an array prototype's length is frozen"
flags: [noStrict]
---*/

var a = [1]; function G() {} G.prototype = a;
function set(o) { o.length = 5; }
set(new G());
Object.defineProperty(a, "length", { writable: false });
var g = new G(); set(g);
assert.sameValue(String(Object.getOwnPropertyNames(g).length + "," + g.length), "0,1");
