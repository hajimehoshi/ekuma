// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Object.prototype keeps its null prototype when Reflect.setPrototypeOf is called from another realm"
---*/

const foreign = $262.createRealm().global;
assert.sameValue(Reflect.setPrototypeOf(foreign.Object.prototype, {}), false);
assert.sameValue(Object.getPrototypeOf(foreign.Object.prototype), null);
