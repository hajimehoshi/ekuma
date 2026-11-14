// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package ekuma_test

import (
	"testing"

	"github.com/hajimehoshi/ekuma"
)

// TestTypedArray runs scripts about typed arrays whose expected results test262 does not fix.
func TestTypedArray(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			// The specification leaves the byte order of typed arrays to the platform.
			name: "little-endian",
			src: `[
				new Uint8Array(new Uint16Array([0x0102]).buffer),
				new Uint8Array(new Int32Array([-2]).buffer),
				new Uint8Array(new Float32Array([1.5]).buffer),
				new Uint8Array(new Float64Array([-2]).buffer),
				new Uint8Array(new Float16Array([1.5]).buffer),
				new Uint8Array(new BigInt64Array([-0x0102n]).buffer),
			].join(" ")`,
			want: "2,1 254,255,255,255 0,0,192,63 0,0,0,0,0,0,0,192 0,62 254,254,255,255,255,255,255,255",
		},
		{
			// A data block that cannot be created is a RangeError instead of an allocation failure.
			name: "allocation limit",
			src: `var r = [];
				for (var f of [() => new ArrayBuffer(2 ** 33), () => new Float64Array(2 ** 31), () => new ArrayBuffer(0, {maxByteLength: 2 ** 33})]) {
					try { f(); r.push("no error"); } catch (e) { r.push(e.constructor.name); }
				}
				r.join()`,
			want: "RangeError,RangeError,RangeError",
		},
		{
			// The length of a typed array keeps following its getter and own properties
			// after repeated reads of the same site.
			name: "length",
			src: `var a = new Uint8Array(4), r = [];
				function len(x) { return x.length; }
				for (var i = 0; i < 10; i++) len(a);
				r.push(len(a));
				Object.defineProperty(Object.getPrototypeOf(Uint8Array.prototype), "length", {get() { return 7; }});
				r.push(len(a));
				Object.defineProperty(a, "length", {value: 9});
				r.push(len(a));
				r.join()`,
			want: "4,7,9",
		},
		{
			// A store into an immutable buffer fails before the value is converted,
			// whether or not the index is valid and whatever the receiver is.
			name: "immutable element store",
			src: `var ta = new Uint8Array(new Uint8Array([1, 2]).buffer.transferToImmutable()), r = [], calls = 0;
				var v = {valueOf() { calls++; return 9; }};
				for (var i = 0; i < 3; i++) { ta[0] = 7; ta[1] = v; }
				r.push(ta.join(), calls);
				try { (function() { "use strict"; ta[0] = 7; })(); r.push("no error"); } catch (e) { r.push(e.constructor.name); }
				r.push(Reflect.set(ta, 0, 1), Reflect.set(ta, 5, 1), Reflect.set(ta, "-0", 1), Reflect.set(Object.create(ta), 0, 1));
				r.join()`,
			want: "1,2,0,TypeError,false,false,false,false",
		},
		{
			// The elements of an immutable buffer are non-writable and non-configurable,
			// so a typed array on one can be frozen.
			name: "immutable element descriptors",
			src: `var ta = new Uint8Array(new ArrayBuffer(2).transferToImmutable()), r = [];
				var d = Object.getOwnPropertyDescriptor(ta, 0);
				r.push(d.writable, d.enumerable, d.configurable);
				r.push(
					Reflect.defineProperty(ta, 0, {value: 0}),
					Reflect.defineProperty(ta, 0, {value: 1}),
					Reflect.defineProperty(ta, 0, {value: "0"}),
					Reflect.defineProperty(ta, 0, {writable: true}),
					Reflect.defineProperty(ta, 0, {writable: false, enumerable: true, configurable: false}));
				r.push(Object.isFrozen(ta), Object.isFrozen(Object.freeze(ta)));
				try { Object.freeze(new Uint8Array(2)); r.push("no error"); } catch (e) { r.push(e.constructor.name); }
				r.join()`,
			want: "false,true,false,true,false,false,false,true,false,true,TypeError",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vm := ekuma.NewVM()
			v, err := vm.RunScript(t.Context(), "test", tt.src)
			if err != nil {
				t.Fatalf("RunScript: %v", err)
			}
			if got := v.String(); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// TestDetachImmutableArrayBuffer tests that $262.detachArrayBuffer refuses an immutable buffer.
func TestDetachImmutableArrayBuffer(t *testing.T) {
	vm := ekuma.NewVM()
	b, err := vm.RunScript(t.Context(), "test", "new ArrayBuffer(1).transferToImmutable()")
	if err != nil {
		t.Fatalf("RunScript: %v", err)
	}
	if err := ekuma.DetachArrayBuffer(vm, b); err == nil {
		t.Errorf("DetachArrayBuffer: got no error")
	}
	if got, err := b.Get("detached"); err != nil || got.Bool() {
		t.Errorf("detached: got %v, %v, want false", got, err)
	}
}
