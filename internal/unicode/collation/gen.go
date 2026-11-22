// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

//go:build ignore

// This program generates the tables of package collation from the XML release of CLDR. Run it with go generate.
//
// The root collation comes from common/uca/allkeys_CLDR.txt, and the script groups and the
// implicit weights of the scripts without explicit weights from common/uca/FractionalUCA.txt.
// The tailorings come from common/collation. Each tailoring is built into the CEs of the
// strings that it changes, as ICU's CollationBuilder does.
package main

import (
	"archive/zip"
	"bufio"
	"bytes"
	"compress/flate"
	"encoding/binary"
	"encoding/xml"
	"flag"
	"fmt"
	"go/format"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/hajimehoshi/ekuma/internal/unicode/cldrgen"
	"github.com/hajimehoshi/ekuma/internal/unicode/datafile"
	"github.com/hajimehoshi/ekuma/internal/unorm"
)

const xmlVersion = "48.2"

const xmlURL = "https://unicode.org/Public/cldr/" + xmlVersion + "/cldr-common-" + xmlVersion + ".zip"

var zipFile = flag.String("zip", "", "read the XML release of CLDR from this file instead of downloading it")

// lastRegularPrimary is the root primary weight after which [last regular] tailors: the
// first weight of the Han group, so that the tailored characters belong to it, as in ICU.
// The unused weights after it, up to the first implicit weight of Han, take the tailored
// weights that do not fit after it.
const lastRegularPrimary = 0xFB10

// hanEnd is the last primary weight of the Han group. Unassigned code points and the
// trailing weights follow it.
const hanEnd = 0xFBBF

func get(url string) []byte {
	resp, err := http.Get(url)
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Fatalf("%s: %s", url, resp.Status)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Fatal(err)
	}
	return b
}

// release holds the files of the XML release that the program reads.
type release struct {
	allkeys    []byte
	fractional []byte
	license    []byte
	collation  map[string][]byte
}

func readRelease() *release {
	var b []byte
	if *zipFile != "" {
		var err error
		if b, err = os.ReadFile(*zipFile); err != nil {
			log.Fatal(err)
		}
	} else {
		b = get(xmlURL)
	}
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		log.Fatal(err)
	}
	r := &release{
		collation: map[string][]byte{},
	}
	for _, f := range zr.File {
		isCollation := path.Dir(f.Name) == "common/collation" && strings.HasSuffix(f.Name, ".xml")
		if !isCollation && f.Name != "common/uca/allkeys_CLDR.txt" && f.Name != "common/uca/FractionalUCA.txt" && f.Name != "LICENSE" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			log.Fatal(err)
		}
		data, err := io.ReadAll(rc)
		if err != nil {
			log.Fatal(err)
		}
		rc.Close()
		switch {
		case isCollation:
			r.collation[path.Base(f.Name)] = data
		case f.Name == "LICENSE":
			r.license = data
		case strings.HasSuffix(f.Name, "allkeys_CLDR.txt"):
			r.allkeys = data
		default:
			r.fractional = data
		}
	}
	if r.allkeys == nil || r.fractional == nil || r.license == nil {
		log.Fatal("the release lacks allkeys_CLDR.txt, FractionalUCA.txt or LICENSE")
	}
	return r
}

// rootCE is a collation element of the root with the weights of allkeys_CLDR.txt.
type rootCE struct {
	p, s, t uint16
}

// root is the root collation.
type root struct {
	// entries maps strings of one or more code points to their CEs.
	entries map[string][]rootCE
	// variable is the set of primary weights of variable CEs.
	variable map[uint16]bool
	// primaries is the set of primary weights of the CEs of the entries.
	primaries map[uint16]bool
	// contractions maps the first code point of the strings of more than one code point
	// to those strings.
	contractions map[rune][]string

	implicits []implicit
	// unified are the ranges of the code points with Unified_Ideograph, as pairs.
	unified []rune
	groups  []group
	// boundaries maps the strings of U+FDD1 and a code point, which name the first
	// primary weight of a group in the rules, to the first primary weight of the group.
	boundaries map[string]uint16
}

// implicit is a range of code points whose CEs are computed from a base primary weight.
type implicit struct {
	lo, hi rune
	base   uint16
	// offset is the value of the second CE's primary for lo, without the high bit.
	offset uint16
}

// group is a reordering group of the root.
type group struct {
	start, end uint16
	codes      []string
}

var ceRE = regexp.MustCompile(`\[([.*])([0-9A-F]{4})\.([0-9A-F]{4})\.([0-9A-F]{4})\]`)

func hex(s string) rune {
	n, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		log.Fatal(err)
	}
	return rune(n)
}

// parseCEs parses CEs, and returns the primary weights of the variable ones.
func parseCEs(s string) ([]rootCE, []uint16) {
	var ces []rootCE
	var variable []uint16
	for _, m := range ceRE.FindAllStringSubmatch(s, -1) {
		ce := rootCE{
			p: uint16(hex(m[2])),
			s: uint16(hex(m[3])),
			t: uint16(hex(m[4])),
		}
		ces = append(ces, ce)
		if m[1] == "*" {
			variable = append(variable, ce.p)
		}
	}
	return ces, variable
}

func parseRoot(rel *release) *root {
	if !bytes.Contains(rel.allkeys, []byte("\n@version "+datafile.UnicodeVersion+"\n")) {
		log.Fatalf("CLDR %s collation lacks UCA version %s", xmlVersion, datafile.UnicodeVersion)
	}

	r := &root{
		entries:      map[string][]rootCE{},
		variable:     map[uint16]bool{},
		primaries:    map[uint16]bool{},
		boundaries:   map[string]uint16{},
		contractions: map[rune][]string{},
	}
	sc := bufio.NewScanner(bytes.NewReader(rel.allkeys))
	for sc.Scan() {
		l := sc.Text()
		if i := strings.IndexByte(l, '#'); i >= 0 {
			l = l[:i]
		}
		l = strings.TrimSpace(l)
		if l == "" || l[0] == '@' {
			continue
		}
		k, v, ok := strings.Cut(l, ";")
		if !ok {
			log.Fatalf("allkeys: %q", l)
		}
		var key []rune
		for _, f := range strings.Fields(k) {
			key = append(key, hex(f))
		}
		ces, variable := parseCEs(v)
		if len(ces) == 0 {
			log.Fatalf("allkeys: %q", l)
		}
		for _, p := range variable {
			r.variable[p] = true
		}
		for _, ce := range ces {
			r.primaries[ce.p] = true
		}
		r.entries[string(key)] = ces
		if len(key) > 1 {
			r.contractions[key[0]] = append(r.contractions[key[0]], string(key))
		}
	}
	for _, cs := range r.contractions {
		slices.Sort(cs)
	}
	parseFractional(r, rel.fractional)
	return r
}

var (
	fracLineRE   = regexp.MustCompile(`^([0-9A-F ]+);[^#]*#\s*(\S+)\s+(\S+)\s+((?:\[[0-9A-F.]+\])+)`)
	fracMarkerRE = regexp.MustCompile(`^FDD1 ([0-9A-F]+);[^#]*#\s*(.+?) first primary`)
)

