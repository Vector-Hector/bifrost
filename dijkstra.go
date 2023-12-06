package bifrost

import (
	"container/heap"
	"fmt"
	"github.com/Vector-Hector/fptf"
	util "github.com/Vector-Hector/goutil"
	"time"
)

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

func (b *Bifrost) RouteOnlyWalk(rounds *Rounds, origins []SourceKey, destKey uint64, debug bool) (*fptf.Journey, error) {
	t := time.Now()

	rounds.NewSession()

	if debug {
		fmt.Println("resetting rounds took", time.Since(t))
		t = time.Now()
	}

	if debug {
		fmt.Println("finding routes to", destKey)

		fmt.Println("origins:")
		for _, origin := range origins {
			fmt.Println("stop", origin.StopKey, "at", origin.Departure)
		}

		fmt.Println("Data stats:")
		b.Data.PrintStats()
	}

	for _, origin := range origins {
		departure := timeToMs(origin.Departure)
		rounds.Rounds[0][origin.StopKey] = StopArrival{Arrival: departure, Trip: TripIdOrigin}
		rounds.MarkedStops[origin.StopKey] = true
		rounds.EarliestArrivals[origin.StopKey] = departure
	}

	b.runTransferRound(rounds, 0)

	if debug {
		fmt.Println("Getting transfer times took", time.Since(t))
	}

	_, ok := rounds.EarliestArrivals[destKey]
	if !ok {
		panic("destination unreachable")
	}

	journey := b.ReconstructJourney(destKey, 1, rounds)

	if debug {
		dep := journey.GetDeparture()
		arr := journey.GetArrival()

		origin := journey.GetOrigin().GetName()
		destination := journey.GetDestination().GetName()

		fmt.Println("Journey from", origin, "to", destination, "took", arr.Sub(dep), ". dep", dep, ", arr", arr)

		fmt.Println("Journey:")
		util.PrintJSON(journey)
	}

	return journey, nil
}

func (b *Bifrost) runTransferRound(rounds *Rounds, current int) {
	round := rounds.Rounds[current]
	next := rounds.Rounds[current+1]

	for stop, t := range round {
		next[stop] = StopArrival{
			Arrival: t.Arrival,
			Trip:    TripIdNoChange,
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
			WalkTime: sa.WalkTime,
		})

		delete(rounds.MarkedStopsForTransfer, stop)
	}

	nodeMap := make(map[uint64]*dijkstraNode)

	for queue.Len() > 0 {
		node := heap.Pop(&queue).(*dijkstraNode)
		delete(nodeMap, node.Vertex)

		arcs := b.Data.StreetGraph[node.Vertex]
		for _, arc := range arcs {
			targetWalkTime := node.WalkTime + arc.Distance

			if targetWalkTime > b.MaxWalkingMs {
				continue
			}

			arrival := node.Arrival + uint64(arc.Distance)

			old, ok := next[arc.Target]

			if ok && arrival >= old.Arrival {
				continue
			}

			next[arc.Target] = StopArrival{
				Arrival:   arrival,
				Trip:      TripIdTransfer,
				EnterKey:  node.Vertex,
				Departure: node.Arrival,
				WalkTime:  targetWalkTime,
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
