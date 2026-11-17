// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Number.parseFloat and Number.parseInt are the globals"
flags: [noStrict]
---*/

assert.sameValue(String((Number.parseFloat === parseFloat) + ":" + (Number.parseInt === parseInt) + ":" + Number.parseInt("ff", 16)), "true:true:255");
