// Package ast declares the types used to represent the syntax tree of Lua
// code parsed by the parser package.
package ast

import "fmt"

// Node is an AST node.
type Node interface {
	// Pos returns the position of the first token of the node.
	Pos() Pos
}

// Expr is an expression node.
type Expr interface {
	Node
	exprNode()
}

// Stmt is a statement node.
type Stmt interface {
	Node
	stmtNode()
}

// Pos is a position in the source: 1-based line and 1-based byte column.
type Pos struct {
	Line int
	Col  int
}

// String returns "line:col".
func (p Pos) String() string {
	return fmt.Sprintf("%d:%d", p.Line, p.Col)
}

// Comment is a source comment. The lexer keeps comments for tools that
// read directives from them; the parser does not use them.
type Comment struct {
	Pos Pos
	// Text is the comment body without "--" and long brackets.
	Text string
	// Long is set for "--[[ ... ]]" comments.
	Long bool
}

// Chunk is a parsed source file.
type Chunk struct {
	Block *Block
	// Comments are all comments of the source in order.
	Comments []Comment
}

// Block is a list of statements.
type Block struct {
	Start Pos
	Stmts []Stmt
}

// Pos implements Node.
func (b *Block) Pos() Pos { return b.Start }

// Local is a local variable. All references to the variable share the
// pointer, so it identifies the variable.
type Local struct {
	Name string
	// Decl is the position of the declaration.
	Decl Pos
}

// Literal expressions.
type (
	// Nil is the nil literal.
	Nil struct{ At Pos }
	// True is the true literal.
	True struct{ At Pos }
	// False is the false literal.
	False struct{ At Pos }
	// Vararg is the "..." expression.
	Vararg struct{ At Pos }
	// Number is a number literal.
	Number struct {
		At Pos
		// Raw is the literal as written.
		Raw string
	}
	// String is a string literal.
	String struct {
		At Pos
		// Value is the decoded value.
		Value string
	}
)

// Name is a variable reference.
type Name struct {
	At   Pos
	Name string
	// Local is the referenced local or upvalue, nil for a global.
	Local *Local
}

// Index is "X.Key" or "X[Key]". For "X.name" Key is a *String.
type Index struct {
	At  Pos
	X   Expr
	Key Expr
}

// Call is a function call "Func(Args)".
type Call struct {
	At   Pos
	Func Expr
	Args []Expr
}

// MethodCall is a method call "Recv:Method(Args)".
type MethodCall struct {
	At     Pos
	Recv   Expr
	Method *String
	Args   []Expr
}

// Function is a function literal or the body of a function statement.
type Function struct {
	At Pos
	// Params are the parameters, including the implicit "self".
	Params []*Local
	Vararg bool
	Body   *Block
}

// Field is a table constructor item. Key is nil for positional items and
// a *String for "name = value" items.
type Field struct {
	Key   Expr
	Value Expr
}

// Table is a table constructor.
type Table struct {
	At     Pos
	Fields []*Field
}

// Binary is a binary operation.
type Binary struct {
	At Pos
	// Op is the operator: "+", "..", "==", "and" and so on.
	Op   string
	L, R Expr
}

// Unary is a unary operation: "-", "not" or "#".
type Unary struct {
	At Pos
	Op string
	X  Expr
}

// Paren is a parenthesized expression. It truncates multiple results and
// is not assignable.
type Paren struct {
	At Pos
	X  Expr
}

