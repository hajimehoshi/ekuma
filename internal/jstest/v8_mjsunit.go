// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package jstest

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io/fs"
	"path"
	"strings"
	"testing"
)

// mjsunitOrdinaryScripts lists tests that declare --allow-natives-syntax and run as ordinary JavaScript.
var mjsunitOrdinaryScripts = map[string]string{
	"array-iterator-prototype-next.js":                           "c6efcd5e5a25f57c7091945dff30c3ea9a62180c8529a0f50e22e1d557327cf5",
	"array-push-hole-double.js":                                  "9056fc9a837354ed1356d046cc0f719652103fb2c4cee2d2565a7eef874e7a05",
	"compare-nil.js":                                             "e191a0c3105cf06a5e2335c7b76d4bd9afede59c0aac2e53d0bc6202e564e217",
	"compiler/regress-max.js":                                    "19f16c8a07b4eaee40fc272755d79e0e6838b386ab82b1e6b94735731409a9fb",
	"es6/class-computed-property-names-super.js":                 "8eade0b7de0d191fef5b09ce54ca6c8e4536f67f62d46dbc74dded1c72433aab",
	"es6/classes-accesors.js":                                    "385e28891eb711f00addb47f76685d572cd22e08069a198a0ad72609218663fd",
	"es6/classes-proto.js":                                       "dcf2425cbc1a71e8590eaa5393d9ae3a7d9d828ff0a14b30c20bf051dfe0daf9",
	"es6/classes-restricted-properties.js":                       "7d197f2602328884c1d705562d4d439170c29a76c4d147f1e86460b75fa42b88",
	"es6/classes-test-super.js":                                  "85a8f3381fe4733c3b87372e6b9e1664d522d293ce6ecee277298dbeff8b0f9f",
	"es6/computed-property-names-super.js":                       "6db2b39cc8ecd9e1a895435992757d7c9c3a8f237ddd444b7e67834afe664a4b",
	"es6/for-of-array-iterator-optimization-custom-prototype.js": "f76f3510a0c1a065030d0c8393054c0ff01decb75f7fea85865262ebbed5d150",
	"es6/generators-objects.js":                                  "cd6a676982b654900813ac963c939dbe0124b11db912995e6e17d9a9deaee73c",
	"es6/string-fromcodepoint.js":                                "550062451dcb8faad73a63a2ac7fd291a8cb0c6758db23a33e107840522f425d",
	"global-prototypes.js":                                       "43d2f0043e6cad78d4ccfcd08c9ca275059b80249ab6bf2d48f3f27ca3fb0f5c",
	"harmony/atomics-waitasync.js":                               "6260f5718afa5bb10be0c8e43da4f28d2bf4de4e2bbfcc2ed66b69ed5b55a8ab",
	"harmony/bigint/exp.js":                                      "0b4a80b5d97b2e8e1b360a3f64751992a83547caa552b5993472ed6eb30e4369",
	"harmony/bigint/json.js":                                     "2c96c4e84270fedec8247cebb616ee43745b908da92af677fc0d1b5448226221",
	"keyed-load-generic.js":                                      "89fcd21214929fc22664b1b64b690a85cfbc47173fc316457b9e9852ebdd1cb5",
	"math-sum-precise/array-iterator-modification-1.js":          "890178696332a022a60bf253d74f468c63bea55fbe3a346fb0a10de4e1860881",
	"math-sum-precise/array-iterator-modification-2.js":          "e8878463dc6609ecf7010819bdf4e9f944a24d4634a084bcab0a841b9dfbfc90",
	"math-sum-precise/typed-array-iterator-modification-1.js":    "e39184456c0d02747fb0e90cf1d11a22a75df0b38cb6ed9eef1da9028501b40c",
	"math-sum-precise/typed-array-iterator-modification-2.js":    "fcf66cb571b30099f286a96f8a2fe5dd610d48a78cdb42f9858583c746d3e882",
	"math-sum-precise/typed-array-iterator-modification-3.js":    "1cd147a4dd15608c5aced55458c311ddbc9ae0ba91e0d322274e898048399c14",
	"math-sum-precise/typed-array-iterator-modification-4.js":    "c4baa0d0d5ddfbc690dc9970faaa5bdcff1f3ba59c9dd434cbec759a07b87817",
	"object-assign-regressions.js":                               "eadc4a61de2fd8e90b3db258c9a450301749e4af3ef33eea4ff3c187dcdcafd5",
	"regress/regress-2836.js":                                    "02b993d08ad1b3f621aed3954a38bacf9e519099dc16451109c42cb5eb5ef973",
	"regress/regress-334.js":                                     "c1db256b5861307652621a750af3166994bfd594a4ca363775d8c79e9aa6039c",
	"regress/regress-5772.js":                                    "c4ad891013f90b73ec20b03107ee02cd9a166ed7e6f402206fa88c841ccb20a7",
	"regress/regress-726636.js":                                  "d10312d60727bf4353154e7009c6b19970df0405f1fece07b3fbf11abe996e70",
	"regress/regress-8630.js":                                    "4edd8a157d53ab70a8947aeb6ae107e05146e4551eb78c95b42ed30a472bff2c",
	"regress/regress-996391.js":                                  "1e9b42ec3e6c29b72ca1b9fabd7e215590a27ab65499c589ee32561a2baf74b8",
	"regress/regress-crbug-1158138.js":                           "885572e7d5418be8644314d8fdeeb36fb401351e0d29fa3c557f338513f8a754",
	"regress/regress-crbug-618845.js":                            "a766d770b9c0ffcc871c543583aebc1ec7edff61c989b1eda0e86b05937f273f",
	"regress/regress-crbug-650973.js":                            "a28fd22c6e3bc85c496cdbfe42ce7751246152cebca2136b33d7b27bedd9c53d",
	"regress/regress-crbug-752712.js":                            "3b7ba21bc572a25fc04d0d107d0c428423e1ba1f058c7d06cafec62b7cd4b294",
	"regress/regress-crbug-917076.js":                            "6c89fb3adffcda9e656aed47ed96f6b16addd0ede6dd47970b3f13241132dea3",
	"regress/regress-crbug-992914.js":                            "ed4a21a4132147a0b3f5c80169111671694cf45b68e7c8a67f2e15b671e482ce",
	"regress/regress-v8-5958.js":                                 "87dea5d2b41c77044b8a1b267033dee20f413b4f84452f0abef89d8110a49c2a",
	"regress/regress-v8-8357.js":                                 "889cc46212300ba7a546d62546e632ff2988412495b22390b29166503f830a56",
	"regress/regress-v8-8445-2.js":                               "2a7b1b1002c971826d7d25b06a3694e1e7e15e1aa6b27d07650af7dbdcf00b29",
	"regress/regress-v8-8445.js":                                 "28a9b1299eb76083d61d7c57baacc2aa42ff132966fca1730cab56a8c4b18c6e",
	"regress/regress-x87.js":                                     "cf78b362d15c7086ed1a215ffe406593aa7be456bc091ab1cecb6f243e05ce03",
	"samevalue.js":                                               "762c71c52aee6e10853ff2a4034310a992883e0bf5fa90175179f9a3c4193e42",
	"typedarray-with-resizable-buffer.js":                        "532ba77b0bff182473c64c555c073ae139deb9955bcce51943088840332679b9",
}

// checkedMjsunitOrdinaryScripts returns the selected test paths whose sources and shared helper match the reviewed versions.
func checkedMjsunitOrdinaryScripts(t *testing.T, checkout fs.FS, root, filter string) map[string]bool {
	t.Helper()
	check := func(file, want string) {
		t.Helper()
		src, err := fs.ReadFile(checkout, file)
		if err != nil {
			t.Fatal(err)
		}
		src = bytes.ReplaceAll(src, []byte("\r\n"), []byte("\n"))
		if fmt.Sprintf("%x", sha256.Sum256(src)) != want {
			t.Fatalf("%s changed: review the --allow-natives-syntax exception", file)
		}
	}
	checked := map[string]bool{}
	for name, sum := range mjsunitOrdinaryScripts {
		file := path.Join("test/mjsunit", name)
		if inSuiteDir(file, root) && strings.Contains(file, filter) {
			check(file, sum)
			checked[file] = true
		}
	}
	if len(checked) != 0 {
		check("test/mjsunit/mjsunit.js", "b72b6d1b7d70598e1c96b12505cec7d4eeef48c0cd1f65f70fd2cd526a2d09dd")
	}
	return checked
}
