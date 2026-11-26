// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package compiler

import (
	"github.com/hajimehoshi/ekuma/internal/ast"
	"github.com/hajimehoshi/ekuma/internal/bytecode"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// closure compiles the function node and emits the creation of its closure into dst.
// name is the value of the name property.
func (f *fn) closure(node *ast.Function, dst int32, name wtf8.String) {
	p := f.c.compileFunction(f, node, name)
	f.f.Protos = append(f.f.Protos, p)
	f.emit(bytecode.OpClosure, dst, int32(len(f.f.Protos)-1), 0)
	f.captureScopes(dst)
}

// forwardsArguments reports whether f can forward its arguments, and read their count and
// elements, from its parameter registers. alias is the name of a variable that a declaration at
// the top level of the body initializes with arguments and that the reads can go through, or "".
func (f *fn) forwardsArguments() (alias string, ok bool) {
	node := f.node
	// The parameter registers hold the arguments only while no parameter code has run.
	if !node.SimpleParams {
		return "", false
	}
	for _, p := range node.Params {
		// A var or function declaration of a parameter's name assigns to the parameter.
		if name := p.(*ast.Identifier).Name; f.assigns(name) || f.redeclares(name) {
			return "", false
		}
	}
	for _, d := range node.Scope.Decls {
		if d.Name == "arguments" {
			return "", false
		}
	}

	aliasVar, aliasInit := argumentsAlias(node)
	// allowed holds the identifiers that can stand for the arguments: references to arguments in
	// a forwarding position, references to arguments or to the alias that are the object of a
	// read of length or of a computed key, and the identifiers of the alias declaration.
	allowed := map[*ast.Identifier]bool{}
	if aliasVar != nil {
		allowed[aliasVar] = true
		allowed[aliasInit] = true
	}
	// notRead holds the member expressions that can be more than a read of their property:
	// assignment targets, operands of updates and delete, and callees, tags and decorators
	// that receive their object as this. Every member expression of a target counts.
	notRead := map[*ast.MemberExpr]bool{}
	markCallee := func(e ast.Expr) {
		if m, _ := callMember(e); m != nil {
			notRead[m] = true
		}
	}
	markNotRead := func(n ast.Node) {
		walk(n, func(n ast.Node) bool {
			if m, isMember := n.(*ast.MemberExpr); isMember {
				notRead[m] = true
			}
			return true
		})
	}
	// The registers of a generator or an async function do not live on the stack, where a read
	// finds the arguments.
	reads := !node.Generator && !node.Async
	ok = true
	depth := 0
	var visit func(ast.Node) bool
	visit = func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.Function:
			if n == node {
				return true
			}
			if !n.Arrow {
				// The function has arguments of its own. An alias is not taken in a function
				// with nested functions, which could capture it or, as declarations of its
				// name, assign to it.
				if aliasVar != nil {
					ok = false
				}
				return false
			}
			// The arguments of an arrow function are those of the enclosing function,
			// but forwarding uses the arguments of the running function.
			depth++
			for _, p := range n.Params {
				walk(p, visit)
			}
			for _, s := range n.Body {
				walk(s, visit)
			}
			walk(n.ExprBody, visit)
			depth--
			return false
		case *ast.WithStmt:
			walk(n.Object, visit)
			// The with object can supply the arguments binding in the body.
			depth++
			walk(n.Body, visit)
			depth--
			return false
		case *ast.CallExpr:
			if depth == 0 {
				if id := forwardedArgument(n); id != nil {
					allowed[id] = true
				}
			}
			markCallee(n.Callee)
		case *ast.TaggedTemplate:
			markCallee(n.Tag)
		case *ast.Class:
			for _, d := range n.Decorators {
				markCallee(d)
			}
			for i := range n.Members {
				for _, d := range n.Members[i].Decorators {
					markCallee(d)
				}
			}
		case *ast.AssignExpr:
			markNotRead(n.Target)
		case *ast.UpdateExpr:
			markNotRead(n.Arg)
		case *ast.UnaryExpr:
			if n.Op == ast.OpDelete {
				markNotRead(n.Arg)
			}
		case *ast.ForInStmt:
			markNotRead(n.Target)
		case *ast.ForOfStmt:
			markNotRead(n.Target)
		case *ast.MemberExpr:
			id, isID := n.Object.(*ast.Identifier)
			if !isID || depth > 0 || !reads || n.Optional || notRead[n] {
				break
			}
			isArgs := id.Name == "arguments"
			// The alias holds the arguments after its initializer, which is in a statement of
			// the top level of the body.
			isAlias := aliasVar != nil && id.Name == aliasVar.Name && id.Start() >= aliasInit.End()
			if !isArgs && !isAlias {
				break
			}
			if !n.Computed {
				if p, named := n.Property.(*ast.Identifier); named && p.Name == "length" {
					allowed[id] = true
				}
				break
			}
			// The alias is not taken for a constant key other than length or an index, which a
			// read of the object finds through an inline cache.
			if s, isString := n.Property.(*ast.StringLit); isAlias && isString && s.Value != "length" {
				if _, isIndex := int32Index(s.Value); !isIndex {
					break
				}
			}
			allowed[id] = true
		case *ast.Identifier:
			if (n.Name == "arguments" || aliasVar != nil && n.Name == aliasVar.Name) && !allowed[n] {
				ok = false
			}
		}
		return ok
	}
	walk(node, visit)
	if !ok {
		return "", false
	}
	if aliasVar != nil {
		alias = aliasVar.Name
	}
	return alias, true
}

