// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "reflecting on or mutating a builtin namespace for the first time uses its own realm"
---*/

const foreign = $262.createRealm().global;

const foreignMath = foreign.Math;
const abs = Object.getOwnPropertyDescriptor(foreignMath, "abs").value;
assert.sameValue(Object.getPrototypeOf(abs), foreign.Function.prototype);
assert.sameValue(foreignMath.abs, abs);

const foreignReflect = foreign.Reflect;
assert.sameValue(delete foreignReflect.apply, true);
assert.sameValue(typeof foreignReflect.construct, "function");
assert.sameValue(Object.hasOwn(foreignReflect, "apply"), false);
assert.sameValue(typeof Reflect.apply, "function");

const foreignAtomics = foreign.Atomics;
Object.freeze(foreignAtomics);
assert.sameValue(Object.getOwnPropertyDescriptor(foreignAtomics, "load").writable, false);
assert.sameValue(Object.getOwnPropertyDescriptor(Atomics, "load").writable, true);
