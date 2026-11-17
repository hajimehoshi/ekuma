// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "return of a generator disposes, and an error of disposing replaces the return"
flags: [noStrict]
---*/

var log = [];
function res(name, error) {
	return { [Symbol.dispose]() { log.push(name); if (error) throw error; } };
}
function* g(error) { using a = res("a", error); yield; }
var it = g();
it.next();
log.push(JSON.stringify(it.return(1)));
it = g("boom");
it.next();
try { it.return(2); } catch (e) { log.push(e); }
log.push(it.next().done);
assert.sameValue(String(log.join()), "a,{\"value\":1,\"done\":true},a,boom,true");
