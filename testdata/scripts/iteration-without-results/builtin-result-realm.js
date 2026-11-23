// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Borrowed next methods create iterator results in the realm of the allocating operation"
---*/

const other = $262.createRealm();
const keys = other.evalScript("new Map([[1, 2]]).keys()");
const mapNext = new Map().keys().next;
assert.sameValue(Object.getPrototypeOf(mapNext.call(keys)), other.global.Object.prototype);
assert.sameValue(Object.getPrototypeOf(mapNext.call(keys)), Object.prototype);

const values = other.evalScript("[1].values()");
assert.sameValue(Object.getPrototypeOf([].values().next.call(values)), Object.prototype);
