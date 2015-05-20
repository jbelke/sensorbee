package parser

import (
	"fmt"
)

// ParseStack is a standard stack implementation, but also holds
// methods for transforming the top k elements into a new element.
type ParseStack struct {
	top  *stackElement
	size int
}

// stackElement is a stack-internal data structure that is used
// as a wrapper for the actual data.
type stackElement struct {
	value *ParsedComponent
	next  *stackElement
}

// ParsedComponent is an element of the parse stack that represents
// a section of the input string that was successfully parsed.
type ParsedComponent struct {
	// begin is the index of the first character that belongs to
	// the parsed statement
	begin int
	// end is the index of the last character that belongs to the
	// parsed statement + 1
	end int
	// comp stores the struct that the string was parsed into
	comp interface{}
}

// Len return the stack's size.
func (ps *ParseStack) Len() int {
	return ps.size
}

// Push pushes a new element onto the stack.
func (ps *ParseStack) Push(value *ParsedComponent) {
	ps.top = &stackElement{value, ps.top}
	ps.size++
}

// Pop removes the top element from the stack and returns its value.
// If the stack is empty, returns nil.
func (ps *ParseStack) Pop() (value *ParsedComponent) {
	if ps.size > 0 {
		value, ps.top = ps.top.value, ps.top.next
		ps.size--
		return
	}
	return nil
}

// Peek returns the top element from the stack but doesn't remove it.
// If the stack is empty, returns nil.
func (ps *ParseStack) Peek() (value *ParsedComponent) {
	if ps.size > 0 {
		return ps.top.value
	}
	return nil
}

// AssembleSelect takes the topmost elements from the stack, assuming
// they are components of a SELECT statement, and replaces them by
// a single SelectStmt element.
//
//  Having
//  Grouping
//  Filter
//  From
//  Projections
//   =>
//  SelectStmt{Projections, From, Filter, Grouping, Having}
func (ps *ParseStack) AssembleSelect() {
	// pop the components from the stack in reverse order
	_having, _grouping, _filter, _from, _projections := ps.pop5()

	// extract and convert the contained structure
	// (if this fails, this is a fundamental parser bug => panic ok)
	having := _having.comp.(Having)
	grouping := _grouping.comp.(Grouping)
	filter := _filter.comp.(Filter)
	from := _from.comp.(From)
	projections := _projections.comp.(Projections)

	// assemble the SelectStmt and push it back
	s := SelectStmt{projections, from, filter, grouping, having}
	se := ParsedComponent{_projections.begin, _having.end, s}
	ps.Push(&se)
}

/* Projections/Columns */

// AssembleProjections takes the elements from the stack that
// correspond to the input[begin:end] string and wraps a
// Projections struct around them.
//
//  Any
//  Any
//  Any
//   =>
//  Projections{[Any, Any, Any]}
func (ps *ParseStack) AssembleProjections(begin int, end int) {
	elems := ps.collectElements(begin, end)
	// push the grouped list back
	ps.PushComponent(begin, end, Projections{elems})
}

/* FROM clause */

// AssembleFrom takes the elements from the stack that
// correspond to the input[begin:end] string, makes sure
// they are all Relation elements and wraps a From struct
// around them. If there are no such elements, adds an
// empty From struct to the stack.
//
//  Relation
//  Relation
//  Relation
//   =>
//  From{[Relation, Relation, Relation]}
func (ps *ParseStack) AssembleFrom(begin int, end int) {
	if begin == end {
		// push an empty from clause
		ps.PushComponent(begin, end, From{})
	} else {
		elems := ps.collectElements(begin, end)
		rels := make([]Relation, len(elems), len(elems))
		for i, elem := range elems {
			// (if this conversion fails, this is a fundamental parser bug)
			e := elem.(Relation)
			rels[i] = e
		}
		// push the grouped list back
		ps.PushComponent(begin, end, From{rels})
	}
}

/* WHERE clause */

// AssembleFilter takes the expression on top of the stack
// (if there is a WHERE clause) and wraps a Filter struct
// around it. If there is no WHERE clause, an empty Filter
// struct is used.
//
//  Any
//   =>
//  Filter{Any}
func (ps *ParseStack) AssembleFilter(begin int, end int) {
	if begin == end {
		// push an empty from clause
		ps.PushComponent(begin, end, Filter{})
	} else {
		// if the stack is empty at this point, this is
		// a serious parser bug
		f := ps.Pop()
		if begin > f.begin || end < f.end {
			panic("the item on top of the stack is not within given range")
		}
		ps.PushComponent(begin, end, Filter{f.comp})
	}
}

/* GROUP BY clause */

// AssembleGrouping takes the elements from the stack that
// correspond to the input[begin:end] string and wraps a
// Grouping struct around them. If there are no such elements,
// adds an empty Grouping struct to the stack.
//
//  Any
//  Any
//  Any
//   =>
//  Grouping{[Any, Any, Any]}
func (ps *ParseStack) AssembleGrouping(begin int, end int) {
	elems := ps.collectElements(begin, end)
	// push the grouped list back
	ps.PushComponent(begin, end, Grouping{elems})
}

/* HAVING clause */

