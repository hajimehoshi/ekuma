// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "caller of a sloppy function called in tail position"
flags: [noStrict]
---*/

function f() { return f.caller; }
// The tail call discards the frame of s, so the caller of f is that of s.
function s() { "use strict"; return f(); }
function outer() { return s(); }
assert.sameValue(outer(), outer);
