// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "era replaces year"
flags: [noStrict]
---*/

// An era and an era year replace the year.
assert.sameValue(String(Temporal.PlainDate.from("2024-03-15[u-ca=gregory]").with({era: "bce", eraYear: 1}).toString()), "0000-03-15[u-ca=gregory]");
