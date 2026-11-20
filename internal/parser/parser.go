// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

// Package parser parses ECMAScript source text into the syntax tree of package ast.
package parser

import (
	"math"
	"slices"
	"strconv"
	"unicode/utf8"

	"github.com/hajimehoshi/ekuma/internal/ast"
	"github.com/hajimehoshi/ekuma/internal/srcpos"
)

// Options describes the context that source text is parsed in.
type Options struct {
	// Strict makes the code strict mode code from its start.
	Strict bool

	// AllowSuperProperty permits super property accesses at the top level, as in a direct eval inside a method.
	AllowSuperProperty bool

	// AllowSuperCall permits super calls at the top level, as in a direct eval inside a derived constructor.
	AllowSuperCall bool

	// AllowNewTarget permits new.target at the top level, as in a direct eval inside a function.
	AllowNewTarget bool

	// InClassFieldInit makes references to arguments an error, as in a direct eval inside a class field initializer.
	InClassFieldInit bool

	// PrivateNames lists the private names, without '#', declared by the classes enclosing a direct eval.
	PrivateNames []string

	// Poll, when not nil, is called now and then during a long parse, and may stop it by
	// panicking.
	Poll func()
}

// pollTokens is the number of tokens between two calls of [Options.Poll].
const pollTokens = 1 << 14

// FunctionKind is the kind of function that [ParseFunction] creates.
type FunctionKind uint8

const (
	NormalFunction FunctionKind = iota
	GeneratorFunction
	AsyncFunction
	AsyncGeneratorFunction
)

// Error is a syntax error.
type Error struct {
	// Offset is the byte offset of the error in the source text.
	Offset int

	// Line is the 1-based line of the error.
	Line int

	// Column is the 1-based column of the error, counted in code points.
	Column int

	Message string
}

// Error returns the position and the message of the error.
func (e *Error) Error() string {
	return "SyntaxError: " + e.Message + " (" + strconv.Itoa(e.Line) + ":" + strconv.Itoa(e.Column) + ")"
}

// ParseScript parses src as a Script. The returned error is an [*Error].
func ParseScript(src string, opts Options) (*ast.Program, error) {
	return parseProgram(src, &opts, false)
}

// ParseModule parses src as a Module. The returned error is an [*Error].
func ParseModule(src string, opts Options) (*ast.Program, error) {
	return parseProgram(src, &opts, true)
}

// ParseFunction parses a function given as separate parameter text and body text,
// as the Function constructor and its generator and async variants require.
//
// The source of the returned program is the synthesized function text.
// Its body is one expression statement holding the function. The returned error is an [*Error].
func ParseFunction(params, body string, kind FunctionKind, opts Options) (*ast.Program, error) {
	prefix := "function"
	switch kind {
	case GeneratorFunction:
		prefix = "function*"
	case AsyncFunction:
		prefix = "async function"
	case AsyncGeneratorFunction:
		prefix = "async function*"
	}
	prefix += " anonymous("
	const mid = "\n) {\n"
	src := prefix + params + mid + body + "\n}"

	if err := parseFunctionPart(src, len(prefix), params, kind, &opts, true); err != nil {
		return nil, err
	}
	if err := parseFunctionPart(src, len(prefix)+len(params)+len(mid), body, kind, &opts, false); err != nil {
		return nil, err
	}
	return parseFunctionSource(src, &opts)
}

// abort is the panic value that unwinds the parser on a syntax error.
type abort struct {
	err *Error
}

func recovered(r any) *Error {
	a, ok := r.(*abort)
	if !ok {
		panic(r)
	}
	return a.err
}

type usage uint8

const (
	useThis usage = 1 << iota
	useArguments
	useSuperProp
	useSuperCall
	useNewTarget
	useDirectEval
)

