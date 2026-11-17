// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "freeze"
flags: [noStrict]
---*/

Object.freeze(Temporal); assert.sameValue(String([Object.isFrozen(Temporal), typeof Temporal.Instant].join()), "true,function");
