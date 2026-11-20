// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "a cached number prototype method and its replacement getter receive the primitive"
---*/

function callMethod(n) { return n.method(); }
function receiver() { "use strict"; return this; }

Number.prototype.method = receiver;
assert.sameValue(callMethod(1), 1);
assert.sameValue(callMethod(2), 2);

var seen;
Object.defineProperty(Number.prototype, "method", {
	get: function () { "use strict"; seen = this; return receiver; },
});
assert.sameValue(callMethod(3), 3);
assert.sameValue(callMethod(4), 4);
assert.sameValue(seen, 4);
