// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "A sloppy assignment to an unresolvable name sets a global property even if the value creates a lexical binding"
flags: [noStrict, noRepeat]
noRepeatReason: "a realm accepts the global lexical declaration only once"
---*/

function value() {
	$262.evalScript("let lateBinding = 1;");
	return 2;
}
lateBinding = value();
assert.sameValue(globalThis.lateBinding, 2);
assert.sameValue(lateBinding, 1);
