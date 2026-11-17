// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "function own keys after a store cached for an object with the new prototype"
flags: [noStrict]
---*/

function set(o) { o.x = 1; }
function f() {}
Object.setPrototypeOf(f, null);
set(Object.create(null)); set(Object.create(null)); set(f);
assert.sameValue(String(Object.getOwnPropertyNames(f).join()), "length,name,arguments,caller,prototype,x");
