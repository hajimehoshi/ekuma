// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Argument forwarding resolves arguments inside a with statement"
flags: [noStrict]
---*/

function first(x) { return x; }
function invoke(scope) {
	with (scope) return first(...arguments);
}

assert.sameValue(invoke({arguments: [9]}), 9);
const scope = {};
assert.sameValue(invoke(scope), scope);