// fnState is the parser state that belongs to the function being parsed.
type fnState struct {
	strict bool

	// inFunction reports whether a return statement is allowed.
	inFunction bool

	// topLevel reports the top level of a script or a module.
	topLevel bool

	// yield and await report whether the words are operators.
	yield bool
	await bool

	// awaitReserved reports that await is not an identifier even where it is not an operator.
	awaitReserved bool

	// inParams reports that formal parameters are being parsed, where yield and await expressions are errors.
	inParams bool

	superProp   bool
	superCall   bool
	newTarget   bool
	noArguments bool

	// noIn reports that the in operator is not allowed at the current expression level.
	noIn bool

	use usage

	// labelBase is the index in parser.labels of the first label of the function.
	labelBase int

	// assignedBase is the index in parser.assigned of the first name that the own code of the
	// function assigns to.
	assignedBase int

	// funcsBefore is the number of functions that end before the function starts.
	funcsBefore int32

	loopDepth  int
	breakDepth int

	// yieldPos, awaitPos and awaitIdentPos are the offsets plus one of the first yield expression,
	// await expression and identifier named await, since the start of the innermost construct
	// that can turn out to be an arrow function head. Zero means none.
	yieldPos      int32
	awaitPos      int32
	awaitIdentPos int32
}

type label struct {
	name string
	loop bool
}

type parser struct {
	src    string
	pos    int
	t      token
	module bool
	opts   Options

	// offset is added to error offsets. src is then a part of fullSrc.
	offset  int
	fullSrc string

	// prevEnd is the end offset of the token before the current one.
	prevEnd int32

	// tokens counts the tokens toward the next call of the Poll option.
	tokens int

	f     fnState
	scope *scope
	pool  []*scope

	labels []label

	// labelRun is the number of labels that directly precede the statement about to be parsed.
	labelRun int

	depth int
	// links is the number of links of the chains being parsed, which nest the tree
	// without counting toward depth.
	links int

	// arrowStart is the offset of the first token of the innermost assignment expression being parsed.
	// An arrow function can only start there.
	arrowStart int32

	// plainAsync is the most recent identifier node that was the unescaped word async.
	plainAsync *ast.Identifier

	// cover is the offset plus one of an error that applies when coverNode stays an expression
	// instead of becoming a pattern. Zero means none.
	cover     int32
	coverNode ast.Expr

	// patErr is the offset plus one of an error that applies when patNode becomes a pattern.
	patErr  int32
	patNode ast.Expr

	// lastParen is the most recently completed parenthesized expression.
	lastParen ast.Expr

	// arrowBase is the scope that was current at the start of the innermost construct that can turn out
	// to be an arrow function head. pending lists the scopes created directly in such scopes.
	arrowBase *scope
	pending   []*ast.Scope

	class *classState

	exported     map[string]struct{}
	exportLocals []exportLocal

	// assigned holds the names that the own code of the functions being parsed assigns to,
	// innermost function last.
	assigned []string

	// deferred holds the names that assignment expressions assign to while the expressions may
	// still become part of a pattern, which assigns to nothing.
	deferred []string

	// funcs is the number of functions that have ended.
	funcs int32

	// assignedIn becomes [ast.Program.Assigned]. lists is the storage that its lists are taken
	// from; its length is the part in use.
	assignedIn map[string][]int32
	lists      []int32

	nodes nodeSlabs
	stmts listStack[ast.Stmt]
	exprs listStack[ast.Expr]
	props listStack[ast.Property]

	// stringBuf is the temporary storage for decoding escaped string literals.
	stringBuf []byte

	// scratch is the set that the storage of the parse outside the tree came from.
	scratch *scratch
}

type exportLocal struct {
	name string
	pos  int32
}

func (p *parser) fail(pos int, msg string) {
	src := p.src
	if p.fullSrc != "" {
		src = p.fullSrc
	}
	pos += p.offset
	line, col := srcpos.Position(src, pos)
	panic(&abort{
		err: &Error{
			Offset:  pos,
			Line:    line,
			Column:  col,
			Message: msg,
		},
	})
}

func (p *parser) failTok(msg string) {
	p.fail(int(p.t.start), msg)
}

