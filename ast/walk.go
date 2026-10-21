package ast

// Inspect traverses the AST in source order. It calls f for a node and,
// if f returns true, for each of its children, like [go/ast.Inspect].
func Inspect(node Node, f func(Node) bool) {
	if node == nil || !f(node) {
		return
	}

	for _, child := range children(node) {
		if child != nil {
			Inspect(child, f)
		}
	}
}

func exprNodes(list []Expr) []Node {
	nodes := make([]Node, 0, len(list))
	for _, e := range list {
		nodes = append(nodes, e)
	}

	return nodes
}

// children returns the child nodes of a node. Nil interface values are
// skipped by Inspect.
func children(node Node) []Node {
	switch n := node.(type) {
	case *Block:
		nodes := make([]Node, 0, len(n.Stmts))
		for _, s := range n.Stmts {
			nodes = append(nodes, s)
		}

		return nodes
	case *Index:
		return []Node{n.X, n.Key}
	case *Call:
		return append([]Node{n.Func}, exprNodes(n.Args)...)
	case *MethodCall:
		return append([]Node{n.Recv, n.Method}, exprNodes(n.Args)...)
	case *Function:
		return []Node{n.Body}
	case *Table:
		return tableNodes(n)
	case *Binary:
		return []Node{n.L, n.R}
	case *Unary:
		return []Node{n.X}
	case *Paren:
		return []Node{n.X}
	default:
		return stmtChildren(node)
	}
}

func tableNodes(t *Table) []Node {
	nodes := make([]Node, 0, 2*len(t.Fields))
	for _, f := range t.Fields {
		if f.Key != nil {
			nodes = append(nodes, f.Key)
		}

		nodes = append(nodes, f.Value)
	}

	return nodes
}

func stmtChildren(node Node) []Node {
	switch n := node.(type) {
	case *LocalStmt:
		return exprNodes(n.Values)
	case *LocalFunction:
		return []Node{n.Func}
	case *Assign:
		return append(exprNodes(n.Targets), exprNodes(n.Values)...)
	case *CallStmt:
		return []Node{n.Call}
	case *Do:
		return []Node{n.Body}
	case *While:
		return []Node{n.Cond, n.Body}
	case *Repeat:
		return []Node{n.Body, n.Cond}
	case *If:
		return ifNodes(n)
	case *NumericFor:
		return forNodes(n)
	case *GenericFor:
		return append(exprNodes(n.Exprs), n.Body)
	case *FunctionStmt:
		return []Node{n.Target, n.Func}
	case *Return:
		return exprNodes(n.Values)
	default:
		return nil
	}
}

func ifNodes(s *If) []Node {
	nodes := make([]Node, 0, 2*len(s.Conds)+1)
	for i, c := range s.Conds {
		nodes = append(nodes, c, s.Blocks[i])
	}

	if s.Else != nil {
		nodes = append(nodes, s.Else)
	}

	return nodes
}

func forNodes(s *NumericFor) []Node {
	nodes := []Node{s.Start, s.Limit}
	if s.Step != nil {
		nodes = append(nodes, s.Step)
	}

	return append(nodes, s.Body)
}
