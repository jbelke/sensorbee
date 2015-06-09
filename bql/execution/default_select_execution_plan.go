package execution

import (
	"fmt"
	"pfi/sensorbee/sensorbee/bql/parser"
	"pfi/sensorbee/sensorbee/bql/udf"
	"pfi/sensorbee/sensorbee/core/tuple"
	"reflect"
	"time"
)

type defaultSelectExecutionPlan struct {
	commonExecutionPlan
	// Window information (extracted from LogicalPlan):
	windowSize int64
	windowType parser.RangeUnit
	emitter    parser.Emitter
	// store name->alias mapping
	relations []parser.AliasRelationAST
	// buffers holds data of a single stream window, keyed by the
	// alias (!) of the respective input stream. It will be
	// updated (appended and possibly truncated) whenever
	// Process() is called with a new tuple.
	buffers map[string][]*tuple.Tuple
	// curResults holds results of a query over the buffer.
	curResults []tuple.Map
	// prevResults holds results of a query over the buffer
	// in the previous execution run.
	prevResults []tuple.Map
}

// CanBuildDefaultSelectExecutionPlan checks whether the given statement
// allows to use an defaultSelectExecutionPlan.
func CanBuildDefaultSelectExecutionPlan(lp *LogicalPlan, reg udf.FunctionRegistry) bool {
	// TODO check that there are no aggregate functions
	return len(lp.Relations) == 1 &&
		len(lp.GroupList) == 0 &&
		lp.Having == nil
}

// defaultSelectExecutionPlan is a very simple plan that follows the
// theoretical processing model.
//
// After each tuple arrives,
// - compute the contents of the current window using the
//   specified window size/type,
// - perform a SELECT query on that data,
// - compute the data that need to be emitted by comparison with
//   the previous run's results.
func NewDefaultSelectExecutionPlan(lp *LogicalPlan, reg udf.FunctionRegistry) (ExecutionPlan, error) {
	// prepare projection components
	projs, err := prepareProjections(lp.Projections, reg)
	if err != nil {
		return nil, err
	}
	// compute evaluator for the filter
	filter, err := prepareFilter(lp.Filter, reg)
	if err != nil {
		return nil, err
	}
	// initialize buffers (one per declared input relation)
	buffers := make(map[string][]*tuple.Tuple, len(lp.Relations))
	for _, rel := range lp.Relations {
		var buffer []*tuple.Tuple
		if lp.Unit == parser.Tuples {
			// we already know the required capacity of this buffer
			// if we work with absolute numbers
			buffer = make([]*tuple.Tuple, 0, lp.Value+1)
		}
		// the alias of the relation is the key of the buffer
		buffers[rel.Alias] = buffer
	}
	return &defaultSelectExecutionPlan{
		commonExecutionPlan: commonExecutionPlan{
			projections: projs,
			filter:      filter,
		},
		windowSize:  lp.Value,
		windowType:  lp.Unit,
		emitter:     lp.EmitterType,
		relations:   lp.Relations,
		buffers:     buffers,
		curResults:  []tuple.Map{},
		prevResults: []tuple.Map{},
	}, nil
}

func (ep *defaultSelectExecutionPlan) Process(input *tuple.Tuple) ([]tuple.Map, error) {
	// stream-to-relation:
	// updates the internal buffer with correct window data
	if err := ep.addTupleToBuffer(input); err != nil {
		return nil, err
	}
	if err := ep.removeOutdatedTuplesFromBuffer(input.Timestamp); err != nil {
		return nil, err
	}

	// relation-to-relation:
	// performs a SELECT query on buffer and writes result
	// to temporary table
	if err := ep.performQueryOnBuffer(); err != nil {
		return nil, err
	}

	// relation-to-stream:
	// compute new/old/all result data and return it
	// TODO use an iterator/generator pattern instead
	return ep.computeResultTuples()
}

// addTupleToBuffer appends the received tuple to all internal buffers that
// are associated to the tuple's input name (more than one on self-join).
// Note that after calling this function, these buffers may hold more
// items than allowed by the window specification, so a call to
// removeOutdatedTuplesFromBuffer is necessary afterwards.
func (ep *defaultSelectExecutionPlan) addTupleToBuffer(t *tuple.Tuple) error {
	// we need to append this tuple to all buffers where the input name
	// matches the relation name, so first we count the those buffers
	// (for `FROM a AS left, a AS right`, this tuple will be
	// appended to the two buffers for `left` and `right`)
	numAppends := 0
	for _, rel := range ep.relations {
		if t.InputName == rel.Name {
			numAppends += 1
		}
	}
	// if the tuple's input name didn't match any known relation,
	// something is wrong in the topology and we should return an error
	if numAppends == 0 {
		knownRelNames := make([]string, 0, len(ep.relations))
		for _, rel := range ep.relations {
			knownRelNames = append(knownRelNames, rel.Name)
		}
		return fmt.Errorf("tuple has input name '%s' set, but we "+
			"can only deal with %v", t.InputName, knownRelNames)
	}
	for _, rel := range ep.relations {
		if t.InputName == rel.Name {
			// if we have numAppends > 1 (meaning: this tuple is used in a
			// self-join) we should work with a copy, otherwise we can use
			// the original item
			editTuple := t
			if numAppends > 1 {
				editTuple = t.Copy()
			}
			// TODO maybe a slice is not the best implementation for a queue?
			ep.buffers[rel.Alias] = append(ep.buffers[rel.Alias], editTuple)
		}
	}

	return nil
}

