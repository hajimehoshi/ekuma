// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "delete now"
flags: [noStrict]
---*/

delete Temporal.Now; assert.sameValue(String("Now" in Temporal), "false");