// AssembleHaving takes the expression on top of the stack
// (if there is a HAVING clause) and wraps a Having struct
// around it. If there is no HAVING clause, an empty Having
// struct is used.
//
//  Any
//   =>
//  Having{Any}
func (ps *ParseStack) AssembleHaving(begin int, end int) {
	if begin == end {
		// push an empty from clause
		ps.PushComponent(begin, end, Having{})
	} else {
		// if the stack is empty at this point, this is
		// a serious parser bug
		h := ps.Pop()
		if begin > h.begin || end < h.end {
			panic("the item on top of the stack is not within given range")
		}
		ps.PushComponent(begin, end, Having{h.comp})
	}
}

/* Expressions */

// AssembleBinaryOperation takes the two elements from the stack that
// correspond to the input[begin:end] string and adds the given
// binary operator in between. If there is just one element, push
// it back unmodified.
//
//  Any
//  Any
//   =>
//  BinaryOp{op, Any, Any}
func (ps *ParseStack) AssembleBinaryOperation(begin int, end int, op string) {
	elems := ps.collectElements(begin, end)
	if len(elems) == 1 {
		// there is no "binary" operation, push back the single element
		ps.PushComponent(begin, end, elems[0])
	} else if len(elems) == 2 {
		// connect left and right with the given operator
		ps.PushComponent(begin, end, BinaryOp{op, elems[0], elems[1]})
	} else {
		panic(fmt.Sprintf("cannot turn %+v into a binary operation", elems))
	}
}

// PushComponent pushes the given component to the top of the stack
// wrapped in a ParsedComponent struct. It's the caller's responsibility
// to make sure that the parameter is one of the AST classes, or there
// will almost surely be a panic at a later point in the parsing process.
func (ps *ParseStack) PushComponent(begin int, end int, comp interface{}) {
	if begin > end {
		panic("begin must be less or equal to end")
	}
	if top := ps.Peek(); top != nil && top.end > begin {
		panic("begin must be larger or equal to the previous item's end")
	}
	se := ParsedComponent{begin, end, comp}
	ps.Push(&se)
}

/* helper functions to reduce code duplication */

// collectElements pops all elements with begin/end contained in
// the parameter range from the stack, reverses their order and
// returns them.
func (ps *ParseStack) collectElements(begin int, end int) []interface{} {
	elems := []interface{}{}
	// look at elements on the stack as long as there are some and
	// they are contained in our interval
	for ps.Peek() != nil {
		if ps.Peek().end <= begin {
			break
		}
		top := ps.Pop().comp
		elems = append(elems, top)
	}
	// reverse the list to restore original order
	size := len(elems)
	for i := 0; i < size/2; i++ {
		elems[i], elems[size-i-1] = elems[size-i-1], elems[i]
	}
	return elems
}

func (ps *ParseStack) pop2() (*ParsedComponent, *ParsedComponent) {
	if ps.Len() < 2 {
		panic("not enough elements on stack to pop 2 of them")
	}
	return ps.Pop(), ps.Pop()
}

func (ps *ParseStack) pop3() (*ParsedComponent, *ParsedComponent,
	*ParsedComponent) {
	if ps.Len() < 3 {
		panic("not enough elements on stack to pop 3 of them")
	}
	return ps.Pop(), ps.Pop(), ps.Pop()
}

func (ps *ParseStack) pop4() (*ParsedComponent, *ParsedComponent,
	*ParsedComponent, *ParsedComponent) {
	if ps.Len() < 4 {
		panic("not enough elements on stack to pop 4 of them")
	}
	return ps.Pop(), ps.Pop(), ps.Pop(), ps.Pop()
}

func (ps *ParseStack) pop5() (*ParsedComponent, *ParsedComponent,
	*ParsedComponent, *ParsedComponent, *ParsedComponent) {
	if ps.Len() < 5 {
		panic("not enough elements on stack to pop 5 of them")
	}
	return ps.Pop(), ps.Pop(), ps.Pop(), ps.Pop(), ps.Pop()
}

func (ps *ParseStack) pop6() (*ParsedComponent, *ParsedComponent,
	*ParsedComponent, *ParsedComponent, *ParsedComponent, *ParsedComponent) {
	if ps.Len() < 6 {
		panic("not enough elements on stack to pop 6 of them")
	}
	return ps.Pop(), ps.Pop(), ps.Pop(), ps.Pop(), ps.Pop(), ps.Pop()
}

func (ps *ParseStack) pop7() (*ParsedComponent, *ParsedComponent,
	*ParsedComponent, *ParsedComponent, *ParsedComponent, *ParsedComponent,
	*ParsedComponent) {
	if ps.Len() < 7 {
		panic("not enough elements on stack to pop 7 of them")
	}
	return ps.Pop(), ps.Pop(), ps.Pop(), ps.Pop(), ps.Pop(), ps.Pop(), ps.Pop()
}

func (ps *ParseStack) pop8() (*ParsedComponent, *ParsedComponent,
	*ParsedComponent, *ParsedComponent, *ParsedComponent, *ParsedComponent,
	*ParsedComponent, *ParsedComponent) {
	if ps.Len() < 8 {
		panic("not enough elements on stack to pop 8 of them")
	}
	return ps.Pop(), ps.Pop(), ps.Pop(), ps.Pop(), ps.Pop(), ps.Pop(), ps.Pop(), ps.Pop()
}
