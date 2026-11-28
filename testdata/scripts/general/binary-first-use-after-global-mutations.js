// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "binary initialization preserves global mutations and the original Array method alias"
---*/

const originalToString = Array.prototype.toString;
Object.defineProperty(Array.prototype, "toString", {
    get() { throw new Test262Error("read of replaced Array.prototype.toString"); },
});
globalThis.ArrayBuffer = undefined;
assert.sameValue(delete globalThis.SharedArrayBuffer, true);
Object.defineProperty(globalThis, "Int8Array", {
    enumerable: true,
    writable: false,
    configurable: false,
});

const inheritsGlobals = Object.create(globalThis);
function readBytes() { return inheritsGlobals.Uint8Array; }
const Bytes = readBytes();
assert.sameValue(readBytes(), Bytes);
assert.sameValue(Bytes, Uint8Array);
assert.sameValue(Bytes.prototype.toString, originalToString);
assert.sameValue(new Bytes([2, 3]).toString(), "2,3");
assert.sameValue(ArrayBuffer, undefined);
assert.sameValue(Object.hasOwn(globalThis, "SharedArrayBuffer"), false);

const desc = Object.getOwnPropertyDescriptor(globalThis, "Int8Array");
assert.sameValue(desc.value, Int8Array);
assert.sameValue(new desc.value(1).length, 1);
assert.sameValue(desc.enumerable, true);
assert.sameValue(desc.writable, false);
assert.sameValue(desc.configurable, false);