// parseFractional reads the script groups, the implicit weights and the unified ideographs
// from FractionalUCA.txt, whose comments give the weights of allkeys_CLDR.txt.
func parseFractional(r *root, data []byte) {
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(nil, 1<<20)
	var pending, pendingKey string
	type run struct {
		lo, hi       rune
		base, second uint16
	}
	var runs []run
	for sc.Scan() {
		l := sc.Text()
		if s, ok := strings.CutPrefix(l, "[Unified_Ideograph "); ok {
			for f := range strings.FieldsSeq(strings.TrimSuffix(s, "]")) {
				lo, hi, ok := strings.Cut(f, "..")
				if !ok {
					hi = lo
				}
				r.unified = append(r.unified, hex(lo), hex(hi))
			}
			continue
		}
		if m := fracMarkerRE.FindStringSubmatch(l); m != nil {
			pending = m[2]
			pendingKey = string([]rune{0xFDD1, hex(m[1])})
			continue
		}
		m := fracLineRE.FindStringSubmatch(l)
		if m == nil || strings.HasPrefix(m[1], "FDD") {
			continue
		}
		cps := strings.Fields(m[1])
		ces, _ := parseCEs(strings.ReplaceAll(m[4], "[", "[."))
		if len(ces) == 0 {
			continue
		}
		if pending != "" && ces[0].p != 0 {
			r.boundaries[pendingKey] = ces[0].p
			if pending == "HAN" {
				r.boundaries[pendingKey] = lastRegularPrimary
			}
			var codes []string
			switch pending {
			case "SPACE":
				codes = []string{"space"}
			case "PUNCTUATION":
				codes = []string{"punct"}
			case "SYMBOL":
				codes = []string{"symbol"}
			case "CURRENCY":
				codes = []string{"currency"}
			case "DIGIT":
				codes = []string{"digit"}
			case "HAN":
				codes = []string{"Hani", "Hans", "Hant"}
			case "HIRAGANA", "KATAKANA":
				codes = []string{"Hira", "Kana", "Hrkt"}
			case "unassigned":
			default:
				if strings.HasPrefix(pending, "REORDER_RESERVED") {
					break
				}
				codes = []string{m[2]}
			}
			pending = ""
			switch {
			case codes == nil:
			case len(r.groups) > 0 && r.groups[len(r.groups)-1].start == ces[0].p:
				// Hiragana and Katakana share their primary weights.
				g := &r.groups[len(r.groups)-1]
				for _, c := range codes {
					if !slices.Contains(g.codes, c) {
						g.codes = append(g.codes, c)
					}
				}
			case codes[0] == "Hani":
				r.groups = append(r.groups, group{
					start: lastRegularPrimary,
					codes: codes,
				})
			default:
				r.groups = append(r.groups, group{
					start: ces[0].p,
					codes: codes,
				})
			}
		}
		if len(cps) == 1 && len(ces) == 2 && ces[0].p >= 0xFB00 && ces[0].p < 0xFB40 && ces[1].p&0x8000 != 0 {
			c := hex(cps[0])
			second := ces[1].p &^ 0x8000
			if n := len(runs); n > 0 && runs[n-1].hi+1 == c && runs[n-1].base == ces[0].p && runs[n-1].second+uint16(c-runs[n-1].lo) == second {
				runs[n-1].hi = c
				continue
			}
			runs = append(runs, run{
				lo:     c,
				hi:     c,
				base:   ces[0].p,
				second: second,
			})
		}
	}
	slices.SortFunc(runs, func(a, b run) int {
		return int(a.lo - b.lo)
	})
	for _, rn := range runs {
		r.implicits = append(r.implicits, implicit{
			lo:     rn.lo,
			hi:     rn.hi,
			base:   rn.base,
			offset: rn.second,
		})
	}
	pairs := make([][2]rune, 0, len(r.unified)/2)
	for i := 0; i < len(r.unified); i += 2 {
		pairs = append(pairs, [2]rune{r.unified[i], r.unified[i+1]})
	}
	slices.SortFunc(pairs, func(a, b [2]rune) int {
		return int(a[0] - b[0])
	})
	r.unified = r.unified[:0]
	for _, p := range pairs {
		r.unified = append(r.unified, p[0], p[1])
	}
	for i := range r.groups {
		if i+1 < len(r.groups) {
			r.groups[i].end = r.groups[i+1].start - 1
		} else {
			r.groups[i].end = hanEnd
		}
		if r.groups[i].end < r.groups[i].start {
			log.Fatalf("group %v is out of order", r.groups[i].codes)
		}
	}
	if len(r.groups) < 10 || r.groups[len(r.groups)-1].codes[0] != "Hani" || len(r.unified) == 0 || len(r.implicits) == 0 {
		log.Fatal("FractionalUCA.txt lacks the script groups, the implicit weights or the unified ideographs")
	}
	for _, im := range r.implicits {
		if im.base >= lastRegularPrimary {
			log.Fatal("an implicit base weight collides with lastRegularPrimary")
		}
	}
}

func (r *root) isUnified(c rune) bool {
	for i := 0; i < len(r.unified); i += 2 {
		if r.unified[i] <= c && c <= r.unified[i+1] {
			return true
		}
	}
	return false
}

// implicitCEs returns the CEs of a code point that the root does not list.
func (r *root) implicitCEs(c rune) []rootCE {
	for _, im := range r.implicits {
		if im.lo <= c && c <= im.hi {
			return []rootCE{{p: im.base, s: 0x20, t: 0x02}, {p: 0x8000 | (im.offset + uint16(c-im.lo))}}
		}
	}
	base := uint16(0xFBC0)
	if r.isUnified(c) {
		if 0x4E00 <= c && c <= 0x9FFF || 0xF900 <= c && c <= 0xFAFF {
			base = 0xFB40
		} else {
			base = 0xFB80
		}
	}
	return []rootCE{{p: base + uint16(c>>15), s: 0x20, t: 0x02}, {p: 0x8000 | uint16(c&0x7FFF)}}
}

// lookup returns the CEs of the NFD string s in the root.
func (r *root) lookup(s []rune) []rootCE {
	var ces []rootCE
	for i := 0; i < len(s); {
		n := 1
		for _, k := range r.contractions[s[i]] {
			kr := []rune(k)
			if len(kr) > n && len(kr) <= len(s)-i && string(s[i:i+len(kr)]) == k {
				n = len(kr)
			}
		}
		if e, ok := r.entries[string(s[i:i+n])]; ok {
			ces = append(ces, e...)
		} else {
			ces = append(ces, r.implicitCEs(s[i])...)
		}
		i += n
	}
	return ces
}

// isUpper reports whether a root tertiary weight is that of an uppercase form.
func isUpper(t uint16) bool {
	switch t {
	case 0x08, 0x09, 0x0A, 0x0B, 0x0C, 0x0E, 0x11, 0x12, 0x1D:
		return true
	}
	return false
}

