// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "binary initialization preserves global mutations and the original Array method alias"
---*/

const foreign = $262.createRealm().global;
const originalToString = foreign.Array.prototype.toString;
Object.defineProperty(foreign.Array.prototype, "toString", {
    get() { throw new Test262Error("read of replaced Array.prototype.toString"); },
});
foreign.ArrayBuffer = undefined;
assert.sameValue(delete foreign.SharedArrayBuffer, true);
Object.defineProperty(foreign, "Int8Array", {
    enumerable: true,
    writable: false,
    configurable: false,
});

const inheritsGlobals = Object.create(foreign);
function readBytes() { return inheritsGlobals.Uint8Array; }
const Bytes = readBytes();
assert.sameValue(readBytes(), Bytes);
assert.sameValue(Bytes, foreign.Uint8Array);
assert.sameValue(Bytes.prototype.toString, originalToString);
assert.sameValue(new Bytes([2, 3]).toString(), "2,3");
assert.sameValue(foreign.ArrayBuffer, undefined);
assert.sameValue(Object.hasOwn(foreign, "SharedArrayBuffer"), false);

const desc = Object.getOwnPropertyDescriptor(foreign, "Int8Array");
assert.sameValue(desc.value, foreign.Int8Array);
assert.sameValue(new desc.value(1).length, 1);
assert.sameValue(desc.enumerable, true);
assert.sameValue(desc.writable, false);
assert.sameValue(desc.configurable, false);
