// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "length of the result of exec"
flags: [noStrict]
---*/

var re = /x/;
re.exec = () => ({0: "x", 1: "y", index: 0, length: 2 ** 53 - 1, get 2() { throw "stop"; }});
var thrown;
try {
	"x".replace(re, "$1");
} catch (e) {
	thrown = e;
}
assert.sameValue(thrown, "stop");
