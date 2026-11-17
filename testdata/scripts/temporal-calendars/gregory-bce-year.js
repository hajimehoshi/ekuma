// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "gregory bce year"
flags: [noStrict]
---*/

assert.sameValue(String(Temporal.PlainDate.from({calendar: "gregory", era: "bce", eraYear: 1, month: 1, day: 1}).year), "0");
