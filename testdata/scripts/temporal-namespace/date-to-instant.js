// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "date to instant"
flags: [noStrict]
---*/

assert.sameValue(String(new Date(0).toTemporalInstant() instanceof Temporal.Instant), "true");
