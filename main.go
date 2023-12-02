package main

import (
	"encoding/json"
	"fmt"
	util "github.com/Vector-Hector/goutil"
	"github.com/artonge/go-gtfs"
	"github.com/blevesearch/bleve"
	"github.com/klauspost/compress/zstd"
	"os"
	"strconv"
	"strings"
	"time"
)

const TransferLimit = 4
const TransferPaddingSeconds = 0 // only search for trips, padded a bit after transitioning
const WalkingSpeed = 0.8         // in meters per second
const MaxWalkingSeconds = 60 * 5 // duration of walks not allowed to be higher than this when transferring

const GtfsPath = "data/germany/gtfs"
const RaptorPath = "data/germany/raptor.raptor"
const IndexPath = "data/germany/index.bleve"

const DayInSeconds uint32 = 24 * 60 * 60

var PivotDate, _ = time.Parse(time.RFC3339, "2023-01-02T00:00:00Z") // must be a monday

const (
	TripIdTransfer = 0xffffffff
	TripIdNoChange = 0xfffffffe
	TripIdOrigin   = 0xfffffffd

	ArrivalTimeNotReached = 0xffffffff
)

type StopArrival struct {
	Arrival uint32
	Trip    uint32 // trip id, 0xffffffff specifies a transfer, 0xfffffffe specifies no change compared to previous round

	EnterStopOrKey   uint32 // stop id for transfers, stop sequence key in route for trips
	DepartureOrRoute uint32 // departure time for transfers, route id for trips
	DepartureDay     uint32 // day of trip departure

	Exists bool
}

func timeToSeconds(day time.Time) uint32 {
	return uint32(day.Sub(PivotDate).Seconds())
}

func getTimeInSeconds(timeStr string) uint32 {
	parts := strings.Split(timeStr, ":")
	hours, err := strconv.Atoi(parts[0])
	if err != nil {
		panic(err)
	}

	minutes, err := strconv.Atoi(parts[1])
	if err != nil {
		panic(err)
	}

	seconds, err := strconv.Atoi(parts[2])
	if err != nil {
		panic(err)
	}

	totalSeconds := seconds + minutes*60 + hours*60*60
	return uint32(totalSeconds)
}

func getTimeString(seconds uint32) string {
	seconds = seconds % (24 * 60 * 60) // make sure it's within a day

	hours := seconds / (60 * 60)
	seconds -= hours * 60 * 60
	minutes := seconds / 60
	seconds -= minutes * 60

	return fmt.Sprintf("%02d:%02d:%02d", hours, minutes, seconds)
}

func NewRound(cap int) []StopArrival {
	return make([]StopArrival, cap)
}

