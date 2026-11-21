// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Temporal.Instant uses the intrinsic prototype of new.target's realm"
---*/

const foreign = $262.createRealm().global;
const temporal = foreign.Temporal;
const target = foreign.Function();
target.prototype = null;
delete foreign.Temporal;

const instant = Reflect.construct(Temporal.Instant, [0n], target);
const toLocaleString = instant.toLocaleString;
assert.sameValue(Object.getPrototypeOf(instant) === temporal.Instant.prototype, true);
assert.sameValue(toLocaleString, temporal.Instant.prototype.toLocaleString);