func (p *parser) unexpected() {
	switch k := p.t.kind; {
	case k == tEOF:
		p.failTok("unexpected end of input")
	case k == tEscKeyword:
		p.failTok("keyword must not contain escaped characters")
	case k.isWord():
		p.failTok("unexpected token '" + p.t.str + "'")
	case k == tString:
		p.failTok("unexpected string")
	case k == tNumber || k == tBigInt:
		p.failTok("unexpected number")
	case k == tTemplate:
		p.failTok("unexpected template string")
	}
	p.failTok("unexpected token '" + p.src[p.t.start:p.t.end] + "'")
}

func (p *parser) expect(k tokKind) {
	if p.t.kind != k {
		p.unexpected()
	}
	p.next()
}

func (p *parser) eat(k tokKind) bool {
	if p.t.kind != k {
		return false
	}
	p.next()
	return true
}

// isWord reports whether the current token is the unescaped contextual keyword s.
func (p *parser) isWord(s string) bool {
	return p.t.kind == tIdent && !p.t.esc && p.t.str == s
}

type peeked struct {
	kind tokKind
	nl   bool
	esc  bool
	str  string
}

// peek returns the token after the current one.
func (p *parser) peek() peeked {
	saved := p.t
	pos := p.pos
	prevEnd := p.prevEnd
	p.next()
	r := peeked{
		kind: p.t.kind,
		nl:   p.t.nl,
		esc:  p.t.esc,
		str:  p.t.str,
	}
	p.t = saved
	p.pos = pos
	p.prevEnd = prevEnd
	return r
}

// peek2 returns the two tokens after the current one.
func (p *parser) peek2() (peeked, peeked) {
	saved := p.t
	pos := p.pos
	prevEnd := p.prevEnd
	var r [2]peeked
	for i := range r {
		p.next()
		r[i] = peeked{
			kind: p.t.kind,
			nl:   p.t.nl,
			esc:  p.t.esc,
			str:  p.t.str,
		}
	}
	p.t = saved
	p.pos = pos
	p.prevEnd = prevEnd
	return r[0], r[1]
}

func (p *parser) enter() {
	p.depth++
	if p.depth > maxDepth {
		p.failTok("syntax is nested too deeply")
	}
}

func (p *parser) leave() {
	p.depth--
}

// enterLink counts a link of a chain that is parsed in a loop but nests the tree, as in a+b+c
// and a.b.c. The caller restores p.links when the chain ends.
func (p *parser) enterLink() {
	p.links++
	if p.links > maxLinks {
		p.failTok("expression chain is too long")
	}
}

// consumeSemicolon consumes a semicolon, applying automatic semicolon insertion.
func (p *parser) consumeSemicolon() {
	switch {
	case p.t.kind == tSemi:
		p.next()
	case p.t.kind == tRBrace || p.t.kind == tEOF || p.t.nl:
	default:
		p.unexpected()
	}
}

func (p *parser) init(src string, opts *Options, module bool) {
	p.src = src
	p.opts = *opts
	p.module = module
	if len(src) > math.MaxInt32-1 {
		p.fail(0, "source text is too large")
	}
	if !utf8.ValidString(src) {
		if pos := invalidWTF8(src); pos >= 0 {
			p.fail(pos, "source text is not valid WTF-8")
		}
	}
	p.f = fnState{
		topLevel:      true,
		strict:        opts.Strict || module,
		await:         module,
		awaitReserved: module,
		superProp:     opts.AllowSuperProperty,
		superCall:     opts.AllowSuperCall,
		newTarget:     opts.AllowNewTarget,
		noArguments:   opts.InClassFieldInit,
	}
	p.takeScratch(getScratch())
}

