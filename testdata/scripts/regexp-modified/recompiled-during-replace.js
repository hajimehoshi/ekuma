// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "A replacer receives the group names from the pattern that matched"
flags: [noStrict]
---*/

const re = /(?<first>a)/g;
let calls = 0;
Object.defineProperty(re, "exec", {
	get() {
		if (++calls === 2) {
			re.compile("(?<second>b)", "g");
			re.lastIndex = 1;
		}
		return RegExp.prototype.exec;
	}
});

const result = "ab".replace(re, (...args) => Object.keys(args.at(-1))[0]);
assert.sameValue(result, "firstsecond");