// Nodes of a tailoring. The primary nodes form one list in weight order. Each primary node
// holds a list of secondary nodes, and each secondary node a list of tertiary nodes. A root
// node has the weight of the root; the weights of the other nodes are assigned in list order.

type pnode struct {
	root       bool
	w          uint16
	prev, next *pnode
	secs       *snode
	weight     uint32
}

type snode struct {
	root       bool
	w          uint16
	p          *pnode
	prev, next *snode
	ters       *tnode
	weight     uint32
}

type tnode struct {
	root       bool
	w          uint16
	s          *snode
	prev, next *tnode
	weight     uint16
}

// item is a tailored string.
type item struct {
	prefix, str []rune
	ces         []*tnode
}

func itemKey(prefix, str []rune) string {
	return string(prefix) + "\x00" + string(str)
}

type settings struct {
	backwards bool
	caseFirst string
	shifted   bool
	caseLevel bool
	reorder   []string
}

type builder struct {
	r     *root
	first *pnode
	// rootPrims are the root primary nodes in weight order.
	rootPrims []*pnode
	items     map[string]*item
	// maxLen is the longest number of code points of a tailored string.
	maxLen     int
	suppressed map[rune]bool
	settings   settings
}

func newBuilder(r *root) *builder {
	b := &builder{
		r:          r,
		items:      map[string]*item{},
		suppressed: map[rune]bool{},
	}
	b.first = &pnode{root: true}
	b.rootPrims = []*pnode{b.first}
	return b
}

// rootPrimary returns the root primary node of weight w, creating it.
func (b *builder) rootPrimary(w uint16) *pnode {
	i, ok := slices.BinarySearchFunc(b.rootPrims, w, func(p *pnode, w uint16) int {
		return int(p.w) - int(w)
	})
	if ok {
		return b.rootPrims[i]
	}
	// Insert before the next root node, after the tailored nodes of the previous one.
	prev := b.rootPrims[i-1]
	for prev.next != nil && !prev.next.root {
		prev = prev.next
	}
	n := &pnode{
		root: true,
		w:    w,
		prev: prev,
		next: prev.next,
	}
	if prev.next != nil {
		prev.next.prev = n
	}
	prev.next = n
	b.rootPrims = slices.Insert(b.rootPrims, i, n)
	return n
}

// rootSecondary returns the root secondary node of weight w under p, creating it.
func rootSecondary(p *pnode, w uint16) *snode {
	var prev *snode
	for n := p.secs; n != nil; n = n.next {
		if n.root {
			if n.w == w {
				return n
			}
			if n.w > w {
				break
			}
		}
		prev = n
	}
	// prev is the last node before the first root node of a greater weight. Tailored
	// nodes before that root node belong to it if a [before 2] put them there, so the
	// new node goes right after the last root node of a smaller weight and its tailored
	// followers.
	n := &snode{
		root: true,
		w:    w,
		p:    p,
	}
	insertSecondaryAfter(p, prev, n)
	return n
}

func insertSecondaryAfter(p *pnode, prev, n *snode) {
	n.p = p
	n.prev = prev
	if prev == nil {
		n.next = p.secs
		p.secs = n
	} else {
		n.next = prev.next
		prev.next = n
	}
	if n.next != nil {
		n.next.prev = n
	}
}

func rootTertiary(s *snode, w uint16) *tnode {
	var prev *tnode
	for n := s.ters; n != nil; n = n.next {
		if n.root {
			if n.w == w {
				return n
			}
			if n.w > w {
				break
			}
		}
		prev = n
	}
	n := &tnode{
		root: true,
		w:    w,
	}
	insertTertiaryAfter(s, prev, n)
	return n
}

func insertTertiaryAfter(s *snode, prev, n *tnode) {
	n.s = s
	n.prev = prev
	if prev == nil {
		n.next = s.ters
		s.ters = n
	} else {
		n.next = prev.next
		prev.next = n
	}
	if n.next != nil {
		n.next.prev = n
	}
}

// node returns the tertiary node of a root CE.
func (b *builder) node(ce rootCE) *tnode {
	return rootTertiary(rootSecondary(b.rootPrimary(ce.p), ce.s), ce.t)
}

// lookup returns the nodes of the CEs of the NFD string s in the tailoring as built so far.
func (b *builder) lookup(s []rune) []*tnode {
	var ces []*tnode
	for i := 0; i < len(s); {
		var it *item
		n := 0
		for l := min(b.maxLen, len(s)-i); l >= 1; l-- {
			if v, ok := b.items[itemKey(nil, s[i:i+l])]; ok {
				it, n = v, l
				break
			}
		}
		// A longer root contraction wins over a tailored string.
		rn := 0
		for _, k := range b.r.contractions[s[i]] {
			if b.suppressed[s[i]] {
				break
			}
			kr := []rune(k)
			if len(kr) > rn && len(kr) <= len(s)-i && string(s[i:i+len(kr)]) == k {
				rn = len(kr)
			}
		}
		if it != nil && n >= rn {
			ces = append(ces, it.ces...)
			i += n
			continue
		}
		if rn == 0 {
			rn = 1
		}
		rc, ok := b.r.entries[string(s[i:i+rn])]
		if !ok {
			rc = b.r.implicitCEs(s[i])
		}
		for _, ce := range rc {
			ces = append(ces, b.node(ce))
		}
		i += rn
	}
	return ces
}

// specialCE returns the root CE of a special reset position.
func (b *builder) specialCE(name string) rootCE {
	switch name {
	case "first tertiary ignorable", "last tertiary ignorable", "first secondary ignorable", "last secondary ignorable":
		return rootCE{}
	case "first primary ignorable", "last primary ignorable":
		var best rootCE
		found := false
		for _, ces := range b.r.entries {
			for _, ce := range ces {
				if ce.p != 0 || ce.s == 0 {
					continue
				}
				less := ce.s < best.s || ce.s == best.s && ce.t < best.t
				if !found || (name == "first primary ignorable") == less {
					best, found = ce, true
				}
			}
		}
		return best
	case "first variable", "last variable", "first regular":
		var best uint16
		found := false
		for _, ces := range b.r.entries {
			for _, ce := range ces {
				if ce.p == 0 || ce.p&0x8000 != 0 && ce.s == 0 {
					continue
				}
				switch name {
				case "first variable":
					if b.r.variable[ce.p] && (!found || ce.p < best) {
						best, found = ce.p, true
					}
				case "last variable":
					if b.r.variable[ce.p] && (!found || ce.p > best) {
						best, found = ce.p, true
					}
				case "first regular":
					if !b.r.variable[ce.p] && (!found || ce.p < best) {
						best, found = ce.p, true
					}
				}
			}
		}
		return rootCE{p: best, s: 0x20, t: 0x02}
	case "last regular":
		return rootCE{p: lastRegularPrimary, s: 0x20, t: 0x02}
	}
	log.Fatalf("unknown reset position [%s]", name)
	return rootCE{}
}

