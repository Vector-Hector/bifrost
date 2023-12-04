package main

import (
	"encoding/json"
	"fmt"
	util "github.com/Vector-Hector/goutil"
	"github.com/gin-gonic/gin"
	"github.com/klauspost/compress/zstd"
	"os"
	"strconv"
	"strings"
	"time"
)

const TransferLimit = 4
const TransferPaddingSeconds = 0                       // only search for trips, padded a bit after transitioning
const WalkingSpeed = 0.8 * 0.001                       // in meters per ms
const MaxWalkingMs uint32 = 60 * 1000 * 15             // duration of walks not allowed to be higher than this when transferring
const MaxStopsConnectionSeconds uint32 = 60 * 1000 * 5 // max length of added arcs between stops and street graph in deciseconds

const GtfsPath = "data/mvv/gtfs"
const StreetPath = "data/mvv/oberbayern"
const RaptorPath = "data/mvv/raptor.raptor"

const DayInMs uint32 = 24 * 60 * 60 * 1000

const (
	TripIdTransfer = 0xffffffff
	TripIdNoChange = 0xfffffffe
	TripIdOrigin   = 0xfffffffd

	ArrivalTimeNotReached uint64 = 0xffffffffffffffff
)

type StopArrival struct {
	Arrival uint64 // arrival time in unix ms
	Trip    uint32 // trip id, 0xffffffff specifies a transfer, 0xfffffffe specifies no change compared to previous round

	EnterKey  uint64 // stop sequence key in route for trips, vertex key for transfers
	Departure uint64 // departure day for trips, departure time in unix ms for transfers

	ExistsSession uint64 // session id of the last session this stop arrival existed in
}

func timeToSeconds(day time.Time) uint64 {
	return uint64(day.UnixMilli())
}

func getTimeInMs(timeStr string) uint32 {
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
	return uint32(totalSeconds) * 1000
}

func getTimeString(ms uint64) string {
	ms = ms % (24 * 60 * 60 * 1000) // make sure it's within a day

	hours := ms / (60 * 60 * 1000)
	ms -= hours * 60 * 60 * 1000
	minutes := ms / (60 * 1000)
	ms -= minutes * 60 * 1000
	seconds := ms / 1000

	return fmt.Sprintf("%02d:%02d:%02d", hours, minutes, seconds)
}

