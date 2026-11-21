package commands

import (
	"fmt"
	"strings"
)

// A small line diff for db diff: Myers' shortest edit script, printed as a
// unified diff with three lines of context.

type diffOp struct {
	kind byte // ' ' same, '-' only in a, '+' only in b
	line string
}

// diffLines is the edit script turning a into b.
func diffLines(a, b []string) []diffOp {
	// The common head and tail need no search.
	pre := 0
	for pre < len(a) && pre < len(b) && a[pre] == b[pre] {
		pre++
	}
	suf := 0
	for suf < len(a)-pre && suf < len(b)-pre && a[len(a)-1-suf] == b[len(b)-1-suf] {
		suf++
	}
	var ops []diffOp
	for _, l := range a[:pre] {
		ops = append(ops, diffOp{' ', l})
	}
	ops = append(ops, myers(a[pre:len(a)-suf], b[pre:len(b)-suf])...)
	for _, l := range a[len(a)-suf:] {
		ops = append(ops, diffOp{' ', l})
	}
	return ops
}

func myers(a, b []string) []diffOp {
	n, m := len(a), len(b)
	if n == 0 && m == 0 {
		return nil
	}
	max := n + m
	off := max + 1
	v := make([]int, 2*max+2)
	var trace [][]int
	found := false
	for d := 0; d <= max && !found; d++ {
		trace = append(trace, append([]int(nil), v...))
		for k := -d; k <= d; k += 2 {
			var x int
			if k == -d || (k != d && v[off+k-1] < v[off+k+1]) {
				x = v[off+k+1]
			} else {
				x = v[off+k-1] + 1
			}
			y := x - k
			for x < n && y < m && a[x] == b[y] {
				x++
				y++
			}
			v[off+k] = x
			if x >= n && y >= m {
				found = true
				break
			}
		}
	}
	// Walk back through the saved frontiers.
	var rev []diffOp
	x, y := n, m
	for d := len(trace) - 1; d >= 0; d-- {
		vd := trace[d]
		k := x - y
		var prevK int
		if k == -d || (k != d && vd[off+k-1] < vd[off+k+1]) {
			prevK = k + 1
		} else {
			prevK = k - 1
		}
		prevX := vd[off+prevK]
		prevY := prevX - prevK
		for x > prevX && y > prevY {
			x--
			y--
			rev = append(rev, diffOp{' ', a[x]})
		}
		if d > 0 {
			if x == prevX {
				y--
				rev = append(rev, diffOp{'+', b[y]})
			} else {
				x--
				rev = append(rev, diffOp{'-', a[x]})
			}
		}
	}
	ops := make([]diffOp, len(rev))
	for i := range rev {
		ops[i] = rev[len(rev)-1-i]
	}
	return ops
}

// unifiedDiff prints the difference of a and b with three lines of
// context; "" when they are the same.
func unifiedDiff(nameA, nameB string, a, b []string) string {
	const context = 3
	ops := diffLines(a, b)
	changed := false
	for _, op := range ops {
		if op.kind != ' ' {
			changed = true
			break
		}
	}
	if !changed {
		return ""
	}
	var out strings.Builder
	fmt.Fprintf(&out, "--- %s\n+++ %s\n", nameA, nameB)
	// Line numbers before each op.
	ai, bi := make([]int, len(ops)+1), make([]int, len(ops)+1)
	for i, op := range ops {
		ai[i+1], bi[i+1] = ai[i], bi[i]
		if op.kind != '+' {
			ai[i+1]++
		}
		if op.kind != '-' {
			bi[i+1]++
		}
	}
	for i := 0; i < len(ops); {
		if ops[i].kind == ' ' {
			i++
			continue
		}
		start := i - context
		if start < 0 {
			start = 0
		}
		// The hunk ends where more than 2*context unchanged lines follow.
		end := i
		for end < len(ops) {
			if ops[end].kind != ' ' {
				end++
				continue
			}
			run := end
			for run < len(ops) && ops[run].kind == ' ' {
				run++
			}
			if run == len(ops) || run-end > 2*context {
				end += min(context, run-end)
				break
			}
			end = run
		}
		aLen, bLen := ai[end]-ai[start], bi[end]-bi[start]
		aStart, bStart := ai[start]+1, bi[start]+1
		if aLen == 0 {
			aStart--
		}
		if bLen == 0 {
			bStart--
		}
		fmt.Fprintf(&out, "@@ -%d,%d +%d,%d @@\n", aStart, aLen, bStart, bLen)
		for _, op := range ops[start:end] {
			out.WriteByte(op.kind)
			out.WriteString(op.line)
			out.WriteByte('\n')
		}
		i = end
	}
	return out.String()
}
