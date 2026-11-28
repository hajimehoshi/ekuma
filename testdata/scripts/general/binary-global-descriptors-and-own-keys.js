// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "binary constructor globals keep their own-key order and data descriptors on first use"
---*/

const foreign = $262.createRealm().global;
const names = [
    "ArrayBuffer", "SharedArrayBuffer", "Int8Array", "Uint8Array", "Uint8ClampedArray",
    "Int16Array", "Uint16Array", "Int32Array", "Uint32Array", "Float16Array", "Float32Array",
    "Float64Array", "BigInt64Array", "BigUint64Array", "DataView",
];
const start = Object.getOwnPropertyNames(foreign).indexOf("ArrayBuffer");
const expected = names.concat("Atomics").join();
assert.sameValue(Object.getOwnPropertyNames(foreign).slice(start, start + names.length + 1).join(), expected);

for (const name of names) {
    const desc = Object.getOwnPropertyDescriptor(foreign, name);
    assert.sameValue(typeof desc.value, "function", name);
    assert.sameValue(desc.value, foreign[name], name);
    assert.sameValue(desc.value.name, name);
    assert.sameValue(desc.writable, true, name);
    assert.sameValue(desc.enumerable, false, name);
    assert.sameValue(desc.configurable, true, name);
}
assert.sameValue(Object.getOwnPropertyNames(foreign).slice(start, start + names.length + 1).join(), expected);
assert.sameValue(Object.getPrototypeOf(foreign.ArrayBuffer), foreign.Function.prototype);
assert.sameValue(foreign.ArrayBuffer === ArrayBuffer, false);
