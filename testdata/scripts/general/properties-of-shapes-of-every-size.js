// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "properties of shapes of every size"
flags: [noStrict]
---*/

var out = [];
for (var n = 1; n <= 10; n++) {
	var o = {};
	for (var i = 0; i < n; i++) o["p" + i] = i;
	var s = 0;
	for (var i = 0; i < n; i++) s += o["p" + i];
	var a = Object.assign({}, o), b = Object.assign({}, o);
	Object.defineProperty(a, "p0", {enumerable: false});
	delete b["p" + (n - 1)];
	out.push(s + ":" + ("q" in o) + ":" + Object.keys(a).length + ":" + Object.keys(b).join(""));
}
assert.sameValue(String(out.join(" ")), "0:false:0: 1:false:1:p0 3:false:2:p0p1 6:false:3:p0p1p2 10:false:4:p0p1p2p3 15:false:5:p0p1p2p3p4 21:false:6:p0p1p2p3p4p5 28:false:7:p0p1p2p3p4p5p6 36:false:8:p0p1p2p3p4p5p6p7 45:false:9:p0p1p2p3p4p5p6p7p8");
