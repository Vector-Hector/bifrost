package bifrost

import (
	"fmt"
	util "github.com/Vector-Hector/goutil"
	"time"
)

type Source struct {
	StopKey   uint64    // stop key in BifrostData
	Departure time.Time // departure time
}

func timeToMs(day time.Time) uint64 {
	return uint64(day.UnixMilli())
}

func (b *Bifrost) Route(rounds *Rounds, origins []Source, destKey uint64, debug bool) {
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

	calcStart := time.Now()

	for _, origin := range origins {
		departure := timeToMs(origin.Departure)

		rounds.Rounds[0][origin.StopKey] = StopArrival{Arrival: departure, Trip: TripIdOrigin}
		rounds.MarkedStops[origin.StopKey] = true
		rounds.EarliestArrivals[origin.StopKey] = departure
	}

	lastRound := 0

	if debug {
		fmt.Println("setting up took", time.Since(t))
		t = time.Now()
	}

	for k := 0; k < b.TransferLimit+1; k++ {
		if debug {
			fmt.Println("------ Round", k, "------")
		}

		ttsKey := k * 2

		t = time.Now()
		b.runRaptorRound(rounds, destKey, ttsKey, debug)

		for _, sa := range rounds.Rounds[ttsKey] {
			if sa.Arrival < uint64(DayInMs*2) {
				panic("arrival too small")
			}
		}

		if debug {
			fmt.Println("Getting trip times took", time.Since(t))
			t = time.Now()

			fmt.Println("Marked", len(rounds.MarkedStops), "stops for trips")
		}

		if k == 0 {
			for _, origin := range origins {
				rounds.MarkedStops[origin.StopKey] = true
			}
		}

		if debug {
			fmt.Println("num reached stops:", len(rounds.Rounds[ttsKey]))
		}

		for stop, marked := range rounds.MarkedStops {
			rounds.MarkedStopsForTransfer[stop] = marked
		}

		b.runTransferRound(rounds, ttsKey+1)

		for _, sa := range rounds.Rounds[ttsKey+1] {
			if sa.Arrival < uint64(DayInMs*2) {
				panic("arrival too small")
			}
		}

		if debug {
			fmt.Println("Getting transfer times took", time.Since(t))
		}

		if debug {
			fmt.Println("Marked", len(rounds.MarkedStops), "stops in total in round", k)
		}

		if debug {
			fmt.Println("num reached stops:", len(rounds.Rounds[ttsKey+1]))
		}

		if len(rounds.MarkedStops) == 0 {
			break
		}

		lastRound = ttsKey + 2
	}

	if debug {
		fmt.Println("Done in", time.Since(calcStart))
	}

	if debug {
		arrival := rounds.EarliestArrivals[destKey]
		if arrival == ArrivalTimeNotReached {
			panic("destination unreachable")
		}

		fmt.Println("max tts size", len(rounds.Rounds[lastRound]))

		journey := b.ReconstructJourney(destKey, lastRound, rounds)

		dep := journey.GetDeparture()
		arr := journey.GetArrival()

		origin := journey.GetOrigin().GetName()
		destination := journey.GetDestination().GetName()

		fmt.Println("Journey from", origin, "to", destination, "took", arr.Sub(dep), ". dep", dep, ", arr", arr)

		fmt.Println("Journey:")
		util.PrintJSON(journey)
	}
}

