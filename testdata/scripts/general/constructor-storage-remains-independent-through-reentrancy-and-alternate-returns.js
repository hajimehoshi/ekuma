// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "constructor storage remains independent through reentrancy and alternate returns"
flags: [noStrict]
---*/

let saved = [], replacement = {};
function C(depth, replace) {
	this.before = depth;
	saved.push(this);
	if (depth) this.child = new C(depth - 1, false);
	this.after = depth;
	if (replace) return replacement;
}
new C(0, false);
let result = new C(2, true);
let later = new C(0, false);
saved[1].before = 9;
assert.sameValue(String([result === replacement, saved.map(o => o.before), saved.map(o => o.after),
 Object.keys(later), saved[1].child === saved[2], saved[2].child === saved[3]].join("|")), "true|0,9,1,0,0|0,2,1,0,0|before,after|true|true");