// argumentsAlias finds the first declarator of a var, let or const declaration at the top level
// of the body of node that initializes a variable with arguments. It returns the variable and the
// initializer, or nils when there is none.
func argumentsAlias(node *ast.Function) (variable, init *ast.Identifier) {
	for _, s := range node.Body {
		d, ok := s.(*ast.VarDecl)
		if !ok || d.Kind != ast.DeclVar && d.Kind != ast.DeclLet && d.Kind != ast.DeclConst {
			continue
		}
		for i := range d.Decls {
			v, isID := d.Decls[i].Target.(*ast.Identifier)
			if in, ok := d.Decls[i].Init.(*ast.Identifier); isID && ok && in.Name == "arguments" {
				return v, in
			}
		}
	}
	return nil, nil
}

// mappedArguments returns the operand of OpArguments that says whether
// the arguments object of the function node is mapped to its parameters.
func mappedArguments(node *ast.Function) int32 {
	if !node.Strict && node.SimpleParams {
		return 1
	}
	return 0
}

// isSuperCallee reports whether the callee e is super or a property of super.
func isSuperCallee(e ast.Expr) bool {
	if m, ok := e.(*ast.MemberExpr); ok {
		e = m.Object
	}
	_, ok := e.(*ast.SuperExpr)
	return ok
}

