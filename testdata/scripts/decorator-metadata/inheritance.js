// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "inheritance"
flags: [noStrict]
---*/

function meta(key, value) { return (v, c) => { c.metadata[key] = value; }; }
function append(value) { return (v, c) => { c.metadata.list = [...(c.metadata.list ?? []), value]; }; }
@meta("a", "x") @append("x")
class A { @meta("b", "y") m() {} }
class B extends A {}
@append("z")
class C extends B { @meta("b", "z") m() {} }
var am = A[Symbol.metadata], cm = C[Symbol.metadata];
assert.sameValue(String([Object.getPrototypeOf(cm) === am, B[Symbol.metadata] === am, Object.hasOwn(B, Symbol.metadata),
	cm.a, cm.b, am.b, cm.list.join(" "), am.list.join(" ")].join()), "true,true,false,x,z,y,x z,x");
