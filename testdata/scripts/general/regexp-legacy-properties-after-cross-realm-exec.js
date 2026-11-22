// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "cross-realm exec preserves legacy RegExp properties in both realms"
flags: [noStrict]
---*/

const other = $262.createRealm().global;
class SubRegExp extends other.RegExp {}

/(first)/.exec("first");
new other.RegExp("(other)").exec("other");
for (const re of [new other.RegExp("(second)"), new SubRegExp("(second)")]) {
	const match = RegExp.prototype.exec.call(re, "second");
	assert.sameValue(match[1], "second");
	assert.sameValue(RegExp.$1, "first");
	assert.sameValue(other.RegExp.$1, "other");
}
