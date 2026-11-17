// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "shift and push on a long queue"
flags: [noStrict]
---*/

var q = [], r = [];
for (var i = 0; i < 100; i++) q.push(i);
for (var i = 0; i < 150; i++) { r.push(q.shift()); if (i % 2) q.push(100 + i); }
assert.sameValue(String([q.length, r.slice(95, 105).join(), q.join()].join("|")), "25|95,96,97,98,99,101,103,105,107,109|201,203,205,207,209,211,213,215,217,219,221,223,225,227,229,231,233,235,237,239,241,243,245,247,249");