// invalidWTF8 returns the offset of the first byte of src that is not part of a code point
// in canonical WTF-8, or -1 if there is none.
func invalidWTF8(src string) int {
	for pos := 0; pos < len(src); {
		c := src[pos]
		if c < utf8.RuneSelf {
			pos++
			continue
		}
		if c == 0xed && pos+2 < len(src) && src[pos+1] >= 0xa0 && src[pos+1] <= 0xbf && src[pos+2]&0xc0 == 0x80 {
			// A surrogate pair is a single four-byte sequence, never two surrogates.
			if src[pos+1] <= 0xaf && pos+4 < len(src) && src[pos+3] == 0xed && src[pos+4] >= 0xb0 && src[pos+4] <= 0xbf {
				return pos + 3
			}
			pos += 3
			continue
		}
		r, size := utf8.DecodeRuneInString(src[pos:])
		if r == utf8.RuneError && size <= 1 {
			return pos
		}
		pos += size
	}
	return -1
}

func parseProgram(src string, opts *Options, module bool) (prog *ast.Program, err error) {
	defer func() {
		if r := recover(); r != nil {
			prog, err = nil, recovered(r)
		}
	}()

	var p parser
	p.init(src, opts, module)
	defer p.release()
	if len(src) >= 2 && src[0] == '#' && src[1] == '!' {
		p.pos = p.skipLine(2)
	}
	p.scan()

	kind := ast.ScopeScript
	if module {
		kind = ast.ScopeModule
	}
	s := p.pushScope(kind, true)
	var body []ast.Stmt
	if !module {
		body = p.parseDirectives(nil)
	}
	body = p.parseStatementList(body, tEOF)
	if module {
		for _, l := range p.exportLocals {
			if s.lookup(l.name) < 0 {
				p.fail(int(l.pos), "export '"+l.name+"' is not defined in the module")
			}
		}
	}
	prog = &ast.Program{
		StartOff: 0,
		EndOff:   int32(len(src)),
		Source:   src,
		Module:   module,
		Strict:   p.f.strict,
		Body:     body,
		Scope:    s.ast,
		Assigned: p.assignedIn,
	}
	p.popScope()
	return prog, nil
}

// parseFunctionPart checks that part, which is at offset in the function text full,
// is on its own a parameter list or a function body.
func parseFunctionPart(full string, offset int, part string, kind FunctionKind, opts *Options, params bool) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = recovered(r)
		}
	}()

	var p parser
	p.offset = offset
	p.fullSrc = full
	p.init(part, opts, false)
	defer p.release()
	p.scan()

	fn := &ast.Function{
		Generator: kind == GeneratorFunction || kind == AsyncGeneratorFunction,
		Async:     kind == AsyncFunction || kind == AsyncGeneratorFunction,
	}
	p.pushScope(ast.ScopeScript, true)
	p.enterFunction(fn)
	if params {
		p.f.inParams = true
		p.parseFormalParams(fn, tEOF)
	} else {
		fn.SimpleParams = true
		p.parseStatementList(p.parseDirectives(fn), tEOF)
	}
	return nil
}

func parseFunctionSource(src string, opts *Options) (prog *ast.Program, err error) {
	defer func() {
		if r := recover(); r != nil {
			prog, err = nil, recovered(r)
		}
	}()

	var p parser
	p.init(src, opts, false)
	defer p.release()
	p.scan()
	s := p.pushScope(ast.ScopeScript, true)
	start := p.t.start
	p.arrowStart = -1
	fn := p.parsePrimary()
	if p.t.kind != tEOF {
		p.unexpected()
	}
	prog = &ast.Program{
		StartOff: 0,
		EndOff:   int32(len(src)),
		Source:   src,
		Strict:   p.f.strict,
		Body: []ast.Stmt{
			&ast.ExprStmt{
				StartOff: start,
				EndOff:   fn.End(),
				Expr:     fn,
			},
		},
		Scope:    s.ast,
		Assigned: p.assignedIn,
	}
	p.popScope()
	return prog, nil
}

type nameFlags uint8

const (
	// nLexical marks a let, const, class, import, catch parameter or lexical function declaration.
	nLexical nameFlags = 1 << iota
	// nVar marks a name declared with var, or as a var-like function, in this scope or hoisted through it.
	nVar
	// nVarListed marks a name that has a DeclVar entry in the scope record.
	nVarListed
	nParam
	// nSloppyFunc marks a lexical name bound only by plain function declarations in sloppy mode code.
	nSloppyFunc
	// nSimpleCatch marks a catch parameter that is a plain identifier.
	nSimpleCatch
	// nAnnexB marks a name that has a DeclAnnexBVar entry in the scope record.
	nAnnexB
)

