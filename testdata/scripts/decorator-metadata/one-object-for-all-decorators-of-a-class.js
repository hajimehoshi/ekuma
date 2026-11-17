// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "one object for all decorators of a class"
flags: [noStrict]
---*/

var seen = [];
function dec(v, c) { seen.push(c.metadata); c.metadata[c.kind + (c.static ? " static" : "") + (c.private ? " private" : "")] = true; }
@dec @dec
class C {
	@dec m() {}
	@dec get g() { return 1; }
	@dec set s(v) {}
	@dec f = 1;
	@dec accessor a = 1;
	@dec static sm() {}
	@dec static sf = 1;
	@dec #pm() {}
	@dec #pf = 1;
	@dec static accessor #pa = 1;
}
var m = C[Symbol.metadata];
assert.sameValue(String([seen.length, seen.every(x => x === m), Object.keys(m).sort().join("|")].join()), "12,true,accessor|accessor static private|class|field|field private|field static|getter|method|method private|method static|setter");
