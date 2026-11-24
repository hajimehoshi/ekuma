// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "a numeric next method throws a TypeError when called"
flags: [noStrict]
---*/

var it = { [Symbol.iterator]() { return { next: 0 }; } };
var log = [];
try { var [a] = it; } catch (e) { log.push(e instanceof TypeError); }
try { for (var x of it); } catch (e) {
	log.push(e instanceof TypeError);
	assert.sameValue(e.stack.includes(".next ("), false);
}
try { [...it]; } catch (e) { log.push(e instanceof TypeError); }
assert.sameValue(String(log.join()), "true,true,true");
