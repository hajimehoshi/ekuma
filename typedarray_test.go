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
