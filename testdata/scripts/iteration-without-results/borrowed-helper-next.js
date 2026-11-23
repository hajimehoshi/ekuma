// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "A borrowed next method creates each zip row in the helper's realm"
includes: [realm-assertions.js]
---*/

const B = $262.createRealm();
const C = $262.createRealm();
const check = realmAssertions({ A: $262, B, C });
const next = B.evalScript("Iterator.zip([]).next");
const zip = C.evalScript("Iterator.zip([[1, 2], [3, 4]])");
const yielded = next.call(zip);
check.prototype(yielded, "C", "Object", "borrowed next: yielded result");
check.prototype(yielded.value, "C", "Array", "borrowed next: zip row");

// The optimized step requires a next method from the realm running destructuring.
zip.next = Iterator.zip([]).next;
const [row] = zip;
check.prototype(row, "C", "Array", "destructuring with A's next: zip row");
check.prototype(next.call(zip), "B", "Object", "borrowed next: completed result");
