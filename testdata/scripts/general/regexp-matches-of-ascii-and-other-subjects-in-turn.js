// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "regexp matches of ASCII and other subjects in turn"
---*/

// One regular expression matched against subjects that alternate between ASCII and not, short
// enough to be packed in the value or not. A character of the others spans several bytes.
var re = /..c/, g = /..c/g;
var subjects = ["abc", "ééc", "xyc", "ééc", "a longer string that ends in abc", "a longer string that ends in ééc"];
var r = [];
for (var k = 0; k < 2; k++) {
	for (var s of subjects) {
		r.push(re.exec(s)[0], s.replace(g, "-").slice(-2), s.split(re).length);
	}
}
var want = "abc,-,2,ééc,-,2,xyc,-,2,ééc,-,2,abc, -,2,ééc, -,2";
assert.sameValue(r.join(), want + "," + want);