type nameEntry struct {
	name  string
	flags nameFlags
}

// hoistCand is a block-level function declaration that is a candidate for Annex B var hoisting.
type hoistCand struct {
	decl *ast.FuncDecl

	// own reports that the declaration is directly in the scope that holds the candidate.
	own bool
}

// scope is the parser's working state for one [ast.Scope].
type scope struct {
	ast    *ast.Scope
	parent *scope

	// varScope reports whether var declarations are hoisted to this scope.
	varScope bool

	// catchBody reports that the scope is the block of a catch clause whose parent holds the parameter.
	catchBody bool

	names []nameEntry
	index map[string]int32
	hoist []hoistCand
}

func (s *scope) lookup(name string) int {
	if s.index != nil {
		if i, ok := s.index[name]; ok {
			return int(i)
		}
		return -1
	}
	for i := range s.names {
		if s.names[i].name == name {
			return i
		}
	}
	return -1
}

func (s *scope) add(name string, flags nameFlags) {
	s.names = append(s.names, nameEntry{
		name:  name,
		flags: flags,
	})
	if s.index != nil {
		s.index[name] = int32(len(s.names) - 1)
		return
	}
	if len(s.names) > 16 {
		s.index = make(map[string]int32, 64)
		for i := range s.names {
			s.index[s.names[i].name] = int32(i)
		}
	}
}

func (p *parser) pushScope(kind ast.ScopeKind, varScope bool) *scope {
	var s *scope
	if n := len(p.pool); n > 0 {
		s = p.pool[n-1]
		p.pool = p.pool[:n-1]
	} else {
		s = &scope{}
	}
	as := alloc(&p.nodes.scope)
	as.Kind = kind
	if p.scope != nil {
		as.Parent = p.scope.ast
		if p.scope == p.arrowBase {
			p.pending = append(p.pending, as)
		}
	}
	s.ast = as
	s.parent = p.scope
	s.varScope = varScope
	p.scope = s
	return s
}

func (p *parser) popScope() {
	s := p.scope
	parent := s.parent
	if parent != nil && s.ast.ContainsDirectEval {
		parent.ast.ContainsDirectEval = true
	}
	for _, c := range s.hoist {
		id := c.decl.Func.Name
		i := s.lookup(id.Name)
		if !c.own && i >= 0 && s.names[i].flags&nLexical != 0 && s.names[i].flags&nSimpleCatch == 0 {
			continue
		}
		if !s.varScope {
			parent.hoist = append(parent.hoist, hoistCand{
				decl: c.decl,
			})
			continue
		}
		if i >= 0 && s.names[i].flags&nParam != 0 {
			continue
		}
		c.decl.AnnexBHoist = true
		if i < 0 {
			s.add(id.Name, nAnnexB)
		} else if s.names[i].flags&nAnnexB != 0 {
			continue
		} else {
			s.names[i].flags |= nAnnexB
		}
		s.ast.Decls = append(s.ast.Decls, ast.Decl{
			Name: id.Name,
			Kind: ast.DeclAnnexBVar,
			Pos:  id.StartOff,
		})
	}

	p.scope = parent
	clear(s.names)
	clear(s.hoist)
	*s = scope{
		names: s.names[:0],
		hoist: s.hoist[:0],
	}
	p.pool = append(p.pool, s)
}

func (p *parser) redeclared(name string, pos int32) {
	p.fail(int(pos), "identifier '"+name+"' has already been declared")
}

