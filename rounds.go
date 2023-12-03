package main

import (
	"math"
)

type Rounds struct {
	Rounds                 []map[uint64]StopArrival
	MarkedStops            map[uint64]bool
	MarkedStopsForTransfer map[uint64]bool
	EarliestArrivals       map[uint64]uint64
	Queue                  map[uint32]uint32
	CurrentSessionId       uint64
}

func NewRounds(stopCount int) *Rounds {
	rounds := make([]map[uint64]StopArrival, (TransferLimit+1)*2+1)

	for i := range rounds {
		rounds[i] = make(map[uint64]StopArrival)
	}

	return &Rounds{
		Rounds:                 rounds,
		MarkedStops:            make(map[uint64]bool),
		MarkedStopsForTransfer: make(map[uint64]bool),
		EarliestArrivals:       make(map[uint64]uint64),
		Queue:                  make(map[uint32]uint32, 10000),
	}
}

func (r *Rounds) NewSession() {
	r.ResetRounds()

	for i := range r.MarkedStops {
		//r.MarkedStops[i] = false
		delete(r.MarkedStops, i)
	}

	for i := range r.MarkedStopsForTransfer {
		//r.MarkedStopsForTransfer[i] = false
		delete(r.MarkedStopsForTransfer, i)
	}

	for i := range r.EarliestArrivals {
		//r.EarliestArrivals[i] = ArrivalTimeNotReached
		delete(r.EarliestArrivals, i)
	}

	for k := range r.Queue {
		delete(r.Queue, k)
	}

	r.CurrentSessionId++

	if r.CurrentSessionId >= math.MaxUint64 {
		r.CurrentSessionId = 1
	}
}

func (r *Rounds) ResetRounds() {
	done := make(chan bool)

	for i := range r.Rounds {
		go func(i int) {
			for k := range r.Rounds[i] {
				delete(r.Rounds[i], k)
			}
			done <- true
		}(i)
	}

	for i := 0; i < len(r.Rounds); i++ {
		<-done
	}
}

func (r *Rounds) Exists(round map[uint64]StopArrival, stop uint64) bool {
	sa, ok := round[stop]
	if !ok {
		return false
	}
	return sa.ExistsSession == r.CurrentSessionId
}