func (c *compiler) compileFunction(parent *fn, node *ast.Function, name wtf8.String) *bytecode.Func {
	f := c.newFn(parent, node, name)
	b := f.f
	b.Strict = node.Strict
	b.IsArrow = node.Arrow
	b.IsGenerator = node.Generator
	b.IsAsync = node.Async
	b.IsMethod = node.Method || node.Getter || node.Setter
	b.IsClassConstructor = node.ClassConstructor
	b.IsDerived = node.DerivedConstructor
	b.IsConstructor = node.ClassConstructor ||
		!(node.Arrow || node.Generator || node.Async || node.Method || node.Getter || node.Setter || node.FieldInit || node.StaticBlock)
	b.Length = node.Length
	b.Source = c.prog.Text(node)
	f.setPos(node.Start())

	if node.UsesArguments && mappedArguments(node) != 0 {
		// The arguments object can change the parameters.
		for _, p := range node.Params {
			if id, ok := p.(*ast.Identifier); ok {
				f.mutated[id.Name] = true
			}
		}
	}
	f.anyMutated = node.Scope.ContainsDirectEval

	// A non-strict function that calls eval directly has a dynamic scope for the variables that
	// eval code declares. The calls in a parameter list that is not simple declare them in a
	// scope of their own, outside the parameters.
	sloppyEval := node.HasDirectEval && !node.Strict
	evalInParams := sloppyEval && !node.SimpleParams && hasDirectEval(evalOwn, paramNodes(node.Params)...)
	evalInBody := sloppyEval && hasDirectEval(evalOwn, bodyNodes(node)...)
	if evalInParams || node.SimpleParams && evalInBody {
		f.pushEvalScope()
		f.scope.withs = f.withDepth
	}

	// The parameters are the first registers.
	nparams := len(node.Params)
	var rest *ast.RestElement
	if nparams > 0 {
		if r, ok := node.Params[nparams-1].(*ast.RestElement); ok {
			rest = r
			nparams--
		}
	}
	b.NumParams = nparams
	slots := f.newRegs(nparams)
	if node.SimpleParams {
		for i, p := range node.Params[:nparams] {
			// With duplicate names, the last parameter wins.
			name := p.(*ast.Identifier).Name
			if prev := f.scope.lookup(name); prev != nil && prev.reg >= slots {
				if b.ShadowedParams == nil {
					b.ShadowedParams = make([]bool, nparams)
				}
				b.ShadowedParams[prev.reg-slots] = true
			}
			f.declareAt(name, bindVar, slots+int32(i)).initialized = true
		}
	}
	// The parameters of a list that is not simple are in their temporal dead zone until bound.
	// A binding that a nested function can capture before then holds the TDZ marker. Any other
	// access before then is known at compile time.
	var early map[string]bool
	paramTDZ := func(pb *binding) {
		pb.tdz = true
		if early[pb.name] {
			f.emit(bytecode.OpLoadTDZ, pb.reg, 0, 0)
		} else {
			pb.unset = true
		}
	}
	if !node.SimpleParams {
		early = earlyCaptures(node.Params)
		if node.Scope.ContainsDirectEval && hasDirectEval(evalAll, paramNodes(node.Params)...) {
			// Eval code can see any parameter before it is bound.
			for _, p := range node.Params {
				patternNames(p, func(id *ast.Identifier) {
					early[id.Name] = true
				})
			}
		}
		for i, p := range node.Params[:nparams] {
			id, ok := p.(*ast.Identifier)
			if !ok {
				if a, isAssign := p.(*ast.AssignPattern); isAssign {
					id, ok = a.Target.(*ast.Identifier)
				}
			}
			if !ok {
				continue
			}
			if early[id.Name] {
				paramTDZ(f.declare(id.Name, bindVar))
			} else {
				paramTDZ(f.declareAt(id.Name, bindVar, slots+int32(i)))
			}
		}
	}

	var paramNames map[string]bool
	if !node.SimpleParams {
		paramNames = map[string]bool{}
		for _, p := range node.Params {
			patternNames(p, func(id *ast.Identifier) {
				paramNames[id.Name] = true
			})
		}
	}

	// argsAlias is the name of the variable that reads of the forwarded arguments can go through.
	var argsAlias string

	// The values that the engine provides. An arrow function uses those of the enclosing function.
	if !node.Arrow {
		if node.UsesThis || node.DerivedConstructor || node.ClassConstructor || node.HasDirectEval {
			this := f.declare("this", bindPseudo)
			switch {
			case node.DerivedConstructor:
				this.tdz = true
				this.alwaysCheck = true
				f.emit(bytecode.OpLoadTDZ, this.reg, 0, 0)
			case node.Strict:
				f.emit(bytecode.OpLoadThis, this.reg, 0, 0)
				this.initialized = true
			default:
				f.emit(bytecode.OpLoadThisSloppy, this.reg, 0, 0)
				this.initialized = true
			}
		}
		if node.UsesNewTarget || node.UsesSuperCall || node.DerivedConstructor || node.HasDirectEval {
			nt := f.declare("new.target", bindPseudo)
			nt.initialized = true
			f.emit(bytecode.OpLoadNewTarget, nt.reg, 0, 0)
		}
		if node.UsesSuperProperty || node.HasDirectEval && (node.Method || node.FieldInit || node.StaticBlock) {
			home := f.declare("%home", bindPseudo)
			home.initialized = true
			f.emit(bytecode.OpLoadHome, home.reg, 0, 0)
		}
		if node.UsesSuperCall || node.ClassConstructor {
			callee := f.declare("%callee", bindPseudo)
			callee.initialized = true
			f.emit(bytecode.OpLoadCallee, callee.reg, 0, 0)
			if node.ClassConstructor && !node.DerivedConstructor {
				// The fields of a base class are initialized before the parameters.
				this, _ := f.lookupLocal("this")
				f.emit(bytecode.OpInitFields, callee.reg, this.reg, 0)
			}
		}
		if node.UsesArguments || node.HasDirectEval && !node.FieldInit {
			if f.scope.lookup("arguments") == nil && !paramNames["arguments"] {
				forwards := false
				if !node.Scope.ContainsDirectEval {
					argsAlias, forwards = f.forwardsArguments()
				}
				switch {
				case forwards:
					f.forwardedArgs = f.declare("arguments", bindVar)
				case !node.UsesArguments && mappedArguments(node) != 0 &&
					!arrowsHaveDirectEval(node):
					// Only the direct evals of the function itself can see the arguments object,
					// which the first of them that needs it creates.
					args := f.declare("arguments", bindVar)
					f.emit(bytecode.OpLoadTDZ, args.reg, 0, 0)
					f.lazyArgs = true
				default:
					args := f.declare("arguments", bindVar)
					f.emit(bytecode.OpArguments, args.reg, mappedArguments(node), 0)
				}
				b.NeedsExtra = true
			}
		}
	}

	// A named function expression can refer to itself by its name. With a parameter list that
	// is not simple, the name is visible to the parameter code, and the body's declarations
	// live in a scope of their own that the parameter code cannot see.
	selfName := node.Name != nil && c.isExpression[node]
	paramScope := f.scope
	if !node.SimpleParams {
		if selfName {
			selfName = false
			if n := node.Name.Name; !paramNames[n] && n != "arguments" {
				sc := f.scope
				if evalInParams {
					// The variables that the direct evals of the parameters declare shadow
					// the name, so the name lives in a scope outside that of OpEvalScope.
					sc = &scope{
						fn:       f,
						parent:   f.scope.parent,
						firstReg: f.top,
						withs:    f.scope.withs - 1,
					}
					f.scope.parent = sc
				}
				self := c.newBinding()
				self.name = n
				self.reg = f.newReg()
				self.kind = bindFuncName
				sc.bind(c.symbol(n), self)
				self.initialized = true
				f.emit(bytecode.OpLoadCallee, self.reg, 0, 0)
			}
		}
		// The registers of the body's declarations are reserved before the parameter code
		// so that they start cleared.
		f.varScope = &scope{
			fn:       f,
			parent:   paramScope,
			firstReg: f.top,
			withs:    f.withDepth,
		}
		if evalInBody {
			f.varScope.withs++
		}
		f.scope = f.varScope
	}

	// The variables and functions of the body.
	funcs := map[string]*ast.Function{}
	var funcOrder []string
	var lexicals []*binding
	for _, d := range node.Scope.Decls {
		switch d.Kind {
		case ast.DeclAnnexBVar:
			// A block-level function is not hoisted over a parameter of the same name.
			if paramNames[d.Name] {
				break
			}
			f.declare(d.Name, bindVar)
		case ast.DeclVar:
			f.declare(d.Name, bindVar)
		case ast.DeclFunction:
			if _, ok := funcs[d.Name]; !ok {
				funcOrder = append(funcOrder, d.Name)
			}
			funcs[d.Name] = d.Func
			f.declare(d.Name, bindVar)
		case ast.DeclLet, ast.DeclConst, ast.DeclClass, ast.DeclUsing, ast.DeclAwaitUsing:
			kind := bindLet
			switch d.Kind {
			case ast.DeclConst, ast.DeclUsing, ast.DeclAwaitUsing:
				kind = bindConst
			case ast.DeclClass:
				kind = bindClass
			}
			lb := f.declare(d.Name, kind)
			lb.tdz = true
			lexicals = append(lexicals, lb)
		}
	}
	if argsAlias != "" {
		f.argsAlias, _ = f.lookupLocal(argsAlias)
	}
	// Parameters that are patterns or have defaults bind after the registers above are
	// reserved, because their code can contain closures that capture any of them.
	if !node.SimpleParams {
		// The parameter code cannot see the body's declarations.
		f.varScope.close()
		f.scope = paramScope
		for _, p := range node.Params {
			switch p := p.(type) {
			case *ast.Identifier:
				continue
			case *ast.AssignPattern:
				if _, ok := p.Target.(*ast.Identifier); ok {
					continue
				}
			}
			patternNames(p, func(id *ast.Identifier) {
				paramTDZ(f.declare(id.Name, bindVar))
			})
		}
		for i, p := range node.Params[:nparams] {
			slot := slots + int32(i)
			switch p := p.(type) {
			case *ast.Identifier:
				f.initBinding(p, p.Name, slot)
			case *ast.AssignPattern:
				if id, ok := p.Target.(*ast.Identifier); ok {
					skip := f.emit(bytecode.OpJumpIfNotUndef, 0, slot, 0)
					f.named(p.Default, slot, wtf8.String(id.Name))
					f.patch(skip)
					f.initBinding(id, id.Name, slot)
					break
				}
				f.destructure(p, slot, patInit)
			default:
				f.destructure(p, slot, patInit)
			}
		}
	}
	if rest != nil {
		b.NeedsExtra = true
		t := f.newReg()
		f.emit(bytecode.OpRestArgs, t, int32(nparams), 0)
		f.destructure(rest.Target, t, patInit)
		f.free(t)
	}
	if !node.SimpleParams {
		if evalInBody {
			f.pushEvalScope()
		}
		// A variable of the body starts with the value of the parameter of the same name.
		f.scope = f.varScope
		f.varScope.reopen()
		moved := map[string]bool{}
		for _, d := range node.Scope.Decls {
			if d.Kind != ast.DeclVar && d.Kind != ast.DeclAnnexBVar || funcs[d.Name] != nil || moved[d.Name] {
				continue
			}
			vb := f.varScope.lookup(d.Name)
			if vb == nil {
				continue
			}
			if pb := paramScope.lookup(d.Name); pb != nil && pb.kind == bindVar {
				f.emit(bytecode.OpMove, vb.reg, pb.reg, 0)
				moved[d.Name] = true
			}
		}
	}
	if selfName {
		if f.scope.lookup(node.Name.Name) == nil {
			sc := f.scope
			if evalInBody {
				// The variables that the direct evals declare shadow the name, so the name
				// lives in a scope outside that of OpEvalScope.
				sc = &scope{
					fn:       f,
					parent:   f.scope.parent,
					firstReg: f.top,
					withs:    f.scope.withs - 1,
				}
				f.scope.parent = sc
			}
			self := c.newBinding()
			self.name = node.Name.Name
			self.reg = f.newReg()
			self.kind = bindFuncName
			sc.bind(c.symbol(self.name), self)
			self.initialized = true
			f.emit(bytecode.OpLoadCallee, self.reg, 0, 0)
		}
	}
	b.NumClear = int(f.top)
	for _, lb := range lexicals {
		f.emit(bytecode.OpLoadTDZ, lb.reg, 0, 0)
	}
	for _, fname := range funcOrder {
		fb, _ := f.lookupLocal(fname)
		f.closure(funcs[fname], fb.reg, wtf8.String(fname))
	}
	if node.Generator {
		f.emit(bytecode.OpInitialYield, 0, 0, 0)
	}

	if node.ExprBody != nil {
		t := f.newReg()
		if name, ok := c.fieldNames[node]; ok {
			f.named(node.ExprBody, t, name)
		} else if name, ok := c.fieldKeys[node]; ok {
			k := f.newReg()
			f.loadVar(name, k, false)
			f.namedByKey(node.ExprBody, t, k)
		} else {
			if f.canTailCall() {
				f.markTails(node.ExprBody)
			}
			f.expr(node.ExprBody, t)
			f.tails = f.tails[:0]
		}
		f.emit(bytecode.OpReturn, t, 0, 0)
	} else {
		if using, async := usingKinds(node.Scope); using {
			f.disposing(async, func() {
				f.stmts(node.Body)
			})
		} else {
			f.stmts(node.Body)
		}
		if node.DerivedConstructor {
			t := f.newReg()
			f.emit(bytecode.OpLoadUndef, t, 0, 0)
			this, _ := f.lookupLocal("this")
			f.emit(bytecode.OpReturnDerived, t, this.reg, 0)
		} else {
			f.emit(bytecode.OpReturnUndef, 0, 0, 0)
		}
	}
	return f.finish()
}