// anchor is the position after which a relation inserts a node.
type anchor struct {
	p *pnode
	s *snode
	t *tnode
}

func lastSecondary(p *pnode) *snode {
	if p.secs == nil {
		return rootSecondary(p, 0x20)
	}
	n := p.secs
	for n.next != nil {
		n = n.next
	}
	return n
}

func lastTertiary(s *snode) *tnode {
	if s.ters == nil {
		return rootTertiary(s, 0x02)
	}
	n := s.ters
	for n.next != nil {
		n = n.next
	}
	return n
}

// before returns the anchor right before the node of t at level n, for [before n].
func (b *builder) before(t *tnode, n int) anchor {
	switch n {
	case 1:
		p := t.s.p
		if p.root && p.w > 0 {
			b.rootPrimary(p.w - 1)
		}
		q := p.prev
		if q == nil {
			log.Fatal("[before 1] of the first primary")
		}
		s := lastSecondary(q)
		return anchor{
			p: q,
			s: s,
			t: lastTertiary(s),
		}
	case 2:
		s := t.s
		if s.root && s.w > 0 {
			rootSecondary(s.p, s.w-1)
		}
		q := s.prev
		if q == nil {
			q = &snode{
				root: true,
			}
			insertSecondaryAfter(s.p, nil, q)
		}
		return anchor{
			p: s.p,
			s: q,
			t: lastTertiary(q),
		}
	case 3:
		if t.root && t.w > 0 {
			rootTertiary(t.s, t.w-1)
		}
		q := t.prev
		if q == nil {
			q = &tnode{
				root: true,
			}
			insertTertiaryAfter(t.s, nil, q)
		}
		return anchor{
			p: t.s.p,
			s: t.s,
			t: q,
		}
	}
	log.Fatalf("[before %d]", n)
	return anchor{}
}

// insert inserts a new node after a at the given strength and returns its tertiary node.
func (b *builder) insert(a anchor, strength int) *tnode {
	switch strength {
	case 1:
		p := &pnode{
			prev: a.p,
			next: a.p.next,
		}
		if a.p.next != nil {
			a.p.next.prev = p
		}
		a.p.next = p
		s := &snode{
			root: true,
			w:    0x20,
		}
		insertSecondaryAfter(p, nil, s)
		t := &tnode{
			root: true,
			w:    0x02,
		}
		insertTertiaryAfter(s, nil, t)
		return t
	case 2:
		s := &snode{}
		insertSecondaryAfter(a.p, a.s, s)
		t := &tnode{
			root: true,
			w:    0x02,
		}
		insertTertiaryAfter(s, nil, t)
		return t
	case 3:
		t := &tnode{}
		insertTertiaryAfter(a.s, a.t, t)
		return t
	}
	log.Fatalf("strength %d", strength)
	return nil
}

func anchorOf(t *tnode) anchor {
	return anchor{
		p: t.s.p,
		s: t.s,
		t: t,
	}
}

// rule is a reset, a relation or a setting of the rules of a tailoring.
type rule struct {
	// op is '&' for a reset, '<' for a relation and '[' for a setting.
	op byte
	// before is n of [before n] of a reset.
	before int
	// special is the special position of a reset, such as "last regular".
	special string
	// strength is the strength of a relation: 1 to 4, or 0 for identity.
	strength          int
	prefix, str, ext  string
	setting, argument string
}

// parseRules parses the rules of a tailoring.
func parseRules(src string) []rule {
	p := &ruleParser{
		src: []rune(src),
	}
	var rules []rule
	for {
		p.skipSpace()
		if p.i >= len(p.src) {
			return rules
		}
		switch c := p.src[p.i]; c {
		case '[':
			s := p.bracket()
			name, arg, _ := strings.Cut(s, " ")
			rules = append(rules, rule{
				op:       '[',
				setting:  name,
				argument: strings.TrimSpace(arg),
			})
		case '&':
			p.i++
			r := rule{
				op: '&',
			}
			p.skipSpace()
			if p.peek() == '[' {
				s := p.bracket()
				if n, ok := strings.CutPrefix(s, "before "); ok {
					r.before = int(n[0] - '0')
					p.skipSpace()
					if p.peek() == '[' {
						r.special = p.bracket()
					}
				} else {
					r.special = s
				}
			}
			if r.special == "" {
				p.skipSpace()
				r.str = p.text()
			}
			rules = append(rules, r)
		case '<', '=':
			strength := 0
			if c == '<' {
				for p.peek() == '<' {
					strength++
					p.i++
				}
			} else {
				p.i++
			}
			if p.peek() == '*' {
				p.i++
				p.skipSpace()
				var prev rune = -1
				for _, c := range p.text() {
					rules = append(rules, rule{
						op:       '<',
						strength: strength,
						str:      string(c),
					})
					prev = c
				}
				// Ranges: a hyphen between two characters of a starred list.
				for {
					p.skipSpace()
					if p.peek() != '-' {
						break
					}
					p.i++
					s := []rune(p.text())
					if prev < 0 || len(s) == 0 {
						log.Fatal("a range without a start or an end")
					}
					for c := prev + 1; c <= s[0]; c++ {
						rules = append(rules, rule{
							op:       '<',
							strength: strength,
							str:      string(c),
						})
					}
					for _, c := range s[1:] {
						rules = append(rules, rule{
							op:       '<',
							strength: strength,
							str:      string(c),
						})
					}
					prev = s[len(s)-1]
				}
				continue
			}
			r := rule{
				op:       '<',
				strength: strength,
			}
			p.skipSpace()
			r.str = p.text()
			p.skipSpace()
			if p.peek() == '|' {
				p.i++
				p.skipSpace()
				r.prefix = r.str
				r.str = p.text()
				p.skipSpace()
			}
			if p.peek() == '/' {
				p.i++
				p.skipSpace()
				r.ext = p.text()
			}
			rules = append(rules, r)
		default:
			log.Fatalf("unexpected %q in rules at %d: %q", c, p.i, string(p.src[max(0, p.i-20):min(len(p.src), p.i+20)]))
		}
	}
}

type ruleParser struct {
	src []rune
	i   int
}

func (p *ruleParser) peek() rune {
	if p.i < len(p.src) {
		return p.src[p.i]
	}
	return -1
}

func (p *ruleParser) skipSpace() {
	for p.i < len(p.src) {
		c := p.src[p.i]
		switch {
		case c == '#':
			for p.i < len(p.src) && p.src[p.i] != '\n' && p.src[p.i] != '\r' {
				p.i++
			}
		case unicode.IsSpace(c) || unicode.Is(unicode.Pattern_White_Space, c):
			p.i++
		default:
			return
		}
	}
}

// bracket returns the contents of a bracketed setting, with nested brackets kept.
func (p *ruleParser) bracket() string {
	start := p.i + 1
	depth := 0
	for ; p.i < len(p.src); p.i++ {
		switch p.src[p.i] {
		case '\\':
			p.i++
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				s := string(p.src[start:p.i])
				p.i++
				return strings.Join(strings.Fields(s), " ")
			}
		}
	}
	log.Fatal("unterminated [")
	return ""
}

