// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "this in sloppy and strict functions"
flags: [noStrict]
---*/

function s() { return this === globalThis; } function t() { "use strict"; return this === undefined; } assert.sameValue(String(s() + ":" + t()), "true:true");