func (b *Bifrost) runRaptorRound(rounds *Rounds, target uint64, current int, debug bool) {
	t := time.Now()

	round := rounds.Rounds[current]
	next := rounds.Rounds[current+1]

	for stop, stopArr := range round {
		next[stop] = StopArrival{
			Arrival: stopArr.Arrival,
			Trip:    TripIdNoChange,
		}
	}

	if debug {
		fmt.Println("copying tts took", time.Since(t))
		t = time.Now()
	}

	// clear queue
	for k := range rounds.Queue {
		delete(rounds.Queue, k)
	}

	if debug {
		fmt.Println("clearing queue took", time.Since(t))
		t = time.Now()
	}

	// add routes to queue
	for stop := range rounds.MarkedStops {
		for _, pair := range b.Data.StopToRoutes[stop] {
			enter, ok := rounds.Queue[pair.Route]
			if !ok {
				rounds.Queue[pair.Route] = pair.StopKeyInTrip
				continue
			}

			if enter < pair.StopKeyInTrip {
				continue
			}

			rounds.Queue[pair.Route] = pair.StopKeyInTrip
		}

		delete(rounds.MarkedStops, stop)
	}

	if debug {
		fmt.Println("adding trips to queue took", time.Since(t))
		t = time.Now()

		fmt.Println("q size", len(rounds.Queue))
	}

	numVisited := 0

	// scan routes
	for routeKey, enterKey := range rounds.Queue {
		route := b.Data.Routes[routeKey]

		tripKey := uint32(0)
		departureDay := uint32(0)
		var trip *Trip

		for stopSeqKeyShifted, stopKey := range route.Stops[enterKey:] {
			stopSeqKey := enterKey + uint32(stopSeqKeyShifted)
			numVisited++

			if trip != nil {
				arr := trip.StopTimes[stopSeqKey].ArrivalAtDay(uint64(departureDay))
				ea, ok := rounds.EarliestArrivals[stopKey]
				targetEa, targetOk := rounds.EarliestArrivals[target]

				if (!ok || arr < ea) && (!targetOk || arr < targetEa) {
					next[stopKey] = StopArrival{
						Arrival:   arr,
						Trip:      tripKey,
						EnterKey:  uint64(stopSeqKey),
						Departure: uint64(departureDay),
					}
					rounds.MarkedStops[stopKey] = true
					rounds.EarliestArrivals[stopKey] = arr
				}
			}

			sa, ok := round[stopKey]

			if ok && (trip == nil || sa.Arrival <= trip.StopTimes[stopSeqKey].ArrivalAtDay(uint64(departureDay))) {
				et, key, depDay := b.Data.earliestTrip(routeKey, stopSeqKey, sa.Arrival+b.TransferPaddingMs)
				if et != nil {
					trip = et
					tripKey = key
					departureDay = depDay
				}
			}
		}
	}

	if debug {
		fmt.Println("scanning trips took", time.Since(t))

		fmt.Println("visited", numVisited, "stops")
	}
}

func (r *BifrostData) tripRunsOnDay(trip *Trip, day uint32) bool {
	service := r.Services[trip.Service]
	if day < service.StartDay || day > service.EndDay {
		return false
	}

	if daysSliceContains(service.RemovedExceptions, day) {
		return false
	}

	if daysSliceContains(service.AddedExceptions, day) {
		return true
	}

	// day is relative to unix epoch, which was a thursday
	// our weekdays are relative to monday, so we need to add 4 to the day

	weekday := (day + 4) % 7

	if (service.Weekdays & (1 << weekday)) == 0 {
		return false
	}

	return true
}

func daysSliceContains(days []uint32, day uint32) bool {
	if len(days) == 0 {
		return false
	}
	key := binarySearchDays(days, day)
	return days[key] == day
}

func binarySearchDays(days []uint32, day uint32) int {
	left := 0
	right := len(days) - 1

	for left < right {
		mid := (left + right) / 2

		if days[mid] < day {
			left = mid + 1
		} else {
			right = mid
		}
	}

	return left
}

func (r *BifrostData) earliestTrip(routeKey uint32, stopSeqKey uint32, minDeparture uint64) (*Trip, uint32, uint32) {
	day := uint32(minDeparture / uint64(DayInMs))
	minDepartureInDay := uint32(minDeparture % uint64(DayInMs))

	for i := uint32(0); i <= r.MaxTripDayLength; i++ {
		trip, key := r.earliestTripInDay(routeKey, stopSeqKey, minDepartureInDay+i*DayInMs, day)
		if trip != nil {
			return trip, key, day
		}

		day--
	}

	return nil, 0, 0
}

func (r *BifrostData) earliestTripInDay(routeKey uint32, stopSeqKey uint32, minDepartureInDay uint32, day uint32) (*Trip, uint32) {
	route := r.Routes[routeKey]
	routeStopKey := uint64(routeKey)<<32 | uint64(stopSeqKey)

	reorder, ok := r.Reorders[routeStopKey]
	if !ok {
		return r.earliestTripOrdered(route, stopSeqKey, minDepartureInDay, day)
	}

	return r.earliestTripReordered(route, stopSeqKey, minDepartureInDay, day, reorder)
}

