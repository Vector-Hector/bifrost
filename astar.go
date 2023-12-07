package bifrost

import (
	"container/heap"
	"fmt"
	"github.com/Vector-Hector/fptf"
	util "github.com/Vector-Hector/goutil"
	"time"
)

type VehicleType uint32

const (
	VehicleTypeCar VehicleType = iota
	VehicleTypeBike
	VehicleTypeFoot
)

type dijkstraNode struct {
	Arrival      uint64
	Vertex       uint64
	TransferTime uint32 // time in ms to walk or cycle to this stop
	Score        uint64
	Index        int // Index of the node in the heap
}

type priorityQueue []*dijkstraNode

func (pq *priorityQueue) Len() int {
	return len(*pq)
}

func (pq *priorityQueue) Less(i, j int) bool {
	l := *pq
	return l[i].Score < l[j].Score
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
	node.TransferTime = targetWalkTime
	heap.Fix(pq, node.Index)
}

func (b *Bifrost) RouteOnlyTimeIndependent(rounds *Rounds, origins []SourceKey, destKey uint64, vehicle VehicleType, debug bool) (*fptf.Journey, error) {
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
		rounds.Rounds[0][origin.StopKey] = StopArrival{Arrival: departure, Trip: TripIdOrigin, Vehicles: 1 << vehicle}
		rounds.MarkedStopsForTransfer[origin.StopKey] = true
		rounds.EarliestArrivals[origin.StopKey] = departure
	}

	b.runTransferRound(rounds, destKey, 0, vehicle, true)

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

func (b *Bifrost) runTransferRound(rounds *Rounds, target uint64, current int, vehicle VehicleType, noTransferCap bool) {
	round := rounds.Rounds[current]
	next := rounds.Rounds[current+1]

	for stop, t := range round {
		next[stop] = StopArrival{
			Arrival:  t.Arrival,
			Trip:     TripIdNoChange,
			Vehicles: t.Vehicles,
		}
	}

	queue := make(priorityQueue, 0)
	heap.Init(&queue)

	targetVertex := &b.Data.Vertices[target]

	// perform dijkstra on street graph
	for stop, marked := range rounds.MarkedStopsForTransfer {
		if !marked {
			continue
		}
		sa, ok := next[stop]

		if !ok {
			continue
		}

		if sa.Vehicles&(1<<vehicle) == 0 && vehicle != VehicleTypeFoot { // foot is always allowed
			continue
		}

		heap.Push(&queue, &dijkstraNode{
			Arrival:      sa.Arrival,
			Vertex:       stop,
			TransferTime: sa.TransferTime,
			Score:        sa.Arrival + b.HeuristicMs(&b.Data.Vertices[stop], targetVertex, vehicle),
		})

		delete(rounds.MarkedStopsForTransfer, stop)
	}

	tripType := TripIdWalk
	if vehicle == VehicleTypeBike {
		tripType = TripIdCycle
	} else if vehicle == VehicleTypeCar {
		tripType = TripIdCar
	}

	nodeMap := make(map[uint64]*dijkstraNode)

	for queue.Len() > 0 {
		node := heap.Pop(&queue).(*dijkstraNode)
		delete(nodeMap, node.Vertex)

		arcs := b.Data.StreetGraph[node.Vertex]
		for _, arc := range arcs {
			dist := arc.WalkDistance
			if vehicle == VehicleTypeBike {
				dist = arc.CycleDistance
			}
			if vehicle == VehicleTypeCar {
				dist = arc.CarDistance
			}

			if dist == 0 {
				continue
			}

			targetTransferTime := node.TransferTime + dist

			if !noTransferCap && vehicle == VehicleTypeFoot && targetTransferTime > b.MaxWalkingMs {
				continue
			}

			if !noTransferCap && vehicle == VehicleTypeBike && targetTransferTime > b.MaxCyclingMs {
				continue
			}

			arrival := node.Arrival + uint64(dist)

			ea, ok := rounds.EarliestArrivals[arc.Target]
			targetEa, targetOk := rounds.EarliestArrivals[target]

			if (ok && ea <= arrival) || (targetOk && targetEa <= arrival) {
				continue
			}

			next[arc.Target] = StopArrival{
				Arrival:      arrival,
				Trip:         tripType,
				EnterKey:     node.Vertex,
				Departure:    node.Arrival,
				TransferTime: targetTransferTime,
				Vehicles:     1 << vehicle,
			}
			rounds.MarkedStops[arc.Target] = true
			rounds.EarliestArrivals[arc.Target] = arrival

			targetNode, ok := nodeMap[arc.Target]
			if ok {
				queue.update(targetNode, arrival, targetTransferTime)
				continue
			}

			targetNode = &dijkstraNode{
				Arrival:      arrival,
				Vertex:       arc.Target,
				TransferTime: targetTransferTime,
				Score:        arrival + b.HeuristicMs(&b.Data.Vertices[arc.Target], targetVertex, vehicle),
			}

			nodeMap[arc.Target] = targetNode

			heap.Push(&queue, targetNode)
		}
	}
}

func (b *Bifrost) HeuristicMs(from, to *Vertex, vehicle VehicleType) uint64 {
	return uint64(b.DistanceMs(from, to, vehicle))
}
