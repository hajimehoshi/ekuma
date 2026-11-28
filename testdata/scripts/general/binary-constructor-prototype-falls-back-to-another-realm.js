// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "binary constructor prototype fallback initializes the new target's realm"
---*/

const foreign = $262.createRealm();
const Target = foreign.evalScript("(function Target() {})");
Target.prototype = null;

const view = Reflect.construct(Uint8Array, [2], Target);
assert.sameValue(Object.getPrototypeOf(view), foreign.global.Uint8Array.prototype);
assert.sameValue(Object.getPrototypeOf(view) === Uint8Array.prototype, false);
