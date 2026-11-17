// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "undecorated class"
flags: [noStrict]
---*/

// A class gets the property only when hasDecorators is true.
@(() => {}) class A {}
class B { accessor x; static {} }
class C extends A { accessor x; }
assert.sameValue(String([Object.hasOwn(B, Symbol.metadata), B[Symbol.metadata], Object.hasOwn(C, Symbol.metadata),
	C[Symbol.metadata] === A[Symbol.metadata]].join()), "false,,false,true");
