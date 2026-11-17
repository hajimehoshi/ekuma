// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "order of the constants of Math"
flags: [noStrict]
---*/

assert.sameValue(String(Object.getOwnPropertyNames(Math).slice(0, 8).join()), "E,LN10,LN2,LOG10E,LOG2E,PI,SQRT1_2,SQRT2");
