// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "break, continue and return dispose the resources they leave"
flags: [noStrict]
---*/

var log = [];
function res(name, error) {
	return { [Symbol.dispose]() { log.push(name); if (error) throw error; } };
}
for (var i = 0; i < 2; i++) {
			using a = res("a" + i);
			if (i == 0) continue;
			break;
		}
		(function () { using b = res("b"); { using c = res("c"); return; } })();
		assert.sameValue(String(log.join()), "a0,a1,c,b");