func isSyntax(c rune) bool {
	switch c {
	case '&', '<', '=', '|', '/', '[', ']', '#', '*', '-':
		return true
	}
	return unicode.IsSpace(c) || unicode.Is(unicode.Pattern_White_Space, c)
}

// text reads a string of the rules: literal characters, quoted text and escapes.
func (p *ruleParser) text() string {
	var b strings.Builder
	for p.i < len(p.src) {
		c := p.src[p.i]
		switch {
		case c == '\'':
			p.i++
			if p.peek() == '\'' {
				b.WriteRune('\'')
				p.i++
				continue
			}
			for p.i < len(p.src) {
				c := p.src[p.i]
				p.i++
				if c == '\'' {
					if p.peek() == '\'' {
						b.WriteRune('\'')
						p.i++
						continue
					}
					break
				}
				if c == '\\' {
					p.i--
					b.WriteRune(p.escape())
					continue
				}
				b.WriteRune(c)
			}
		case c == '\\':
			b.WriteRune(p.escape())
		case isSyntax(c):
			return b.String()
		default:
			b.WriteRune(c)
			p.i++
		}
	}
	return b.String()
}

// escape reads a backslash escape.
func (p *ruleParser) escape() rune {
	p.i++
	if p.i >= len(p.src) {
		log.Fatal("a backslash at the end of the rules")
	}
	c := p.src[p.i]
	p.i++
	digits := func(n int) rune {
		if p.i+n > len(p.src) {
			log.Fatal("a short escape")
		}
		v := hex(string(p.src[p.i : p.i+n]))
		p.i += n
		return v
	}
	switch c {
	case 'u':
		return digits(4)
	case 'U':
		return digits(8)
	case 'x':
		if p.peek() == '{' {
			end := slices.Index(p.src[p.i:], '}')
			v := hex(string(p.src[p.i+1 : p.i+end]))
			p.i += end + 1
			return v
		}
		return digits(2)
	case 't':
		return '\t'
	case 'n':
		return '\n'
	case 'r':
		return '\r'
	}
	return c
}

// parseSet parses the code points of a simple UnicodeSet such as "[a-c d เ]".
func parseSet(s string) []rune {
	p := &ruleParser{
		src: []rune(strings.TrimSuffix(strings.TrimPrefix(s, "["), "]")),
	}
	var cs []rune
	for {
		p.skipSpace()
		if p.i >= len(p.src) {
			return cs
		}
		var c rune
		if p.peek() == '\\' {
			c = p.escape()
		} else {
			c = p.src[p.i]
			p.i++
		}
		if p.peek() == '-' {
			p.i++
			var d rune
			if p.peek() == '\\' {
				d = p.escape()
			} else {
				d = p.src[p.i]
				p.i++
			}
			for x := c; x <= d; x++ {
				cs = append(cs, x)
			}
			continue
		}
		cs = append(cs, c)
	}
}

func nfd(s string) []rune {
	return []rune(unorm.String(unorm.NFD, s, nil))
}

// tailoringSource is the rules of a tailoring by its locale file and type.
type tailoringSource struct {
	file, typ string
}

// apply applies the rules of a tailoring, with its imports.
func (b *builder) apply(rules []rule, sources map[tailoringSource]string, depth int) {
	if depth > 8 {
		log.Fatal("too deep imports")
	}
	var reset []*tnode
	var a anchor
	for _, r := range rules {
		switch r.op {
		case '[':
			switch r.setting {
			case "import":
				src, ok := importSource(r.argument, sources)
				if !ok {
					log.Fatalf("unknown import %q", r.argument)
				}
				b.apply(parseRules(src), sources, depth+1)
			case "reorder":
				b.settings.reorder = strings.Fields(r.argument)
			case "backwards":
				b.settings.backwards = r.argument == "2"
			case "caseFirst":
				b.settings.caseFirst = r.argument
			case "alternate":
				b.settings.shifted = r.argument == "shifted"
			case "caseLevel":
				b.settings.caseLevel = r.argument == "on"
			case "suppressContractions":
				for _, c := range parseSet(r.argument) {
					b.suppressed[c] = true
				}
			case "normalization", "strength", "optimize":
			default:
				log.Fatalf("unknown setting [%s %s]", r.setting, r.argument)
			}
		case '&':
			var t *tnode
			if p, ok := b.r.boundaries[r.str]; ok {
				// The first primary weight of a group sorts right before the first
				// character of the group.
				t = b.node(rootCE{
					p: p,
					s: 0x20,
					t: 0x02,
				})
				reset = []*tnode{t}
				a = b.before(t, 1)
				continue
			}
			if r.special != "" {
				t = b.node(b.specialCE(r.special))
				reset = []*tnode{t}
			} else {
				reset = b.lookup(nfd(r.str))
				if len(reset) == 0 {
					log.Fatalf("reset to an ignorable string %q", r.str)
				}
				t = reset[len(reset)-1]
			}
			a = anchorOf(t)
			if r.before > 0 {
				a = b.before(t, r.before)
			}
		case '<':
			if reset == nil {
				log.Fatal("a relation before any reset")
			}
			ces := slices.Clone(reset)
			if r.strength >= 1 && r.strength <= 3 {
				t := b.insert(a, r.strength)
				ces[len(ces)-1] = t
				a = anchorOf(t)
			}
			reset = slices.Clone(ces)
			if r.ext != "" {
				ces = append(ces, b.lookup(nfd(r.ext))...)
			}
			prefix, str := nfd(r.prefix), nfd(r.str)
			if len(str) == 0 {
				log.Fatal("an empty relation string")
			}
			b.items[itemKey(prefix, str)] = &item{
				prefix: prefix,
				str:    str,
				ces:    ces,
			}
			b.maxLen = max(b.maxLen, len(str))
		}
	}
}

// typeNames maps the BCP 47 names of collation types to the names of the XML data.
var typeNames = map[string]string{
	"phonebk": "phonebook",
	"trad":    "traditional",
	"dict":    "dictionary",
	"gb2312":  "gb2312han",
}

func importSource(arg string, sources map[tailoringSource]string) (string, bool) {
	loc, typ, ok := strings.Cut(arg, "-u-co-")
	if !ok {
		typ = "standard"
	}
	if n, ok := typeNames[typ]; ok {
		typ = n
	}
	file := strings.ReplaceAll(loc, "-", "_")
	if file == "und" {
		file = "root"
	}
	s, ok := sources[tailoringSource{
		file: file,
		typ:  typ,
	}]
	return s, ok
}

