// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "locals of a module that a closure assigns while they are in use"
flags: [module]
---*/

let origp;
{ let p = {}; origp = p; let h = () => { p = {}; }; p.x = (h(), 1); }
assert.sameValue(origp.x, 1, "member object in a block");

let q = {};
const origq = q;
const h = () => { q = {}; };
q.x = (h(), 1);
assert.sameValue(origq.x, 1, "member object at the top level");
