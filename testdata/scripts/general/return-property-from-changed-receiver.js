// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: A getter returning a field observes its replacement with an accessor
---*/

const o = { x: 1, get value() { return this.x; } };
assert.sameValue(o.value, 1);
Object.defineProperty(o, "x", { get() { return this; } });
for (let i = 0; i < 2; i++) {
	assert.sameValue(o.value, o);
}
