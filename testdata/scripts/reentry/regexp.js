// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "RegExp methods see a RegExp object that a conversion or a callback changes"
---*/

// check compares what f returns, as JSON with "undefined" for undefined, or the name of the error
// that it throws.
function check(name, expected, f) {
	let actual;
	try {
		actual = JSON.stringify(f(), (key, value) => value === undefined ? "undefined" : value);
	} catch (e) {
		actual = e.name;
	}
	assert.sameValue(actual, expected, name);
}

check("split recompiled by the limit", '["a","c","d"]', function () {
	const re = /b/;
	return re[Symbol.split]("abcbd", { valueOf() { re.compile("c"); return 9; } });
});
check("split with an exec defined by the limit", '["a","c","d"]', function () {
	const re = /b/;
	return re[Symbol.split]("abcbd", { valueOf() { re.exec = () => null; return 9; } });
});
check("replace with a replacer that changes lastIndex and the pattern", '"a!c!d"', function () {
	const re = /b/g;
	return "abcbd".replace(re, () => { re.lastIndex = 99; re.compile("z"); return "!"; });
});
check("exec recompiled by lastIndex", '["cd","c","d"]', function () {
	const re = /(b)/g;
	re.lastIndex = { valueOf() { re.compile("(c)(d)", "g"); return 0; } };
	return re.exec("abcd");
});
check("test recompiled by lastIndex", "[true,0]", function () {
	const re = /b/y;
	re.lastIndex = { valueOf() { re.compile("a", ""); return 1; } };
	return [re.test("ab"), re.lastIndex];
});
check("match with a conversion that sets lastIndex", '["b","b"]', function () {
	const re = /b/g;
	return re[Symbol.match]({ toString() { re.lastIndex = 3; return "abab"; } });
});
check("matchAll recompiled by lastIndex", '["b","b"]', function () {
	const re = /b/g;
	re.lastIndex = { valueOf() { re.compile("a", "g"); return 0; } };
	return [...re[Symbol.matchAll]("abab")].map(m => m[0]);
});
check("search with a conversion that sets lastIndex", "[1,2]", function () {
	const re = /b/g;
	re.lastIndex = 1;
	return [re[Symbol.search]({ toString() { re.lastIndex = 2; return "ab"; } }), re.lastIndex];
});
