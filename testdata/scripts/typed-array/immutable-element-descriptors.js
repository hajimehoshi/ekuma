// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "immutable element descriptors"
flags: [noStrict]
---*/

// The elements of an immutable buffer are non-writable and non-configurable,
// so a typed array on one can be frozen.
var ta = new Uint8Array(new ArrayBuffer(2).transferToImmutable()), r = [];
var d = Object.getOwnPropertyDescriptor(ta, 0);
r.push(d.writable, d.enumerable, d.configurable);
r.push(
	Reflect.defineProperty(ta, 0, {value: 0}),
	Reflect.defineProperty(ta, 0, {value: 1}),
	Reflect.defineProperty(ta, 0, {value: "0"}),
	Reflect.defineProperty(ta, 0, {writable: true}),
	Reflect.defineProperty(ta, 0, {writable: false, enumerable: true, configurable: false}));
r.push(Object.isFrozen(ta), Object.isFrozen(Object.freeze(ta)));
try { Object.freeze(new Uint8Array(2)); r.push("no error"); } catch (e) { r.push(e.constructor.name); }
assert.sameValue(String(r.join()), "false,true,false,true,false,false,false,true,false,true,TypeError");
