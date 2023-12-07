package bifrost

import (
	"fmt"
	"github.com/Vector-Hector/fptf"
	util "github.com/Vector-Hector/goutil"
	"time"
)

type SourceKey struct {
	StopKey   uint64    // stop key in RoutingData
	Departure time.Time // departure time
}

type SourceLocation struct {
	Location  *fptf.Location
	Departure time.Time
}

func timeToMs(day time.Time) uint64 {
	return uint64(day.UnixMilli())
}

type RoutingMode string

const (
	ModeCar     RoutingMode = "car"
	ModeBike    RoutingMode = "bike"
	ModeFoot    RoutingMode = "foot"
	ModeTransit RoutingMode = "transit"
)

func (b *Bifrost) Route(rounds *Rounds, origins []SourceLocation, dest *fptf.Location, mode RoutingMode, debug bool) (*fptf.Journey, error) {
	vehicleType := VehicleTypeFoot
	if mode == ModeCar {
		vehicleType = VehicleTypeCar
	} else if mode == ModeBike {
		vehicleType = VehicleTypeBike
	} else if mode != ModeFoot && mode != ModeTransit {
		return nil, fmt.Errorf("unknown mode %v", mode)
	}

	originKeys, err := b.matchSourceLocations(origins, vehicleType)
	if err != nil {
		return nil, err
	}

	destKey, err := b.matchTargetLocation(dest, vehicleType)
	if err != nil {
		return nil, err
	}

	if mode != ModeTransit {
		return b.RouteOnlyTimeIndependent(rounds, originKeys, destKey, vehicleType, debug)
	}

	return b.RouteTransit(rounds, originKeys, destKey, debug)
}

func (b *Bifrost) RouteTransit(rounds *Rounds, origins []SourceKey, destKey uint64, debug bool) (*fptf.Journey, error) {
	// todo add vehicle support (take more of the vehicle bitmask into account, what if bicycle is taken with you on the train?)
	// what if bicycle is taken with you on a car? what if that car is going to a train station and you take the bicycle with you on the train?
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

		rounds.Rounds[0][origin.StopKey] = StopArrival{Arrival: departure, Trip: TripIdOrigin, Vehicles: 1 << VehicleTypeFoot}
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

		b.runTransferRound(rounds, destKey, ttsKey+1, VehicleTypeFoot, false)

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

	_, ok := rounds.EarliestArrivals[destKey]
	if !ok {
		// add an unrestricted transfer round
		// first, mark all vertices that are reachable already
		for vert := range rounds.EarliestArrivals {
			rounds.MarkedStopsForTransfer[vert] = true
		}

		// then, run a transfer round
		b.runTransferRound(rounds, destKey, lastRound, VehicleTypeFoot, true)
		lastRound++
	}

	_, ok = rounds.EarliestArrivals[destKey]
	if !ok {
		return nil, fmt.Errorf("destination unreachable")
	}

	journey := b.ReconstructJourney(destKey, lastRound, rounds)

	if debug {
		fmt.Println("max tts size", len(rounds.Rounds[lastRound]))

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

func (r *RoutingData) tripRunsOnDay(trip *Trip, day uint32) bool {
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

func (r *RoutingData) earliestTrip(routeKey uint32, stopSeqKey uint32, minDeparture uint64) (*Trip, uint32, uint32) {
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

func (r *RoutingData) earliestTripInDay(routeKey uint32, stopSeqKey uint32, minDepartureInDay uint32, day uint32) (*Trip, uint32) {
	route := r.Routes[routeKey]
	routeStopKey := uint64(routeKey)<<32 | uint64(stopSeqKey)

	reorder, ok := r.Reorders[routeStopKey]
	if !ok {
		return r.earliestTripOrdered(route, stopSeqKey, minDepartureInDay, day)
	}

	return r.earliestTripReordered(route, stopSeqKey, minDepartureInDay, day, reorder)
}

func (r *RoutingData) earliestTripOrdered(route *Route, stopSeqKey uint32, minDepartureInDay uint32, day uint32) (*Trip, uint32) {
	if r.Trips[route.Trips[0]].StopTimes[stopSeqKey].Departure >= minDepartureInDay {
		return r.earliestExistentTripOrdered(route, day, 0)
	}

	if r.Trips[route.Trips[len(route.Trips)-1]].StopTimes[stopSeqKey].Departure < minDepartureInDay {
		return nil, 0
	}

	return r.earliestTripBinarySearch(route, stopSeqKey, minDepartureInDay, day, 0, len(route.Trips)-1)
}

func (r *RoutingData) earliestExistentTripOrdered(route *Route, day uint32, indexStart int) (*Trip, uint32) {
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
func (r *RoutingData) earliestTripBinarySearch(route *Route, stopSeqKey uint32, minDepartureInDay uint32, day uint32, left int, right int) (*Trip, uint32) {
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

func (r *RoutingData) earliestExistentTripReordered(route *Route, day uint32, indexStart int, reorder []uint32) (*Trip, uint32) {
	for i := indexStart; i < len(route.Trips); i++ {
		trip := r.Trips[route.Trips[reorder[i]]]
		if r.tripRunsOnDay(trip, day) {
			return trip, route.Trips[reorder[i]]
		}
	}
	return nil, 0
}

func (r *RoutingData) earliestTripReordered(route *Route, stopSeqKey uint32, minDepartureInDay uint32, day uint32, reorder []uint32) (*Trip, uint32) {
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
func (r *RoutingData) earliestTripBinarySearchReordered(route *Route, stopSeqKey uint32, minDepartureInDay uint32, day uint32, reorder []uint32, left int, right int) (*Trip, uint32) {
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

func (b *Bifrost) matchSourceLocations(origins []SourceLocation, vehicleToStart VehicleType) ([]SourceKey, error) {
	originKeys := make([]SourceKey, 0)

	tree := b.Data.WalkableVertexTree
	if vehicleToStart == VehicleTypeBike {
		tree = b.Data.CycleableVertexTree
	} else if vehicleToStart == VehicleTypeCar {
		tree = b.Data.CarableVertexTree
	}

	for _, origin := range origins {
		loc := &GeoPoint{
			Latitude:  origin.Location.Latitude,
			Longitude: origin.Location.Longitude,
		}

		vertices := tree.KNN(loc, 30)

		for _, vertex := range vertices {
			point := vertex.(*GeoPoint)

			originKeys = append(originKeys, SourceKey{
				StopKey:   point.VertKey,
				Departure: origin.Departure,
			})
		}
	}

	if len(originKeys) == 0 {
		return nil, fmt.Errorf("no origin vertex found for any provided location")
	}

	return originKeys, nil
}

func (b *Bifrost) matchTargetLocation(dest *fptf.Location, vehicleToReach VehicleType) (uint64, error) {
	loc := &GeoPoint{
		Latitude:  dest.Latitude,
		Longitude: dest.Longitude,
	}

	tree := b.Data.WalkableVertexTree
	if vehicleToReach == VehicleTypeBike {
		tree = b.Data.CycleableVertexTree
	} else if vehicleToReach == VehicleTypeCar {
		tree = b.Data.CarableVertexTree
	}

	vertices := tree.KNN(loc, 30)

	if len(vertices) == 0 {
		return 0, fmt.Errorf("no stop within tolerance found for location %v", loc)
	}

	for _, vert := range vertices {
		point := vert.(*GeoPoint)

		return point.VertKey, nil
	}

	return 0, fmt.Errorf("no destination vertex found for location %v", loc)
}
