package main

import "math"

type Rounds struct {
	Rounds                 [][]StopArrival
	MarkedStops            []bool
	MarkedStopsForTransfer []bool
	EarliestArrivals       []uint64
	Queue                  map[uint32]uint32
	CurrentSessionId       uint64
}

func NewRounds(stopCount int) *Rounds {
	rounds := make([][]StopArrival, (TransferLimit+1)*2+1)

	for i := range rounds {
		rounds[i] = NewRound(stopCount)
	}

	return &Rounds{
		Rounds:                 rounds,
		MarkedStops:            make([]bool, stopCount),
		MarkedStopsForTransfer: make([]bool, stopCount),
		EarliestArrivals:       make([]uint64, stopCount),
		Queue:                  make(map[uint32]uint32, 10000),
	}
}

func (r *Rounds) Reset() {
	for i := range r.MarkedStops {
		r.MarkedStops[i] = false
	}

	for i := range r.MarkedStopsForTransfer {
		r.MarkedStopsForTransfer[i] = false
	}

	for i := range r.EarliestArrivals {
		r.EarliestArrivals[i] = ArrivalTimeNotReached
	}

	for k := range r.Queue {
		delete(r.Queue, k)
	}

	r.CurrentSessionId++

	if r.CurrentSessionId >= math.MaxUint64 {
		r.CurrentSessionId = 1

		r.ResetRounds()
	}
}

func (r *Rounds) ResetRounds() {
	empty := NewRound(len(r.Rounds[0]))
	done := make(chan bool)

	for i := range r.Rounds {
		go func(i int) {
			copy(r.Rounds[i], empty)
			done <- true
		}(i)
	}

	for i := 0; i < len(r.Rounds); i++ {
		<-done
	}
}

func (r *Rounds) Exists(sa *StopArrival) bool {
	return sa.ExistsSession == r.CurrentSessionId
}