func (r *RaptorData) StopTimesForKthTrip(rounds *Rounds, target uint32, current int, debug bool) {
	t := time.Now()

	round := rounds.Rounds[current]
	next := rounds.Rounds[current+1]

	for stop, stopArr := range round {
		if !stopArr.Exists {
			continue
		}
		next[stop] = StopArrival{
			Arrival: stopArr.Arrival,
			Trip:    TripIdNoChange,
			Exists:  true,
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
	for stop, marked := range rounds.MarkedStops {
		if !marked {
			continue
		}

		for _, pair := range r.StopToRoutes[stop] {
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

		rounds.MarkedStops[stop] = false
	}

	if debug {
		fmt.Println("adding trips to queue took", time.Since(t))
		t = time.Now()

		fmt.Println("q size", len(rounds.Queue))
	}

	numVisited := 0

	// scan routes
	for routeKey, enterKey := range rounds.Queue {
		route := r.Routes[routeKey]

		tripKey := uint32(0)
		departureDay := uint32(0)
		var trip *Trip

		for stopSeqKeyShifted, stopKey := range route.Stops[enterKey:] {
			stopSeqKey := enterKey + uint32(stopSeqKeyShifted)
			numVisited++

			if trip != nil {
				arr := trip.StopTimes[stopSeqKey].Arrival + departureDay*DayInSeconds
				if arr < rounds.EarliestArrivals[stopKey] && arr < rounds.EarliestArrivals[target] {
					next[stopKey] = StopArrival{
						Arrival:          arr,
						Trip:             tripKey,
						DepartureOrRoute: routeKey,
						EnterStopOrKey:   stopSeqKey,
						DepartureDay:     departureDay,
						Exists:           true,
					}
					rounds.MarkedStops[stopKey] = true
					rounds.EarliestArrivals[stopKey] = arr
				}
			}

			if round[stopKey].Exists && (trip == nil || round[stopKey].Arrival <= trip.StopTimes[stopSeqKey].Departure+departureDay*DayInSeconds) {
				et, key, depDay := r.EarliestTrip(routeKey, stopSeqKey, round[stopKey].Arrival+TransferPaddingSeconds)
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

func (r *RaptorData) tripRunsOnDay(trip *Trip, day uint32) bool {
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

	weekday := day % 7
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

func (r *RaptorData) EarliestTrip(routeKey uint32, stopSeqKey uint32, minDeparture uint32) (*Trip, uint32, uint32) {
	day := minDeparture / DayInSeconds
	minDepartureInDay := minDeparture % DayInSeconds

	for i := uint32(0); i <= r.MaxTripDayLength; i++ {
		trip, key := r.earliestTripInDay(routeKey, stopSeqKey, minDepartureInDay+i*DayInSeconds, day)
		if trip != nil {
			return trip, key, day
		}

		day--
	}

	return nil, 0, 0
}

func (r *RaptorData) earliestTripInDay(routeKey uint32, stopSeqKey uint32, minDepartureInDay uint32, day uint32) (*Trip, uint32) {
	route := r.Routes[routeKey]
	routeStopKey := uint64(routeKey)<<32 | uint64(stopSeqKey)

	reorder, ok := r.Reorders[routeStopKey]
	if !ok {
		return r.earliestTripOrdered(route, stopSeqKey, minDepartureInDay, day)
	}

	return r.earliestTripReordered(route, stopSeqKey, minDepartureInDay, day, reorder)
}

func (r *RaptorData) earliestTripOrdered(route *Route, stopSeqKey uint32, minDepartureInDay uint32, day uint32) (*Trip, uint32) {
	if r.Trips[route.Trips[0]].StopTimes[stopSeqKey].Departure >= minDepartureInDay {
		return r.earliestExistentTripOrdered(route, day, 0)
	}

	if r.Trips[route.Trips[len(route.Trips)-1]].StopTimes[stopSeqKey].Departure < minDepartureInDay {
		return nil, 0
	}

	return r.earliestTripBinarySearch(route, stopSeqKey, minDepartureInDay, day, 0, len(route.Trips)-1)
}

func (r *RaptorData) earliestExistentTripOrdered(route *Route, day uint32, indexStart int) (*Trip, uint32) {
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
func (r *RaptorData) earliestTripBinarySearch(route *Route, stopSeqKey uint32, minDepartureInDay uint32, day uint32, left int, right int) (*Trip, uint32) {
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

func (r *RaptorData) earliestExistentTripReordered(route *Route, day uint32, indexStart int, reorder []uint32) (*Trip, uint32) {
	for i := indexStart; i < len(route.Trips); i++ {
		trip := r.Trips[route.Trips[reorder[i]]]
		if r.tripRunsOnDay(trip, day) {
			return trip, route.Trips[reorder[i]]
		}
	}
	return nil, 0
}

func (r *RaptorData) earliestTripReordered(route *Route, stopSeqKey uint32, minDepartureInDay uint32, day uint32, reorder []uint32) (*Trip, uint32) {
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
func (r *RaptorData) earliestTripBinarySearchReordered(route *Route, stopSeqKey uint32, minDepartureInDay uint32, day uint32, reorder []uint32, left int, right int) (*Trip, uint32) {
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

func (r *RaptorData) StopTimesForKthTransfer(rounds *Rounds, current int) {
	round := rounds.Rounds[current]
	next := rounds.Rounds[current+1]

	for stop, t := range round {
		if !t.Exists {
			continue
		}
		next[stop] = StopArrival{
			Arrival: t.Arrival,
			Trip:    TripIdNoChange,
			Exists:  true,
		}
	}

	for stop, marked := range rounds.MarkedStopsForTransfer {
		if !marked {
			continue
		}
		if !round[stop].Exists {
			continue
		}
		arrivalAtStop := round[stop]
		transfers := r.TransferGraph[stop]

		for _, transfer := range transfers {
			arrival := arrivalAtStop.Arrival + transfer.Distance

			old := next[transfer.Target]
			if !old.Exists {
				next[transfer.Target] = StopArrival{
					Arrival:          arrival,
					Trip:             TripIdTransfer,
					EnterStopOrKey:   uint32(stop),
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
				EnterStopOrKey:   uint32(stop),
				DepartureOrRoute: arrivalAtStop.Arrival,
				Exists:           true,
			}
			rounds.MarkedStops[transfer.Target] = true
			rounds.EarliestArrivals[transfer.Target] = arrival
		}
	}
}

func ReadRaptorFile(fileName string) *RaptorData {
	f, err := os.Open(fileName)
	if err != nil {
		panic(err)
	}
	defer f.Close()

	read, err := zstd.NewReader(f)
	if err != nil {
		panic(err)
	}
	defer read.Close()

	r := &RaptorData{}

	decoder := json.NewDecoder(read)
	err = decoder.Decode(r)
	if err != nil {
		panic(err)
	}

	return r
}

func WriteRaptorFile(fileName string, r *RaptorData) {
	f, err := os.Create(fileName)
	if err != nil {
		panic(err)
	}
	defer f.Close()

	write, err := zstd.NewWriter(f)
	if err != nil {
		panic(err)
	}
	defer write.Flush()
	defer write.Close()

	encoder := json.NewEncoder(write)
	err = encoder.Encode(r)
	if err != nil {
		panic(err)
	}

}

func GetIndex(fileName string, feed *gtfs.GTFS) bleve.Index {
	_, err := os.Stat(fileName)
	if os.IsNotExist(err) {
		return CreateIndex(fileName, feed)
	}
	if err != nil {
		panic(err)
	}

	index, err := bleve.Open(fileName)
	if err != nil {
		panic(err)
	}

	return index
}

func LoadRaptorDataset() *RaptorData {
	cacheExists := true

	_, err := os.Stat(RaptorPath)
	if os.IsNotExist(err) {
		cacheExists = false
	}

	if cacheExists {
		r := ReadRaptorFile(RaptorPath)
		return r
	}

	fmt.Println("reading gtfs data")
	t := time.Now()

	r, err := ReadGtfsData(GtfsPath)
	if err != nil {
		panic(err)
	}

	//feed, err := gtfs.Load(GtfsPath, nil)
	//if err != nil {
	//	panic(err)
	//}
	//
	//fmt.Println("reading gtfs took", time.Since(t))
	//t = time.Now()
	//
	//r := GtfsToRaptorData(feed)

	fmt.Println("converting gtfs to raptor data took", time.Since(t))
	fmt.Println("writing raptor data")
	t = time.Now()

	WriteRaptorFile(RaptorPath, r)

	fmt.Println("writing raptor data took", time.Since(t))

	return r
}

func CountMarkedStops(marked []bool) int {
	count := 0
	for _, isMarked := range marked {
		if isMarked {
			count++
		}
	}
	return count
}

func runRaptor(r *RaptorData, rounds *Rounds, originKey uint32, destKey uint32, debug bool) {
	t := time.Now()

	rounds.Reset()

	if debug {
		fmt.Println("resetting rounds took", time.Since(t))
		t = time.Now()
	}

	departureTime, err := time.Parse(time.RFC3339, "2023-11-12T08:30:00Z")
	if err != nil {
		panic(err)
	}
	departure := timeToSeconds(departureTime) // depart at 8:30

	if debug {
		r.PrintStats()

		fmt.Println("finding routes from", originKey, "to", destKey)
	}

	calcStart := time.Now()

	rounds.Rounds[0][originKey] = StopArrival{Arrival: uint32(departure), Trip: TripIdOrigin, Exists: true}

	rounds.MarkedStops[originKey] = true

	rounds.EarliestArrivals[originKey] = uint32(departure)

	lastRound := 0

	if debug {
		fmt.Println("setting up took", time.Since(t))
		t = time.Now()
	}

	for k := 0; k < TransferLimit+1; k++ {
		if debug {
			fmt.Println("------ Round", k, "------")
		}

		ttsKey := k * 2

		t = time.Now()
		r.StopTimesForKthTrip(rounds, destKey, ttsKey, debug)

		if debug {
			fmt.Println("Getting trip times took", time.Since(t))
			t = time.Now()

			fmt.Println("Marked", CountMarkedStops(rounds.MarkedStops), "stops for trips")
		}

		if k == 0 {
			rounds.MarkedStops[originKey] = true
		}

		copy(rounds.MarkedStopsForTransfer, rounds.MarkedStops)

		r.StopTimesForKthTransfer(rounds, ttsKey+1)

		if debug {
			fmt.Println("Getting transfer times took", time.Since(t))
		}

		if debug {
			fmt.Println("Marked", CountMarkedStops(rounds.MarkedStops), "stops in total in round", k)
		}

		if CountMarkedStops(rounds.MarkedStops) == 0 {
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

		fmt.Println("Destination reached after", (float64(arrival)-float64(departure))/60, "minutes. dep", getTimeString(departure), "/", departure, ", arr", getTimeString(arrival), "/", arrival)

		journey := r.ReconstructJourney(destKey, lastRound, rounds.Rounds)

		fmt.Println("Journey:")
		util.PrintJSON(journey)
	}
}

func main() {
	r := LoadRaptorDataset()

	originID := "476628" // münchen hbf
	destID := "170058"   // marienplatz
	//destID := "193261" // berlin hbf

	rounds := NewRounds(len(r.StopsIndex))

	runRaptor(r, rounds, r.StopsIndex[originID], r.StopsIndex[destID], true)
}