// declareVar declares a var, or a var-like function when fn is not nil, hoisting it to the enclosing var scope.
func (p *parser) declareVar(name string, pos int32, fn *ast.Function) {
	for s := p.scope; ; s = s.parent {
		i := s.lookup(name)
		if i < 0 {
			s.add(name, nVar)
			i = len(s.names) - 1
		} else {
			if f := s.names[i].flags; f&nLexical != 0 && f&nSimpleCatch == 0 {
				p.redeclared(name, pos)
			}
			s.names[i].flags |= nVar
		}
		if !s.varScope {
			continue
		}
		if fn != nil {
			s.ast.Decls = append(s.ast.Decls, ast.Decl{
				Name: name,
				Kind: ast.DeclFunction,
				Pos:  pos,
				Func: fn,
			})
		} else if s.names[i].flags&nVarListed == 0 {
			s.names[i].flags |= nVarListed
			s.ast.Decls = append(s.ast.Decls, ast.Decl{
				Name: name,
				Kind: ast.DeclVar,
				Pos:  pos,
			})
		}
		return
	}
}

// declareLexical declares a lexical binding in the current scope.
// sloppyFunc reports a plain function declaration in sloppy mode code, which may be repeated.
func (p *parser) declareLexical(name string, pos int32, kind ast.DeclKind, fn *ast.Function, sloppyFunc bool) {
	s := p.scope
	flags := nLexical
	if sloppyFunc {
		flags |= nSloppyFunc
	}
	if i := s.lookup(name); i >= 0 {
		if !sloppyFunc || s.names[i].flags != nLexical|nSloppyFunc {
			p.redeclared(name, pos)
		}
	} else {
		s.add(name, flags)
	}
	if s.catchBody {
		if i := s.parent.lookup(name); i >= 0 && s.parent.names[i].flags&nLexical != 0 {
			p.redeclared(name, pos)
		}
	}
	s.ast.Decls = append(s.ast.Decls, ast.Decl{
		Name: name,
		Kind: kind,
		Pos:  pos,
		Func: fn,
	})
}

// declareParam declares a parameter in the current scope, which is a function scope.
func (p *parser) declareParam(name string, pos int32, allowDup bool) {
	s := p.scope
	if i := s.lookup(name); i >= 0 {
		if !allowDup {
			p.fail(int(pos), "duplicate parameter name '"+name+"'")
		}
	} else {
		s.add(name, nParam)
	}
	s.ast.Decls = append(s.ast.Decls, ast.Decl{
		Name: name,
		Kind: ast.DeclParam,
		Pos:  pos,
	})
}

// declMode selects how declarePattern declares the names of a pattern.
type declMode uint8

const (
	declVar declMode = iota
	declLet
	declConst
	declUsing
	declAwaitUsing
	declParam
	declParamDup
	declCatch
	declSimpleCatch
)

// declarePattern checks and declares every name bound by pat.
func (p *parser) declarePattern(pat ast.Pattern, mode declMode) {
	switch pat := pat.(type) {
	case *ast.Identifier:
		p.checkIdent(pat.Name, pat.StartOff, true)
		switch mode {
		case declVar:
			p.declareVar(pat.Name, pat.StartOff, nil)
		case declLet, declConst, declUsing, declAwaitUsing:
			if pat.Name == "let" {
				p.fail(int(pat.StartOff), "let is disallowed as a lexically bound name")
			}
			kind := ast.DeclLet
			switch mode {
			case declConst:
				kind = ast.DeclConst
			case declUsing:
				kind = ast.DeclUsing
			case declAwaitUsing:
				kind = ast.DeclAwaitUsing
			}
			p.declareLexical(pat.Name, pat.StartOff, kind, nil, false)
		case declParam, declParamDup:
			p.declareParam(pat.Name, pat.StartOff, mode == declParamDup)
		case declCatch, declSimpleCatch:
			p.declareLexical(pat.Name, pat.StartOff, ast.DeclCatchParam, nil, false)
			if mode == declSimpleCatch {
				p.scope.names[p.scope.lookup(pat.Name)].flags |= nSimpleCatch
			}
		}
	case *ast.ObjectPattern:
		for i := range pat.Properties {
			p.declarePattern(pat.Properties[i].Value, mode)
		}
		if pat.Rest != nil {
			p.declarePattern(pat.Rest, mode)
		}
	case *ast.ArrayPattern:
		for _, e := range pat.Elements {
			if e != nil {
				p.declarePattern(e, mode)
			}
		}
	case *ast.AssignPattern:
		p.declarePattern(pat.Target, mode)
	case *ast.RestElement:
		p.declarePattern(pat.Target, mode)
	default:
		p.fail(int(pat.Start()), "invalid binding target")
	}
}

