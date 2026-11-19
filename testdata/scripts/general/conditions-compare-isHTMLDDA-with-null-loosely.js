// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "conditions compare an IsHTMLDDA object with null loosely"
flags: [noStrict]
---*/

var dda = $262.IsHTMLDDA;
var r = [];
if (dda == null) r.push("== null");
if (dda != undefined) r.push("!= undefined");
if (dda === null) r.push("=== null");
if (!(dda !== undefined)) r.push("!== undefined");
assert.sameValue(r.join(), "== null");