// assignWeights assigns the weights of the tailored nodes.
func (b *builder) assignWeights() {
	var base uint32
	k := uint32(0)
	for p := b.first; p != nil; p = p.next {
		if p.root {
			base, k = uint32(p.w)<<15, 0
			p.weight = base
		} else {
			k++
			if k > 0x7FFF {
				// Continue after the next root weight if the root does not use it.
				next := uint16(base>>15) + 1
				if b.r.primaries[next] || next >= 0xFB40 || p.next != nil && p.next.root && p.next.w <= next {
					log.Fatal("too many tailored primary weights after one root weight")
				}
				base, k = uint32(next)<<15, 1
			}
			p.weight = base | k
		}
		var sbase uint32
		sk := uint32(0)
		for s := p.secs; s != nil; s = s.next {
			if s.root {
				sbase, sk = uint32(s.w)<<11, 0
				s.weight = sbase
			} else {
				sk++
				if sk > 0x7FF {
					log.Fatal("too many tailored secondary weights after one root weight")
				}
				s.weight = sbase | sk
			}
			var tbase uint16
			tk := uint16(0)
			for t := s.ters; t != nil; t = t.next {
				if t.root {
					tbase, tk = t.w<<6, 0
					t.weight = tbase
				} else {
					tk++
					if tk > 0x3F {
						log.Fatal("too many tailored tertiary weights after one root weight")
					}
					t.weight = tbase | tk
				}
			}
		}
	}
}

// tailoredCE is a CE of a tailoring.
type tailoredCE struct {
	p uint32
	s uint32
	t uint16
	c uint8
}

func (b *builder) ce(t *tnode) tailoredCE {
	return tailoredCE{
		p: t.s.p.weight,
		s: t.s.weight,
		t: t.weight,
	}
}

// entry is a string of a tailoring with its CEs.
type entry struct {
	prefix, str []rune
	ces         []tailoredCE
}

// caseOf returns the case of the root CEs ces: 0 lower, 1 mixed, 2 upper.
func caseOf(ces []rootCE) uint8 {
	upper, lower := false, false
	for _, ce := range ces {
		if ce.p == 0 {
			continue
		}
		if isUpper(ce.t) {
			upper = true
		} else {
			lower = true
		}
	}
	switch {
	case upper && lower:
		return 1
	case upper:
		return 2
	}
	return 0
}

// entries returns the entries of the tailoring.
func (b *builder) entries() []entry {
	b.assignWeights()
	var es []entry
	starters := map[rune]bool{}
	for _, it := range b.items {
		e := entry{
			prefix: it.prefix,
			str:    it.str,
		}
		for _, t := range it.ces {
			e.ces = append(e.ces, b.ce(t))
		}
		// The case of the primary CEs comes from the root CEs of the string.
		rc := b.r.lookup(it.str)
		var prim []int
		for i, ce := range e.ces {
			if ce.p != 0 {
				prim = append(prim, i)
			}
		}
		var rprim []rootCE
		for _, ce := range rc {
			if ce.p != 0 {
				rprim = append(rprim, ce)
			}
		}
		for j, i := range prim {
			switch {
			case j < len(prim)-1 && j < len(rprim):
				e.ces[i].c = caseOf(rprim[j : j+1])
			case j < len(rprim):
				e.ces[i].c = caseOf(rprim[j:])
			}
		}
		es = append(es, e)
		starters[it.str[0]] = true
	}
	for c := range b.suppressed {
		starters[c] = true
	}
	// A starter that the tailoring changes keeps the root contractions that start with it,
	// unless it suppresses them.
	for c := range starters {
		if !b.suppressed[c] {
			for _, k := range b.r.contractions[c] {
				kr := []rune(k)
				if _, ok := b.items[itemKey(nil, kr)]; ok {
					continue
				}
				es = append(es, b.rootEntry(kr, b.r.entries[k]))
			}
		}
		if _, ok := b.items[itemKey(nil, []rune{c})]; !ok && b.suppressed[c] {
			rc, ok := b.r.entries[string(c)]
			if !ok {
				rc = b.r.implicitCEs(c)
			}
			es = append(es, b.rootEntry([]rune{c}, rc))
		}
	}
	slices.SortFunc(es, func(a, b entry) int {
		if c := slices.Compare(a.str, b.str); c != 0 {
			return c
		}
		return slices.Compare(a.prefix, b.prefix)
	})
	return es
}

func (b *builder) rootEntry(str []rune, rc []rootCE) entry {
	e := entry{
		str: str,
	}
	for _, ce := range rc {
		var c uint8
		if ce.p != 0 && isUpper(ce.t) {
			c = 2
		}
		e.ces = append(e.ces, tailoredCE{
			p: uint32(ce.p) << 15,
			s: uint32(ce.s) << 11,
			t: ce.t << 6,
			c: c,
		})
	}
	return e
}

// reorderRanges returns the ranges of root primary weights that the reordering codes move,
// as (first, last, new first) triples.
func reorderRanges(groups []group, codes []string) []uint16 {
	if len(codes) == 0 {
		return nil
	}
	isSpecial := func(g group) bool {
		switch g.codes[0] {
		case "space", "punct", "symbol", "currency", "digit":
			return true
		}
		return false
	}
	placed := make([]bool, len(groups))
	var order []int
	listed := func(g group) bool {
		for _, c := range codes {
			if c == "others" || c == "Zzzz" {
				continue
			}
			if slices.Contains(g.codes, c) {
				return true
			}
		}
		return false
	}
	for i, g := range groups {
		if isSpecial(g) && !listed(g) {
			order = append(order, i)
			placed[i] = true
		}
	}
	others := func() {
		for i := range groups {
			if !placed[i] && !listed(groups[i]) {
				order = append(order, i)
				placed[i] = true
			}
		}
	}
	for _, c := range codes {
		if c == "others" || c == "Zzzz" {
			others()
			continue
		}
		found := false
		for i, g := range groups {
			if slices.Contains(g.codes, c) {
				found = true
				if !placed[i] {
					order = append(order, i)
					placed[i] = true
				}
			}
		}
		if !found {
			log.Fatalf("unknown reordering code %s", c)
		}
	}
	for i := range groups {
		if !placed[i] {
			order = append(order, i)
		}
	}
	type move struct {
		first, last, to uint16
	}
	var moves []move
	pos := groups[0].start
	for _, i := range order {
		g := groups[i]
		if pos != g.start {
			moves = append(moves, move{
				first: g.start,
				last:  g.end,
				to:    pos,
			})
		}
		pos += g.end - g.start + 1
	}
	slices.SortFunc(moves, func(a, b move) int {
		return int(a.first) - int(b.first)
	})
	var out []uint16
	for i, m := range moves {
		if n := len(out); i > 0 && out[n-2]+1 == m.first && int(out[n-1])-int(out[n-3]) == int(m.to)-int(m.first) {
			out[n-2] = m.last
			continue
		}
		out = append(out, m.first, m.last, m.to)
	}
	return out
}

