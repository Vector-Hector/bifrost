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
		if !rounds.Exists(&t) {
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
		if !rounds.Exists(&next[stop]) {
			continue
		}

		heap.Push(&queue, &dijkstraNode{
			Arrival:  next[stop].Arrival,
			Vertex:   uint64(stop),
			WalkTime: 0,
		})
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

			if rounds.Exists(&old) && arrival >= old.Arrival {
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

/*
func legacyStopTransfers() {
	for stop, marked := range rounds.MarkedStopsForTransfer {
		if !marked {
			continue
		}
		if !round[stop].Exists {
			continue
		}
		arrivalAtStop := round[stop]
		transfers := r.StreetGraph[stop]

		for _, transfer := range transfers {
			arrival := arrivalAtStop.Arrival + transfer.Distance

			old := next[transfer.Target]
			if !old.Exists {
				next[transfer.Target] = StopArrival{
					Arrival:          arrival,
					Trip:             TripIdTransfer,
					EnterKey:         uint64(stop),
					DepartureOrRoute: arrivalAtStop.Arrival,
					Exists:           true,
				}
				rounds.MarkedStops[transfer.Target] = true
				rounds.EarliestArrivals[transfer.Target] = arrival
				continue
			}

			if arrival >= old.Arrival {
				continue
			}

			next[transfer.Target] = StopArrival{
				Arrival:          arrival,
				Trip:             TripIdTransfer,
				EnterKey:         uint32(stop),
				DepartureOrRoute: arrivalAtStop.Arrival,
				Exists:           true,
			}
			rounds.MarkedStops[transfer.Target] = true
			rounds.EarliestArrivals[transfer.Target] = arrival
		}
	}
}*/
