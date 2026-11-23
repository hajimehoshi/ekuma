// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "the first own property query of a Temporal member with a key computed at run time finds it"
---*/

// The key is made at run time, so that no property key has its contents before Temporal sets up
// its members. The query must be the first operation on Temporal.
var key = ["Plain", "Time"].join("");
assert.sameValue(Object.hasOwn(Temporal, key), true);
