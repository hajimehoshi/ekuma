// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "string and json"
flags: [noStrict]
---*/

assert.sameValue(String(Object.prototype.toString.call(Temporal) + " " + JSON.stringify(Temporal)), "[object Temporal] {}");