// encodeTailoring encodes the settings and the entries of a tailoring:
//
//	flags (uvarint): 1 backwards secondary, 2 caseFirst upper, 4 caseFirst lower,
//	    8 caseFirst off, 16 alternate shifted, 32 caseLevel
//	number of reorder ranges, then (first, last, new first) for each
//	number of entries, then for each entry:
//	    number of prefix code points, the code points
//	    number of code points, the code points
//	    number of CEs, then for each CE: the zigzag difference of the primary from the
//	        previous primary, the secondary, and the tertiary shifted left by 2 with the case
func encodeTailoring(s settings, reorder []uint16, es []entry) []byte {
	var b []byte
	u := func(v uint64) {
		b = binary.AppendUvarint(b, v)
	}
	var flags uint64
	if s.backwards {
		flags |= 1
	}
	switch s.caseFirst {
	case "upper":
		flags |= 2
	case "lower":
		flags |= 4
	case "off":
		flags |= 8
	case "":
	default:
		log.Fatalf("caseFirst %s", s.caseFirst)
	}
	if s.shifted {
		flags |= 16
	}
	if s.caseLevel {
		flags |= 32
	}
	u(flags)
	u(uint64(len(reorder) / 3))
	for _, v := range reorder {
		u(uint64(v))
	}
	u(uint64(len(es)))
	var prev uint32
	for _, e := range es {
		u(uint64(len(e.prefix)))
		for _, c := range e.prefix {
			u(uint64(c))
		}
		u(uint64(len(e.str)))
		for _, c := range e.str {
			u(uint64(c))
		}
		u(uint64(len(e.ces)))
		for _, ce := range e.ces {
			d := int64(ce.p) - int64(prev)
			u(uint64(d<<1) ^ uint64(d>>63))
			prev = ce.p
			u(uint64(ce.s))
			u(uint64(ce.t)<<2 | uint64(ce.c))
		}
	}
	return b
}

func deflate(data []byte) []byte {
	var buf bytes.Buffer
	w, err := flate.NewWriter(&buf, flate.BestCompression)
	if err != nil {
		log.Fatal(err)
	}
	if _, err := w.Write(data); err != nil {
		log.Fatal(err)
	}
	if err := w.Close(); err != nil {
		log.Fatal(err)
	}
	return buf.Bytes()
}

// rootValue encodes the root CEs of a string, appending expansions to exps.
func rootValue(ces []rootCE, exps *[]uint32) uint32 {
	enc := func(ce rootCE) uint32 {
		if ce.s > 0x1FF || ce.t > 0x1F {
			log.Fatalf("a root weight does not fit: %v", ce)
		}
		return uint32(ce.p)<<16 | uint32(ce.s)<<7 | uint32(ce.t)<<2
	}
	if len(ces) == 1 {
		if v := enc(ces[0]); v != 0 {
			return v
		}
		return 3
	}
	// Reuse an expansion that is already there.
	want := make([]uint32, len(ces))
	for i, ce := range ces {
		want[i] = enc(ce)
	}
	idx := -1
	for i := 0; i+len(want) <= len(*exps); i++ {
		if slices.Equal((*exps)[i:i+len(want)], want) {
			idx = i
			break
		}
	}
	if idx < 0 {
		idx = len(*exps)
		*exps = append(*exps, want...)
	}
	if idx >= 1<<22 || len(want) > 0xFF {
		log.Fatal("an expansion does not fit")
	}
	return uint32(idx)<<10 | uint32(len(want))<<2 | 1
}

