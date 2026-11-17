// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "caller hidden from a sloppy function"
flags: [noStrict]
---*/

function f() { return f.caller; }
function s() { "use strict"; var c = f(); return c; }
function* gen() { yield f(); }
var r;
(async function() { r = f(); })();
function n() { return [0].map(f)[0]; }
assert.sameValue(String([s(), gen().next().value, r, n()].map(v => v === null).join()), "true,true,true,true");