// noteAwaitIdent records pos as the first use of await as an identifier when name is await.
func (p *parser) noteAwaitIdent(name string, pos int32) {
	if name == "await" && p.f.awaitIdentPos == 0 {
		p.f.awaitIdentPos = pos + 1
	}
}

// checkIdent checks that name may be used as an identifier reference, or as a binding when binding is set.
func (p *parser) checkIdent(name string, pos int32, binding bool) {
	switch name {
	case "yield":
		if p.f.yield || p.f.strict {
			p.fail(int(pos), "yield is not a valid identifier here")
		}
	case "await":
		if p.f.await || p.f.awaitReserved {
			p.fail(int(pos), "await is not a valid identifier here")
		}
		p.noteAwaitIdent(name, pos)
	case "let", "static", "implements", "interface", "package", "private", "protected", "public":
		if p.f.strict {
			p.fail(int(pos), "'"+name+"' is a reserved word in strict mode")
		}
	case "eval", "arguments":
		if binding && p.f.strict {
			p.fail(int(pos), "'"+name+"' cannot be bound or assigned in strict mode")
		}
	}
}

// checkStrictName checks a name that was accepted in sloppy mode against the strict mode rules.
func (p *parser) checkStrictName(id *ast.Identifier) {
	switch id.Name {
	case "yield", "let", "static", "implements", "interface", "package", "private", "protected", "public":
		p.fail(int(id.StartOff), "'"+id.Name+"' is a reserved word in strict mode")
	case "eval", "arguments":
		p.fail(int(id.StartOff), "'"+id.Name+"' cannot be bound or assigned in strict mode")
	}
}

// classState is the private name bookkeeping of a class body.
type classState struct {
	outer *classState
	names []privateEntry
	refs  []privateRef

	// index maps the names in names to their indices once there are many.
	index map[string]int
}

type privateEntry struct {
	name  string
	flags uint8
}

const (
	privGetter = 1 << iota
	privSetter
	privStatic
	privOther
)

type privateRef struct {
	name string
	pos  int32
}

func (c *classState) declared(name string) int {
	if c.index != nil {
		if i, ok := c.index[name]; ok {
			return i
		}
		return -1
	}
	for i := range c.names {
		if c.names[i].name == name {
			return i
		}
	}
	return -1
}

// privateRef records a use of the private name at pos.
func (p *parser) privateRef(name string, pos int32) {
	if p.class == nil {
		if !slices.Contains(p.opts.PrivateNames, name) {
			p.fail(int(pos), "private name #"+name+" is not defined")
		}
		return
	}
	if p.class.declared(name) >= 0 {
		return
	}
	p.class.refs = append(p.class.refs, privateRef{
		name: name,
		pos:  pos,
	})
}

// privateDecl records the declaration of a private class member.
func (p *parser) privateDecl(name string, pos int32, flags uint8) {
	c := p.class
	if name == "constructor" {
		p.fail(int(pos), "classes may not have a private member named #constructor")
	}
	if i := c.declared(name); i >= 0 {
		old := c.names[i].flags
		accessor := uint8(privGetter | privSetter)
		if old&privOther != 0 || flags&privOther != 0 || old&privStatic != flags&privStatic || old&flags&accessor != 0 {
			p.fail(int(pos), "private name #"+name+" has already been declared")
		}
		c.names[i].flags |= flags
		return
	}
	c.names = append(c.names, privateEntry{
		name:  name,
		flags: flags,
	})
	if c.index != nil {
		c.index[name] = len(c.names) - 1
		return
	}
	if len(c.names) > 16 {
		c.index = make(map[string]int, 64)
		for i := range c.names {
			c.index[c.names[i].name] = i
		}
	}
}