func main() {
	flag.Parse()
	rel := readRelease()
	r := parseRoot(rel)
	copyright, err := cldrgen.Copyright(rel.license, "../LICENSE", "NOTICE")
	if err != nil {
		log.Fatal(err)
	}

	// The root trie: stage 1 by code point >> 10, stage 2 blocks of 32 entries by
	// (code point >> 5) & 31, and leaves of 32 values.
	var exps []uint32
	var contrs []uint32
	values := make([]uint32, 0x110000)
	for c := rune(0); c < 0x110000; c++ {
		e, ok := r.entries[string(c)]
		cs := r.contractions[c]
		if !ok && len(cs) == 0 {
			continue
		}
		var v uint32
		if ok {
			v = rootValue(e, &exps)
		}
		if len(cs) == 0 {
			values[c] = v
			continue
		}
		idx := len(contrs)
		contrs = append(contrs, v, uint32(len(cs)))
		for _, k := range cs {
			kr := []rune(k)[1:]
			contrs = append(contrs, uint32(len(kr)))
			for _, x := range kr {
				contrs = append(contrs, uint32(x))
			}
			contrs = append(contrs, rootValue(r.entries[k], &exps))
		}
		values[c] = uint32(idx)<<2 | 2
	}
	var leaves [][32]uint32
	leafIndex := map[[32]uint32]uint16{}
	var mids [][32]uint16
	midIndex := map[[32]uint16]uint16{}
	var stage1 []uint16
	for hi := 0; hi < 0x110000; hi += 1 << 10 {
		var mid [32]uint16
		for m := range 32 {
			var leaf [32]uint32
			copy(leaf[:], values[hi+m<<5:hi+m<<5+32])
			li, ok := leafIndex[leaf]
			if !ok {
				li = uint16(len(leaves))
				leaves = append(leaves, leaf)
				leafIndex[leaf] = li
			}
			mid[m] = li
		}
		mi, ok := midIndex[mid]
		if !ok {
			mi = uint16(len(mids))
			mids = append(mids, mid)
			midIndex[mid] = mi
		}
		stage1 = append(stage1, mi)
	}
	var bin []byte
	for _, v := range stage1 {
		bin = binary.LittleEndian.AppendUint16(bin, v)
	}
	midOff := len(bin)
	for _, m := range mids {
		for _, v := range m {
			bin = binary.LittleEndian.AppendUint16(bin, v)
		}
	}
	leafOff := len(bin)
	for _, l := range leaves {
		for _, v := range l {
			bin = binary.LittleEndian.AppendUint32(bin, v)
		}
	}
	expOff := len(bin)
	for _, v := range exps {
		bin = binary.LittleEndian.AppendUint32(bin, v)
	}
	contrOff := len(bin)
	for _, v := range contrs {
		bin = binary.LittleEndian.AppendUint32(bin, v)
	}
	if err := os.WriteFile("root.bin", bin, 0o644); err != nil {
		log.Fatal(err)
	}

	// Tailorings.
	sources := map[tailoringSource]string{}
	defaults := map[string]string{}
	for name, data := range rel.collation {
		file := strings.TrimSuffix(name, ".xml")
		var doc struct {
			Default    string `xml:"collations>defaultCollation"`
			Collations []struct {
				Type string `xml:"type,attr"`
				Alt  string `xml:"alt,attr"`
				CR   string `xml:"cr"`
			} `xml:"collations>collation"`
		}
		if err := xml.Unmarshal(data, &doc); err != nil {
			log.Fatalf("%s: %v", name, err)
		}
		for _, c := range doc.Collations {
			if c.Alt != "" {
				continue
			}
			sources[tailoringSource{
				file: file,
				typ:  c.Type,
			}] = c.CR
		}
		if doc.Default != "" && doc.Default != "standard" {
			defaults[tag(file)] = bcp47Type(doc.Default)
		}
	}
	type tailoring struct {
		key  string
		data []byte
	}
	var tailorings []tailoring
	keys := slices.SortedFunc(func(yield func(tailoringSource) bool) {
		for k := range sources {
			if !yield(k) {
				return
			}
		}
	}, func(a, b tailoringSource) int {
		return strings.Compare(a.file+"/"+a.typ, b.file+"/"+b.typ)
	})
	for _, k := range keys {
		if strings.HasPrefix(k.typ, "private-") || strings.TrimSpace(sources[k]) == "" {
			continue
		}
		b := newBuilder(r)
		b.apply(parseRules(sources[k]), sources, 0)
		es := b.entries()
		data := encodeTailoring(b.settings, reorderRanges(r.groups, b.settings.reorder), es)
		tailorings = append(tailorings, tailoring{
			key:  tag(k.file) + "/" + bcp47Type(k.typ),
			data: deflate(data),
		})
	}
	slices.SortFunc(tailorings, func(a, b tailoring) int {
		return strings.Compare(a.key, b.key)
	})
	var tdata []byte
	var tkeys []string
	var tends []string
	for _, t := range tailorings {
		tdata = append(tdata, t.data...)
		tkeys = append(tkeys, strconv.Quote(t.key))
		tends = append(tends, strconv.Itoa(len(tdata)))
	}
	if err := os.WriteFile("tailorings.deflate", tdata, 0o644); err != nil {
		log.Fatal(err)
	}

	var src bytes.Buffer
	fmt.Fprintf(&src, "// SPDX-License-Identifier: Unicode-3.0\n// SPDX-FileCopyrightText: %s\n\n", copyright)
	fmt.Fprintf(&src, "// Code generated by gen.go from the CLDR %s XML data. DO NOT EDIT.\n\npackage collation\n\n", xmlVersion)
	src.WriteString("import _ \"embed\"\n\n")
	src.WriteString("// Root holds the root collation in little-endian words: the uint16 stage 1 of its trie,\n")
	src.WriteString("// indexed by a code point shifted right by 10, at offset 0; the uint16 stage 2 blocks of\n")
	src.WriteString("// 32 entries at RootStage2; the uint32 leaves of 32 values at RootLeaves; the uint32 CEs\n")
	src.WriteString("// of expansions at RootExpansions; and the uint32 contractions at RootContractions. A\n")
	src.WriteString("// contraction is the value of its first code point alone, the number of its strings, and\n")
	src.WriteString("// for each string the number of its other code points, the code points, and the value.\n")
	src.WriteString("//\n//go:embed root.bin\nvar Root string\n\n")
	fmt.Fprintf(&src, "// Offsets in Root.\nconst (\n\tRootStage2 = %d\n\tRootLeaves = %d\n\tRootExpansions = %d\n\tRootContractions = %d\n)\n\n", midOff, leafOff, expOff, contrOff)
	src.WriteString("// Tailorings holds the tailorings, each a raw DEFLATE stream.\n//\n//go:embed tailorings.deflate\nvar Tailorings string\n\n")
	src.WriteString("// TailoringKeys are the tailorings as \"locale/type\", sorted, with \"und\" for the root\n// locale and the BCP 47 names of the types.\n")
	fmt.Fprintf(&src, "var TailoringKeys = []string{%s}\n\n", strings.Join(tkeys, ", "))
	src.WriteString("// TailoringEnds are the end offsets of the tailorings in Tailorings.\n")
	fmt.Fprintf(&src, "var TailoringEnds = []uint32{%s}\n\n", strings.Join(tends, ", "))
	src.WriteString("// DefaultTypes maps locales to their default collation types other than \"standard\".\n")
	src.WriteString("var DefaultTypes = map[string]string{\n")
	for _, k := range slices.Sorted(func(yield func(string) bool) {
		for k := range defaults {
			if !yield(k) {
				return
			}
		}
	}) {
		fmt.Fprintf(&src, "\t%q: %q,\n", k, defaults[k])
	}
	src.WriteString("}\n\n")

	// Variable tops and the digit weight.
	tops := map[string]uint16{}
	bottom := r.groups[0].start
	for _, g := range r.groups {
		switch c := g.codes[0]; c {
		case "space", "punct", "symbol", "currency":
			tops[c] = g.end
		}
	}
	fmt.Fprintf(&src, "// MergeSeparator is the primary weight of U+FFFE.\nconst MergeSeparator = 0x%04X\n\n", r.entries["\uFFFE"][0].p)
	zero := r.entries["0"]
	fmt.Fprintf(&src, "// FirstVariable is the first primary weight of the space group, the first group that can\n// be variable.\nconst FirstVariable = 0x%04X\n\n", bottom)
	src.WriteString("// The last primary weights of the groups that maxVariable can name.\nconst (\n")
	fmt.Fprintf(&src, "\tSpaceTop = 0x%04X\n\tPunctTop = 0x%04X\n\tSymbolTop = 0x%04X\n\tCurrencyTop = 0x%04X\n)\n\n", tops["space"], tops["punct"], tops["symbol"], tops["currency"])
	fmt.Fprintf(&src, "// DigitZero is the primary weight of the digit zero.\nconst DigitZero = 0x%04X\n\n", zero[0].p)
	for p := range r.variable {
		if p > tops["punct"] || p < bottom {
			log.Fatalf("variable primary %04X is above the punctuation group", p)
		}
	}

	src.WriteString("// Implicits are the ranges of code points whose CEs are a base primary weight followed by\n")
	src.WriteString("// a second primary weight, as (first, last, base, offset of the second weight) quadruples.\n")
	src.WriteString("var Implicits = []uint32{\n")
	for _, im := range r.implicits {
		fmt.Fprintf(&src, "\t0x%X, 0x%X, 0x%04X, 0x%04X,\n", im.lo, im.hi, im.base, im.offset)
	}
	src.WriteString("}\n\n")
	src.WriteString("// UnifiedIdeographs are the ranges of the code points with Unified_Ideograph, as pairs.\n")
	src.WriteString("var UnifiedIdeographs = []rune{\n")
	for i := 0; i < len(r.unified); i += 2 {
		fmt.Fprintf(&src, "\t0x%X, 0x%X,\n", r.unified[i], r.unified[i+1])
	}
	src.WriteString("}\n")
	out, err := format.Source(src.Bytes())
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile("tables.go", out, 0o644); err != nil {
		log.Fatal(err)
	}
	log.Printf("root %d bytes (%d leaves, %d mids, %d expansion words, %d contraction words), %d tailorings in %d bytes",
		len(bin), len(leaves), len(mids), len(exps), len(contrs), len(tailorings), len(tdata))
}

// tag returns the BCP 47 tag of a locale file name.
func tag(file string) string {
	if file == "root" {
		return "und"
	}
	return strings.ReplaceAll(file, "_", "-")
}

func bcp47Type(typ string) string {
	for k, v := range typeNames {
		if v == typ {
			return k
		}
	}
	return typ
}