// Statements.
type (
	// LocalStmt is "local Names = Values".
	LocalStmt struct {
		At     Pos
		Names  []*Local
		Values []Expr
	}
	// LocalFunction is "local function Name() end".
	LocalFunction struct {
		At   Pos
		Name *Local
		Func *Function
	}
	// Assign is "Targets = Values". Targets are *Name or *Index.
	Assign struct {
		At      Pos
		Targets []Expr
		Values  []Expr
	}
	// CallStmt is a call used as a statement.
	CallStmt struct {
		At   Pos
		Call Expr
	}
	// Do is "do Body end".
	Do struct {
		At   Pos
		Body *Block
	}
	// While is "while Cond do Body end".
	While struct {
		At   Pos
		Cond Expr
		Body *Block
	}
	// Repeat is "repeat Body until Cond". Cond sees the locals of Body.
	Repeat struct {
		At   Pos
		Body *Block
		Cond Expr
	}
	// If is "if Conds[0] then Blocks[0] elseif ... else Else end".
	If struct {
		At     Pos
		Conds  []Expr
		Blocks []*Block
		Else   *Block
	}
	// NumericFor is "for Var = Start, Limit, Step do Body end".
	NumericFor struct {
		At                 Pos
		Var                *Local
		Start, Limit, Step Expr
		Body               *Block
	}
	// GenericFor is "for Vars in Exprs do Body end".
	GenericFor struct {
		At    Pos
		Vars  []*Local
		Exprs []Expr
		Body  *Block
	}
	// FunctionStmt is "function Target() end". Target is a *Name or an
	// *Index chain; for "function a.b:m()" it is a.b.m and Method is set.
	FunctionStmt struct {
		At     Pos
		Target Expr
		Method bool
		Func   *Function
	}
	// Return is "return Values".
	Return struct {
		At     Pos
		Values []Expr
	}
	// Break is "break".
	Break struct{ At Pos }
	// Goto is "goto Label".
	Goto struct {
		At    Pos
		Label string
	}
	// Label is "::Name::".
	Label struct {
		At   Pos
		Name string
	}
)

// Pos implementations.
func (e *Nil) Pos() Pos           { return e.At }
func (e *True) Pos() Pos          { return e.At }
func (e *False) Pos() Pos         { return e.At }
func (e *Vararg) Pos() Pos        { return e.At }
func (e *Number) Pos() Pos        { return e.At }
func (e *String) Pos() Pos        { return e.At }
func (e *Name) Pos() Pos          { return e.At }
func (e *Index) Pos() Pos         { return e.At }
func (e *Call) Pos() Pos          { return e.At }
func (e *MethodCall) Pos() Pos    { return e.At }
func (e *Function) Pos() Pos      { return e.At }
func (e *Table) Pos() Pos         { return e.At }
func (e *Binary) Pos() Pos        { return e.At }
func (e *Unary) Pos() Pos         { return e.At }
func (e *Paren) Pos() Pos         { return e.At }
func (s *LocalStmt) Pos() Pos     { return s.At }
func (s *LocalFunction) Pos() Pos { return s.At }
func (s *Assign) Pos() Pos        { return s.At }
func (s *CallStmt) Pos() Pos      { return s.At }
func (s *Do) Pos() Pos            { return s.At }
func (s *While) Pos() Pos         { return s.At }
func (s *Repeat) Pos() Pos        { return s.At }
func (s *If) Pos() Pos            { return s.At }
func (s *NumericFor) Pos() Pos    { return s.At }
func (s *GenericFor) Pos() Pos    { return s.At }
func (s *FunctionStmt) Pos() Pos  { return s.At }
func (s *Return) Pos() Pos        { return s.At }
func (s *Break) Pos() Pos         { return s.At }
func (s *Goto) Pos() Pos          { return s.At }
func (s *Label) Pos() Pos         { return s.At }

func (*Nil) exprNode()        {}
func (*True) exprNode()       {}
func (*False) exprNode()      {}
func (*Vararg) exprNode()     {}
func (*Number) exprNode()     {}
func (*String) exprNode()     {}
func (*Name) exprNode()       {}
func (*Index) exprNode()      {}
func (*Call) exprNode()       {}
func (*MethodCall) exprNode() {}
func (*Function) exprNode()   {}
func (*Table) exprNode()      {}
func (*Binary) exprNode()     {}
func (*Unary) exprNode()      {}
func (*Paren) exprNode()      {}

func (*LocalStmt) stmtNode()     {}
func (*LocalFunction) stmtNode() {}
func (*Assign) stmtNode()        {}
func (*CallStmt) stmtNode()      {}
func (*Do) stmtNode()            {}
func (*While) stmtNode()         {}
func (*Repeat) stmtNode()        {}
func (*If) stmtNode()            {}
func (*NumericFor) stmtNode()    {}
func (*GenericFor) stmtNode()    {}
func (*FunctionStmt) stmtNode()  {}
func (*Return) stmtNode()        {}
func (*Break) stmtNode()         {}
func (*Goto) stmtNode()          {}
func (*Label) stmtNode()         {}