// removeOutdatedTuplesFromBuffer removes tuples from the buffer that
// lie outside the current window as per the statement's window
// specification.
func (ep *defaultSelectExecutionPlan) removeOutdatedTuplesFromBuffer(curTupTime time.Time) error {
	if ep.windowType == parser.Tuples {
		// loop over all buffers and truncate them to ep.windowSize items
		// (do not change ep.buffers while iterating)
		newBufs := make(map[string][]*tuple.Tuple, len(ep.buffers))
		for inputName, buffer := range ep.buffers {
			curBufSize := int64(len(buffer))
			if curBufSize > ep.windowSize {
				// we just need to take the last `windowSize` items:
				// {a, b, c, d} => {b, c, d}
				newBufs[inputName] = buffer[curBufSize-ep.windowSize : curBufSize]
			} else {
				newBufs[inputName] = buffer
			}
		}
		ep.buffers = newBufs

	} else if ep.windowType == parser.Seconds {
		// we need to remove all items older than `windowSize` seconds,
		// compared to the current tuple
		newBufs := make(map[string][]*tuple.Tuple, len(ep.buffers))
		for inputName, buffer := range ep.buffers {
			curBufSize := int64(len(buffer))
			// copy all "sufficiently new" tuples to new buffer
			newBuf := make([]*tuple.Tuple, 0, curBufSize)
			for _, tup := range buffer {
				dur := curTupTime.Sub(tup.Timestamp)
				if dur.Seconds() <= float64(ep.windowSize) {
					newBuf = append(newBuf, tup)
				}
			}
			newBufs[inputName] = newBuf
		}
		ep.buffers = newBufs
	}

	return nil
}

// performQueryOnBuffer executes a SELECT query on the data of the tuples
// currently stored in the buffer. The query results (which is a set of
// tuple.Value, not tuple.Tuple) is stored in ep.curResults. The data
// that was stored in ep.curResults before this method was called is
// moved to ep.prevResults.
//
// Currently performQueryOnBuffer can only perform SELECT ... WHERE ...
// queries on a single relation without aggregate functions, GROUP BY,
// JOIN etc. clauses.
func (ep *defaultSelectExecutionPlan) performQueryOnBuffer() error {
	if len(ep.buffers) > 1 {
		return fmt.Errorf("JOIN not implemented")
	}
	var buffer []*tuple.Tuple
	for _, val := range ep.buffers {
		buffer = val
	}
	// reuse the allocated memory
	output := ep.prevResults[0:0]
	// remember the previous results
	ep.prevResults = ep.curResults
	for _, t := range buffer {
		// evaluate filter condition and convert to bool
		if ep.filter != nil {
			filterResult, err := ep.filter.Eval(t.Data)
			if err != nil {
				return err
			}
			filterResultBool, err := tuple.ToBool(filterResult)
			if err != nil {
				return err
			}
			// if it evaluated to false, do not further process this tuple
			if !filterResultBool {
				continue
			}
		}
		// otherwise, compute all the expressions
		result := tuple.Map(make(map[string]tuple.Value, len(ep.projections)))
		for _, proj := range ep.projections {
			value, err := proj.evaluator.Eval(t.Data)
			if err != nil {
				return err
			}
			if err := assignOutputValue(result, proj.alias, value); err != nil {
				return err
			}
		}
		output = append(output, result)
	}
	ep.curResults = output
	return nil
}

// computeResultTuples compares the results of this run's query with
// the results of the previous run's query and returns the data to
// be emitted as per the Emitter specification (Rstream = new,
// Istream = new-old, Dstream = old-new).
//
// Currently there is no support for multiplicities, i.e., if an item
// is 3 times in `new` and 1 time in `old` it will *not* be contained
// in the result set.
func (ep *defaultSelectExecutionPlan) computeResultTuples() ([]tuple.Map, error) {
	// TODO turn this into an iterator/generator pattern
	var output []tuple.Map
	if ep.emitter == parser.Rstream {
		// emit all tuples
		for _, res := range ep.curResults {
			output = append(output, res)
		}
	} else if ep.emitter == parser.Istream {
		// emit only new tuples
		for _, res := range ep.curResults {
			// check if this tuple is already present in the previous results
			found := false
			for _, prevRes := range ep.prevResults {
				if reflect.DeepEqual(res, prevRes) {
					// yes, it is, do not emit
					// TODO we may want to delete the found item from prevRes
					//      so that item counts are considered for "new items"
					found = true
					break
				}
			}
			if found {
				continue
			}
			// if we arrive here, `res` is not contained in prevResults
			output = append(output, res)
		}
	} else if ep.emitter == parser.Dstream {
		// emit only old tuples
		for _, prevRes := range ep.prevResults {
			// check if this tuple is present in the current results
			found := false
			for _, res := range ep.curResults {
				if reflect.DeepEqual(res, prevRes) {
					// yes, it is, do not emit
					// TODO we may want to delete the found item from curRes
					//      so that item counts are considered for "new items",
					//      but can we do this safely with regard to the next run?
					found = true
					break
				}
			}
			if found {
				continue
			}
			// if we arrive here, `prevRes` is not contained in curResults
			output = append(output, prevRes)
		}
	}
	return output, nil
}
