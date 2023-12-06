package bifrost

type Rounds struct {
	Rounds                 []map[uint64]StopArrival
	MarkedStops            map[uint64]bool
	MarkedStopsForTransfer map[uint64]bool
	EarliestArrivals       map[uint64]uint64
	Queue                  map[uint32]uint32
}

func (b *Bifrost) NewRounds() *Rounds {
	rounds := make([]map[uint64]StopArrival, (b.TransferLimit+1)*2+1)

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

type StopArrival struct {
	Arrival uint64 // arrival time in unix ms
	Trip    uint32 // trip id, 0xffffffff specifies a transfer, 0xfffffffe specifies no change compared to previous round

	EnterKey  uint64 // stop sequence key in route for trips, vertex key for transfers
	Departure uint64 // departure day for trips, departure time in unix ms for transfers

	WalkTime uint32 // time in ms to walk to this stop
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
}

func (r *Rounds) ResetRounds() {
	done := make(chan bool)

	maxThreads := 12

	free := make(chan bool, maxThreads)

	for i := 0; i < maxThreads; i++ {
		free <- true
	}

	for i := range r.Rounds {
		go func(i int) {
			<-free
			for k := range r.Rounds[i] {
				delete(r.Rounds[i], k)
			}
			done <- true
			free <- true
		}(i)
	}

	for i := 0; i < len(r.Rounds); i++ {
		<-done
	}
}
