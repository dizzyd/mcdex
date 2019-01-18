package algo

import "fmt"

type Value interface{}

type Graph map[Value]*Node

type Node struct {
	Value Value
	graph Graph
	Dependents,
	Dependencies,
	Optionals map[*Node]struct{}
}


func (n *Node) String() string {
	return fmt.Sprintf("%+v", n.Value)
}

func (n *Node) AddDependencies(keys ...Value) {
	for _, key := range keys {
		dep := n.graph.AddNode(key)
		n.Dependencies[dep] = struct{}{}
		dep.Dependents[n] = struct{}{}
	}
}

func (n *Node) AddOptionals(keys ...Value) {
	for _, key := range keys {
		dep := n.graph.AddNode(key)
		n.Optionals[dep] = struct{}{}
	}
}

func (n *Node) IsRoot() bool {
	return len(n.Dependents) == 0
}

func (n *Node) IsLeaf() bool {
	return len(n.Dependencies) == 0
}


func MakeGraph() Graph {
	return make(Graph)
}

func (g Graph) AddNode(key Value) *Node {
	if g[key] == nil {
		g[key] = &Node{
			Value:        key,
			graph:        g,
			Dependents:   make(map[*Node]struct{}),
			Dependencies: make(map[*Node]struct{}),
			Optionals: make(map[*Node]struct{}),
		}
	}
	return g[key]
}

func (g Graph) RemoveNode(key Value) {
	if g[key] == nil {
		return
	}
	n := g[key]
	for _, d := range g {
		delete(d.Dependencies, n)
		delete(d.Dependents, n)
		delete(d.Optionals, n)
	}
	delete(g, key)
}

func (g Graph) Sorted() []*Node {
	sorted := make([]*Node, 0, len(g))
	degree := make(map[*Node]int)

	var next []*Node
	for _, n := range g {
		if n.IsRoot() {
			next = append(next, n)
		} else {
			degree[n] = len(n.Dependents)
		}
	}

	for len(next) > 0 {
		n := next[0]
		next = next[1:]

		sorted = append(sorted, n)

		for d := range n.Dependencies {
			degree[d]--
			if degree[d] == 0 {
				next = append(next, d)
			}
		}
	}

	return sorted
}
