package main

import "container/heap"

type dijkstraNode struct {
	Arrival  uint64
	Vertex   uint64
	WalkTime uint32
	Index    int // Index of the node in the heap
}

type priorityQueue []*dijkstraNode

func (pq *priorityQueue) Len() int {
	return len(*pq)
}

func (pq *priorityQueue) Less(i, j int) bool {
	l := *pq
	return l[i].Arrival < l[j].Arrival
}

func (pq *priorityQueue) Swap(i, j int) {
	l := *pq
	l[i], l[j] = l[j], l[i]
	l[i].Index = i
	l[j].Index = j
}

func (pq *priorityQueue) Push(x interface{}) {
	*pq = append(*pq, x.(*dijkstraNode))
}

func (pq *priorityQueue) Pop() interface{} {
	old := *pq
	n := len(old)
	x := old[n-1]
	x.Index = -1 // for safety
	*pq = old[:n-1]
	return x
}

func (pq *priorityQueue) update(node *dijkstraNode, arrival uint64, targetWalkTime uint32) {
	node.Arrival = arrival
	node.WalkTime = targetWalkTime
	heap.Fix(pq, node.Index)
}

func (r *RaptorData) StopTimesForKthTransfer(rounds *Rounds, current int) {
	round := rounds.Rounds[current]
	next := rounds.Rounds[current+1]

	for stop, t := range round {
		next[stop] = StopArrival{
			Arrival:       t.Arrival,
			Trip:          TripIdNoChange,
			ExistsSession: rounds.CurrentSessionId,
		}
	}

	queue := make(priorityQueue, 0)
	heap.Init(&queue)

	// perform dijkstra on street graph
	for stop, marked := range rounds.MarkedStopsForTransfer {
		if !marked {
			continue
		}
		sa, ok := next[stop]

		if !ok {
			continue
		}

		heap.Push(&queue, &dijkstraNode{
			Arrival:  sa.Arrival,
			Vertex:   stop,
			WalkTime: 0,
		})

		delete(rounds.MarkedStopsForTransfer, stop)
	}

	nodeMap := make(map[uint64]*dijkstraNode)

	for queue.Len() > 0 {
		node := heap.Pop(&queue).(*dijkstraNode)
		delete(nodeMap, node.Vertex)

		arcs := r.StreetGraph[node.Vertex]
		for _, arc := range arcs {
			targetWalkTime := node.WalkTime + arc.Distance

			if targetWalkTime > MaxWalkingMs {
				continue
			}

			arrival := node.Arrival + uint64(arc.Distance)

			old, ok := next[arc.Target]

			if ok && arrival >= old.Arrival {
				continue
			}

			next[arc.Target] = StopArrival{
				Arrival:       arrival,
				Trip:          TripIdTransfer,
				EnterKey:      node.Vertex,
				Departure:     node.Arrival,
				ExistsSession: rounds.CurrentSessionId,
			}
			rounds.MarkedStops[arc.Target] = true
			rounds.EarliestArrivals[arc.Target] = arrival

			target, ok := nodeMap[arc.Target]
			if ok {
				queue.update(target, arrival, targetWalkTime)
				continue
			}

			target = &dijkstraNode{
				Arrival:  arrival,
				Vertex:   arc.Target,
				WalkTime: targetWalkTime,
			}

			nodeMap[arc.Target] = target

			heap.Push(&queue, target)
		}
	}
}
