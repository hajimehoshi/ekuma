// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "freeze of an object with its own shape after a cached store"
flags: [onlyStrict]
---*/

var o = {}, r; for (var i = 0; i < 200; i++) o["k" + i] = i;
function set(v) { o.k5 = v; }
set(1); set(2);
Object.freeze(o);
try { set(3); } catch (e) { r = e.constructor.name; }
assert.sameValue(String([o.k5, r, Object.isFrozen(o), Object.keys(o).length, Object.getOwnPropertyDescriptor(o, "k199").writable].join()), "2,TypeError,true,200,false");