func (r *BifrostData) earliestTripOrdered(route *Route, stopSeqKey uint32, minDepartureInDay uint32, day uint32) (*Trip, uint32) {
	if r.Trips[route.Trips[0]].StopTimes[stopSeqKey].Departure >= minDepartureInDay {
		return r.earliestExistentTripOrdered(route, day, 0)
	}

	if r.Trips[route.Trips[len(route.Trips)-1]].StopTimes[stopSeqKey].Departure < minDepartureInDay {
		return nil, 0
	}

	return r.earliestTripBinarySearch(route, stopSeqKey, minDepartureInDay, day, 0, len(route.Trips)-1)
}

func (r *BifrostData) earliestExistentTripOrdered(route *Route, day uint32, indexStart int) (*Trip, uint32) {
	for i := indexStart; i < len(route.Trips); i++ {
		trip := r.Trips[route.Trips[i]]
		if r.tripRunsOnDay(trip, day) {
			return trip, route.Trips[i]
		}
	}
	return nil, 0
}

// binary searches for the earliest trip, starting later than minDepartureInDay at stopSeqKey.
// this assumes that left is below minDeparture and right is above minDeparture
func (r *BifrostData) earliestTripBinarySearch(route *Route, stopSeqKey uint32, minDepartureInDay uint32, day uint32, left int, right int) (*Trip, uint32) {
	mid := (left + right) / 2

	if left == mid {
		return r.earliestExistentTripOrdered(route, day, right)
	}

	trip := r.Trips[route.Trips[mid]]
	dep := trip.StopTimes[stopSeqKey].Departure

	if dep < minDepartureInDay {
		return r.earliestTripBinarySearch(route, stopSeqKey, minDepartureInDay, day, mid, right)
	}

	return r.earliestTripBinarySearch(route, stopSeqKey, minDepartureInDay, day, left, mid)
}

func (r *BifrostData) earliestExistentTripReordered(route *Route, day uint32, indexStart int, reorder []uint32) (*Trip, uint32) {
	for i := indexStart; i < len(route.Trips); i++ {
		trip := r.Trips[route.Trips[reorder[i]]]
		if r.tripRunsOnDay(trip, day) {
			return trip, route.Trips[reorder[i]]
		}
	}
	return nil, 0
}

func (r *BifrostData) earliestTripReordered(route *Route, stopSeqKey uint32, minDepartureInDay uint32, day uint32, reorder []uint32) (*Trip, uint32) {
	if r.Trips[route.Trips[reorder[0]]].StopTimes[stopSeqKey].Departure >= minDepartureInDay {
		return r.earliestExistentTripReordered(route, day, 0, reorder)
	}

	if r.Trips[route.Trips[reorder[len(route.Trips)-1]]].StopTimes[stopSeqKey].Departure < minDepartureInDay {
		return nil, 0
	}

	return r.earliestTripBinarySearchReordered(route, stopSeqKey, minDepartureInDay, day, reorder, 0, len(route.Trips)-1)
}

// binary searches for the earliest trip, starting later than minDeparture at stopSeqKey.
// this assumes that left is below minDeparture and right is above minDeparture
func (r *BifrostData) earliestTripBinarySearchReordered(route *Route, stopSeqKey uint32, minDepartureInDay uint32, day uint32, reorder []uint32, left int, right int) (*Trip, uint32) {
	mid := (left + right) / 2

	if left == mid {
		return r.earliestExistentTripReordered(route, day, right, reorder)
	}

	trip := r.Trips[route.Trips[reorder[mid]]]
	dep := trip.StopTimes[stopSeqKey].Departure

	if dep < minDepartureInDay {
		return r.earliestTripBinarySearchReordered(route, stopSeqKey, minDepartureInDay, day, reorder, mid, right)
	}

	return r.earliestTripBinarySearchReordered(route, stopSeqKey, minDepartureInDay, day, reorder, left, mid)
}
