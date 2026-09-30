// Package callgraph holds the call-graph analysis shared by the Go stack
// bound of translated code and the stack-weight pass: which functions lie
// on a cycle of calls.
package callgraph

import "slices"

// Recursive returns the functions in a cycle of the call graph
// calls: every function of a strongly connected component with more than
// one function, and every function that calls itself.
func Recursive(calls [][]int) map[int]bool {
	index := make([]int, len(calls))
	low := make([]int, len(calls))
	for i := range index {
		index[i] = -1
	}
	onStack := make([]bool, len(calls))
	var stack []int
	next := 0
	out := map[int]bool{}
	// Iterative Tarjan: a module's call chains can be deeper than a
	// recursive walk should go.
	type frame struct{ v, edge int }
	for root := range calls {
		if index[root] >= 0 {
			continue
		}
		work := []frame{{root, 0}}
		index[root], low[root] = next, next
		next++
		stack = append(stack, root)
		onStack[root] = true
		for len(work) > 0 {
			f := &work[len(work)-1]
			v := f.v
			if f.edge < len(calls[v]) {
				w := calls[v][f.edge]
				f.edge++
				if index[w] < 0 {
					index[w], low[w] = next, next
					next++
					stack = append(stack, w)
					onStack[w] = true
					work = append(work, frame{w, 0})
				} else if onStack[w] {
					low[v] = min(low[v], index[w])
				}
				continue
			}
			work = work[:len(work)-1]
			if len(work) > 0 {
				u := work[len(work)-1].v
				low[u] = min(low[u], low[v])
			}
			if low[v] == index[v] {
				var scc []int
				for {
					w := stack[len(stack)-1]
					stack = stack[:len(stack)-1]
					onStack[w] = false
					scc = append(scc, w)
					if w == v {
						break
					}
				}
				if len(scc) > 1 || slices.Contains(calls[v], v) {
					for _, w := range scc {
						out[w] = true
					}
				}
			}
		}
	}
	return out
}