// paramNodes returns the parameters params as nodes.
func paramNodes(params []ast.Pattern) []ast.Node {
	nodes := make([]ast.Node, len(params))
	for i, p := range params {
		nodes[i] = p
	}
	return nodes
}

// bodyNodes returns the body of the function node as nodes.
func bodyNodes(node *ast.Function) []ast.Node {
	nodes := make([]ast.Node, 0, len(node.Body)+1)
	for _, s := range node.Body {
		nodes = append(nodes, s)
	}
	if node.ExprBody != nil {
		nodes = append(nodes, node.ExprBody)
	}
	return nodes
}

// earlyCaptures returns the names of the parameters in params that a function nested in
// the parameter list can refer to before the parameter is bound.
func earlyCaptures(params []ast.Pattern) map[string]bool {
	seen := map[string]bool{}
	early := map[string]bool{}
	depth := 0
	var visit func(ast.Node) bool
	visit = func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.Function:
			depth++
			for _, p := range n.Params {
				walk(p, visit)
			}
			for _, s := range n.Body {
				walk(s, visit)
			}
			walk(n.ExprBody, visit)
			depth--
			return false
		case *ast.Identifier:
			if depth > 0 {
				seen[n.Name] = true
			}
		}
		return true
	}
	for _, p := range params {
		walk(p, visit)
		patternNames(p, func(id *ast.Identifier) {
			if seen[id.Name] {
				early[id.Name] = true
			}
		})
	}
	return early
}
