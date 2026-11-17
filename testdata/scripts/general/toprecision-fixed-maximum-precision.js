// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "toPrecision fixed maximum precision"
flags: [noStrict]
---*/

assert.sameValue(String((-0.0000011).toPrecision(100)), "-0.000001100000000000000056102041415262426937715645181015133857727050781250000000000000000000000000000000000");
