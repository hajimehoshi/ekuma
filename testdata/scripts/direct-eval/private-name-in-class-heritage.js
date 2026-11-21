// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "direct eval in class heritage rejects a private name declared only in the class body"
flags: [onlyStrict]
negative:
  phase: runtime
  type: SyntaxError
---*/

class C extends (eval("#x in {}"), Object) {
	#x;
}
