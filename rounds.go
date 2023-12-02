package main

type Rounds struct {
	Rounds                 [][]StopArrival
	MarkedStops            []bool
	MarkedStopsForTransfer []bool
	EarliestArrivals       []uint32
	Queue                  map[uint32]uint32
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
		EarliestArrivals:       make([]uint32, stopCount),
		Queue:                  make(map[uint32]uint32, 10000),
	}
}

func (r *Rounds) Reset() {
	for _, round := range r.Rounds {
		for j := range round {
			round[j] = StopArrival{}
		}
	}

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
}
