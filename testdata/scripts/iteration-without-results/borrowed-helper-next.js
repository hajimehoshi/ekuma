// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "A borrowed next method creates each zip row in the helper's realm"
---*/

const other = $262.createRealm();
const local = Iterator.zip([]);
const zip = other.evalScript("Iterator.zip([[1, 2], [3, 4]])");
assert.sameValue(Object.getPrototypeOf(local.next.call(zip).value), other.global.Array.prototype);

zip.next = local.next;
const [row] = zip;
assert.sameValue(Object.getPrototypeOf(row), other.global.Array.prototype);
