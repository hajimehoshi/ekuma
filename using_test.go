// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package ekuma_test

import (
	"testing"

	"github.com/hajimehoshi/ekuma"
)

// TestUsing covers the ways out of a scope with using declarations that test262 does not.
func TestUsing(t *testing.T) {
	const res = `var log = [];
		function res(name, error) {
			return { [Symbol.dispose]() { log.push(name); if (error) throw error; } };
		}
		`
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "break, continue and return dispose the resources they leave",
			src: res + `for (var i = 0; i < 2; i++) {
					using a = res("a" + i);
					if (i == 0) continue;
					break;
				}
				(function () { using b = res("b"); { using c = res("c"); return; } })();
				log.join()`,
			want: "a0,a1,c,b",
		},
		{
			name: "an error of disposing on a break is thrown",
			src: res + `var r;
				try { for (;;) { using a = res("a", "boom"); break; } } catch (e) { r = e; }
				log.push(r);
				log.join()`,
			want: "a,boom",
		},
		{
			name: "for-of disposes the binding before it closes the iterator",
			src: res + `var iterable = {
					[Symbol.iterator]() {
						return {
							next() { return { value: res("v"), done: false }; },
							return() { log.push("close"); return {}; },
						};
					},
				};
				for (using v of iterable) break;
				try { for (using v of iterable) throw "body"; } catch (e) { log.push(e); }
				log.join()`,
			want: "v,close,v,close,body",
		},
		{
			name: "return of a generator disposes, and an error of disposing replaces the return",
			src: res + `function* g(error) { using a = res("a", error); yield; }
				var it = g();
				it.next();
				log.push(JSON.stringify(it.return(1)));
				it = g("boom");
				it.next();
				try { it.return(2); } catch (e) { log.push(e); }
				log.push(it.next().done);
				log.join()`,
			want: `a,{"value":1,"done":true},a,boom,true`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vm := ekuma.NewVM()
			v, err := vm.RunScript(t.Context(), tt.name, tt.src)
			if err != nil {
				t.Fatalf("RunScript: %v", err)
			}
			if got := toString(t, vm, v); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// TestAwaitUsingOnReturn checks that the return method of an async generator awaits the
// disposal of the await using declarations it leaves.
func TestAwaitUsingOnReturn(t *testing.T) {
	vm := ekuma.NewVM()
	src := `var log = [];
		async function* g(error) {
			await using a = {
				async [Symbol.asyncDispose]() { await null; log.push("disposed"); if (error) throw error; },
			};
			yield;
		}
		var it = g(), it2 = g("boom");
		it.next().then(() => it.return(1)).then(r => log.push(JSON.stringify(r)));
		it2.next().then(() => it2.return(2)).catch(e => log.push(e));`
	if _, err := vm.RunScript(t.Context(), "await using", src); err != nil {
		t.Fatalf("RunScript: %v", err)
	}
	v, err := vm.RunScript(t.Context(), "log", `log.join()`)
	if err != nil {
		t.Fatalf("RunScript: %v", err)
	}
	if got, want := toString(t, vm, v), `disposed,disposed,{"value":1,"done":true},boom`; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
