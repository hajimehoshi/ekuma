// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "immutable element store"
flags: [noStrict]
---*/

// A store into an immutable buffer fails before the value is converted,
// whether or not the index is valid and whatever the receiver is.
var ta = new Uint8Array(new Uint8Array([1, 2]).buffer.transferToImmutable()), r = [], calls = 0;
var v = {valueOf() { calls++; return 9; }};
for (var i = 0; i < 3; i++) { ta[0] = 7; ta[1] = v; }
r.push(ta.join(), calls);
try { (function() { "use strict"; ta[0] = 7; })(); r.push("no error"); } catch (e) { r.push(e.constructor.name); }
r.push(Reflect.set(ta, 0, 1), Reflect.set(ta, 5, 1), Reflect.set(ta, "-0", 1), Reflect.set(Object.create(ta), 0, 1));
assert.sameValue(String(r.join()), "1,2,0,TypeError,false,false,false,false");
