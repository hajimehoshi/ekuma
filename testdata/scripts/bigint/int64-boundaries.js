// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "BigInt operations at and beyond signed 64-bit boundaries"
flags: [noStrict]
---*/

assert.sameValue(String(9223372036854775807n + 1n), "9223372036854775808");
assert.sameValue(String(-9223372036854775808n - 1n), "-9223372036854775809");
assert.sameValue(String(-4294967296n * 2147483648n), "-9223372036854775808");
assert.sameValue(String(4294967296n * 2147483648n), "9223372036854775808");
assert.sameValue(String(-9223372036854775808n / -1n), "9223372036854775808");
assert.sameValue(String(-9223372036854775808n % -1n), "0");
assert.sameValue(String(`${-7n / 2n} ${-7n % 2n}`), "-3 -1");
assert.sameValue(String((() => { try { 1n / 0n; } catch (e) { return e instanceof RangeError; } })()), "true");
assert.sameValue(String((() => { try { 1n % 0n; } catch (e) { return e instanceof RangeError; } })()), "true");
assert.sameValue(String((-2n) ** 63n), "-9223372036854775808");
assert.sameValue(String(3n ** 40n), "12157665459056928801");
assert.sameValue(String((-1n) ** 9007199254740993n), "-1");
assert.sameValue(String((() => { try { 2n ** -1n; } catch (e) { return e instanceof RangeError; } })()), "true");
assert.sameValue(String(1n << 63n), "9223372036854775808");
assert.sameValue(String(-1n << 63n), "-9223372036854775808");
assert.sameValue(String(1n << 64n), "18446744073709551616");
assert.sameValue(String(-5n >> 1n), "-3");
assert.sameValue(String(5n >> -2n), "20");
assert.sameValue(String(-1n >> 64n), "-1");
assert.sameValue(String((() => { try { -1n >> -9223372036854775808n; } catch (e) { return e instanceof RangeError; } })()), "true");
assert.sameValue(String(-(-9223372036854775808n)), "9223372036854775808");
assert.sameValue(String((() => { let x = 9223372036854775807n; x++; return x; })()), "9223372036854775808");
// -1n has the bits of a NaN.
assert.sameValue(String(0n - 1n === -1n), "true");
// The bits of -9223372036854775808n are those of -0.
assert.sameValue(String(-9223372036854775808n === 0n), "false");
assert.sameValue(String(!!-9223372036854775808n), "true");
// A result computed beyond the range of int64 that comes back into it, compared with
// a value that never left it.
assert.sameValue(String(2n ** 64n - 18446744073709551615n === BigInt(1)), "true");
// The number is 2 ** 63.
assert.sameValue(String(9223372036854775807n < 9223372036854775807), "true");
assert.sameValue(String(-9223372036854775808n == -9223372036854775808), "true");
assert.sameValue(String(1n < 1.5), "true");
assert.sameValue(String(-1n > -1.5), "true");
assert.sameValue(String(1.5 > 1n), "true");
assert.sameValue(String(BigInt.asIntN(64, 9223372036854775808n)), "-9223372036854775808");
assert.sameValue(String(BigInt.asIntN(8, 255n)), "-1");
assert.sameValue(String(BigInt.asUintN(64, -1n)), "18446744073709551615");
assert.sameValue(String(BigInt.asUintN(8, -1n)), "255");
assert.sameValue(String(BigInt.asUintN(65, -1n)), "36893488147419103231");
assert.sameValue(String(new BigUint64Array([-1n])[0]), "18446744073709551615");
assert.sameValue(String(new BigInt64Array([2n ** 63n])[0]), "-9223372036854775808");
assert.sameValue(String(new BigInt64Array([-1n]).includes(-1n)), "true");
// The element has the bits of -1n.
assert.sameValue(String(new BigUint64Array([-1n]).includes(-1n)), "false");
assert.sameValue(String(new BigUint64Array([-1n]).includes(2n ** 64n - 1n)), "true");
// The value lies halfway between two numbers.
assert.sameValue(String(Number(9007199254740993n)), "9007199254740992");
assert.sameValue(String(BigInt(-9223372036854775808)), "-9223372036854775808");
assert.sameValue(String(BigInt(9223372036854775808)), "9223372036854775808");
assert.sameValue(String((-9223372036854775808n).toString(2)), "-1000000000000000000000000000000000000000000000000000000000000000");
assert.sameValue(String(Object(0n - 1n).valueOf() === -1n), "true");
