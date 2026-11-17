// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "let read before its initialization"
flags: [noStrict]
---*/

function f() {
	const g = () => x;
	function h() { return x; }
	let r = [];
	try { g(); } catch (e) { r.push(e.name); }
	try { h(); } catch (e) { r.push(e.name); }
	let x = 1;
	return r.concat(g(), h()).join();
}
assert.sameValue(String(f()), "ReferenceError,ReferenceError,1,1");
