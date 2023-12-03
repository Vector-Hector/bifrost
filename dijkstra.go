package main

import "container/heap"

type dijkstraNode struct {
	Arrival  uint64
	Vertex   uint64
	WalkTime uint32
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
}

func (pq *priorityQueue) Push(x interface{}) {
	*pq = append(*pq, x.(*dijkstraNode))
}

func (pq *priorityQueue) Pop() interface{} {
	old := *pq
	n := len(old)
	x := old[n-1]
	*pq = old[:n-1]
	return x
}

func (r *RaptorData) StopTimesForKthTransfer(rounds *Rounds, current int) {
	round := rounds.Rounds[current]
	next := rounds.Rounds[current+1]

	for stop, t := range round {
		if !rounds.Exists(round, stop) {
			continue
		}
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
		if !rounds.Exists(next, stop) {
			continue
		}

		heap.Push(&queue, &dijkstraNode{
			Arrival:  next[stop].Arrival,
			Vertex:   stop,
			WalkTime: 0,
		})

		delete(rounds.MarkedStopsForTransfer, stop)
	}

	for queue.Len() > 0 {
		node := heap.Pop(&queue).(*dijkstraNode)

		arcs := r.StreetGraph[node.Vertex]
		for _, arc := range arcs {
			targetWalkTime := node.WalkTime + arc.Distance

			if targetWalkTime > MaxWalkingMs {
				continue
			}

			arrival := node.Arrival + uint64(arc.Distance)

			old := next[arc.Target]

			if rounds.Exists(next, arc.Target) && arrival >= old.Arrival {
				continue
			}

			// todo handle shortcuts

			next[arc.Target] = StopArrival{
				Arrival:       arrival,
				Trip:          TripIdTransfer,
				EnterKey:      node.Vertex,
				Departure:     node.Arrival,
				ExistsSession: rounds.CurrentSessionId,
			}
			rounds.MarkedStops[arc.Target] = true
			rounds.EarliestArrivals[arc.Target] = arrival
			heap.Push(&queue, &dijkstraNode{
				Arrival:  arrival,
				Vertex:   arc.Target,
				WalkTime: targetWalkTime,
			})
		}
	}
}