func (r *RaptorData) StopTimesForKthTrip(rounds *Rounds, target uint64, current int, debug bool) {
	t := time.Now()

	round := rounds.Rounds[current]
	next := rounds.Rounds[current+1]

	for stop, stopArr := range round {
		next[stop] = StopArrival{
			Arrival:       stopArr.Arrival,
			Trip:          TripIdNoChange,
			ExistsSession: rounds.CurrentSessionId,
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
		route := r.Routes[routeKey]

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
						Arrival:       arr,
						Trip:          tripKey,
						EnterKey:      uint64(stopSeqKey),
						Departure:     uint64(departureDay),
						ExistsSession: rounds.CurrentSessionId,
					}
					rounds.MarkedStops[stopKey] = true
					rounds.EarliestArrivals[stopKey] = arr
				}
			}

			sa, ok := round[stopKey]

			if ok && (trip == nil || sa.Arrival <= trip.StopTimes[stopSeqKey].ArrivalAtDay(uint64(departureDay))) {
				et, key, depDay := r.EarliestTrip(routeKey, stopSeqKey, sa.Arrival+TransferPaddingSeconds)
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

func (r *RaptorData) EarliestTrip(routeKey uint32, stopSeqKey uint32, minDeparture uint64) (*Trip, uint32, uint32) {
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

	err = ReadStreetData(r, StreetPath)
	if err != nil {
		panic(err)
	}

	fmt.Println("converting gtfs to raptor data took", time.Since(t))
	fmt.Println("writing raptor data")
	t = time.Now()

	WriteRaptorFile(RaptorPath, r)

	fmt.Println("writing raptor data took", time.Since(t))

	return r
}

func CountMarkedStops(marked map[uint64]bool) int {
	count := 0
	for _, isMarked := range marked {
		if isMarked {
			count++
		}
	}
	return count
}

func runRaptor(r *RaptorData, rounds *Rounds, originKey uint64, destKey uint64, debug bool) {
	t := time.Now()

	rounds.NewSession()

	if debug {
		fmt.Println("resetting rounds took", time.Since(t))
		t = time.Now()
	}

	departureTime, err := time.Parse(time.RFC3339, "2023-12-12T08:30:00Z")
	if err != nil {
		panic(err)
	}
	departure := timeToSeconds(departureTime) // depart at 8:30

	if debug {
		r.PrintStats()

		fmt.Println("finding routes from", originKey, "to", destKey)
	}

	calcStart := time.Now()

	rounds.Rounds[0][originKey] = StopArrival{Arrival: departure, Trip: TripIdOrigin, ExistsSession: rounds.CurrentSessionId}

	rounds.MarkedStops[originKey] = true

	rounds.EarliestArrivals[originKey] = departure

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

		for _, sa := range rounds.Rounds[ttsKey] {
			if sa.Arrival < uint64(DayInMs*2) {
				panic("arrival too small")
			}
		}

		if debug {
			fmt.Println("Getting trip times took", time.Since(t))
			t = time.Now()

			fmt.Println("Marked", CountMarkedStops(rounds.MarkedStops), "stops for trips")
		}

		if k == 0 {
			rounds.MarkedStops[originKey] = true
		}

		if debug {
			debugExistentStops(rounds.Rounds[ttsKey], rounds.CurrentSessionId)
		}

		//copy(rounds.MarkedStopsForTransfer, rounds.MarkedStops)
		for stop, marked := range rounds.MarkedStops {
			rounds.MarkedStopsForTransfer[stop] = marked
		}

		r.StopTimesForKthTransfer(rounds, ttsKey+1)

		for _, sa := range rounds.Rounds[ttsKey+1] {
			if sa.Arrival < uint64(DayInMs*2) {
				panic("arrival too small")
			}
		}

		if debug {
			fmt.Println("Getting transfer times took", time.Since(t))
		}

		if debug {
			fmt.Println("Marked", CountMarkedStops(rounds.MarkedStops), "stops in total in round", k)
		}

		if debug {
			debugExistentStops(rounds.Rounds[ttsKey+1], rounds.CurrentSessionId)
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

		fmt.Println("Destination reached after", time.UnixMilli(int64(arrival)).Sub(time.UnixMilli(int64(departure))), ". dep", getTimeString(departure), "/", departure, ", arr", getTimeString(arrival), "/", arrival)

		journey := r.ReconstructJourney(destKey, lastRound, rounds)

		fmt.Println("Journey:")
		util.PrintJSON(journey)
	}
}

func debugExistentStops(round map[uint64]StopArrival, sessionId uint64) {
	numExisting := 0
	for _, stop := range round {
		if stop.ExistsSession == sessionId {
			numExisting++
		}
	}
	fmt.Println("num existing stops", numExisting)
	fmt.Println("num stops", len(round))
	fmt.Println("ratio", float64(numExisting)/float64(len(round)))
}

func main() {
	fmt.Println("Loading raptor data")
	r := LoadRaptorDataset()

	fmt.Println("Raptor data loaded")

	numHandlerThreads := 12

	roundChan := make(chan *Rounds, numHandlerThreads)

	for i := 0; i < numHandlerThreads; i++ {
		roundChan <- NewRounds()
	}

	engine := gin.Default()

	engine.GET("/", func(c *gin.Context) {
		rounds := <-roundChan

		originID := "de:09162:6"
		destID := "de:09162:2"

		originKey := r.StopsIndex[originID]
		destKey := r.StopsIndex[destID]

		t := time.Now()
		runRaptor(r, rounds, originKey, destKey, true)
		fmt.Println("Routing took", time.Since(t))

		roundChan <- rounds

		c.String(200, "Hello world")
	})

	err := engine.Run(":8090")
	if err != nil {
		panic(err)
	}

}
