// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "A borrowed generator next method returns a yielded result from the generator's realm"
---*/

const other = $262.createRealm();
const gen = other.evalScript("(function* () { yield 1; })()");
const next = (function* () {})().next;
assert.sameValue(Object.getPrototypeOf(next.call(gen)), other.global.Object.prototype);
assert.sameValue(Object.getPrototypeOf(next.call(gen)), Object.prototype);
