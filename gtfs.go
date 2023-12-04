package main

import (
	"fmt"
	"math"
	"time"
)

type StopContext struct {
	Id   string
	Name string
}

type Vertex struct {
	Longitude float64
	Latitude  float64
	Stop      *StopContext
}

func (v Vertex) Dimensions() int {
	return 2
}

func (v Vertex) Dimension(i int) float64 {
	switch i {
	case 0:
		return v.Latitude
	case 1:
		return v.Longitude
	default:
		panic("invalid dimension")
	}
}

type GeoPoint struct {
	Latitude  float64
	Longitude float64
	VertKey   uint64
}

func (s *GeoPoint) Dimensions() int {
	return 2
}

func (s *GeoPoint) Dimension(i int) float64 {
	switch i {
	case 0:
		return s.Latitude
	case 1:
		return s.Longitude
	default:
		panic("invalid dimension")
	}
}

type Stopover struct {
	Arrival   uint32 // ms time since start of day
	Departure uint32 // ms time since start of day
}

func (s Stopover) ArrivalAtDay(day uint64) uint64 {
	return uint64(s.Arrival) + day*uint64(DayInMs)
}

func (s Stopover) DepartureAtDay(day uint64) uint64 {
	return uint64(s.Departure) + day*uint64(DayInMs)
}

type Route struct {
	Stops []uint64
	Trips []uint32
}

type StopRoutePair struct {
	Route         uint32
	StopKeyInTrip uint32
}

type Trip struct {
	Service   uint32
	StopTimes []Stopover
}

type Arc struct {
	Target   uint64
	Distance uint32 // in ms
}

type Service struct {
	Weekdays uint8  // bitfield, 1 << 0 = monday, 1 << 6 = sunday
	StartDay uint32 // day relative to PivotDate
	EndDay   uint32 // day relative to PivotDate

	AddedExceptions   []uint32 // days relative to PivotDate
	RemovedExceptions []uint32 // days relative to PivotDate
}

type RouteInformation struct {
	ShortName string
}

type TripInformation struct {
	Headsign string
}

type Shortcut struct {
	Target uint64
	Via    uint64
}

type RaptorData struct {
	MaxTripDayLength uint32 // number of days to go backwards in time (for trips that end after midnight or multiple days later than the start)

	Services []*Service

	Routes       []*Route
	StopToRoutes [][]StopRoutePair
	Trips        []*Trip
	StreetGraph  [][]Arc

	Reorders map[uint64][]uint32

	// for reconstructing journeys after routing
	Vertices           []Vertex
	StopsIndex         map[string]uint64 // gtfs stop id -> vertex index
	NodesIndex         map[int64]uint64  // csv vertex id -> vertex index
	RaptorToGtfsRoutes []uint32
	RouteInformation   []*RouteInformation
	TripInformation    []*TripInformation
	TripToRoute        []uint32
}

func (r *RaptorData) PrintStats() {
	fmt.Println("stops", len(r.Vertices))
	fmt.Println("routes", len(r.Routes))
	fmt.Println("trips", len(r.Trips))
	fmt.Println("transfer graph", len(r.StreetGraph))
	fmt.Println("stop to routes", len(r.StopToRoutes))
	fmt.Println("reorders", len(r.Reorders))
	fmt.Println("services", len(r.Services))
	fmt.Println("max trip day length", r.MaxTripDayLength)
}

func getUnixDay(date string) uint32 {
	t, err := time.Parse("20060102", date)
	if err != nil {
		panic(err)
	}

	return uint32(uint64(t.UnixMilli()) / uint64(DayInMs))
}

func DistanceMs(from *Vertex, to *Vertex) uint32 {
	distInKm := Distance(from.Latitude, from.Longitude, to.Latitude, to.Longitude, "K")
	distInMs := (distInKm * 1000) / WalkingSpeed
	res := uint32(math.Ceil(distInMs))
	if res == 0 {
		return 1
	}
	return res
}

// https://www.geodatasource.com/developers/go
func Distance(lat1 float64, lng1 float64, lat2 float64, lng2 float64, unit ...string) float64 {
	const PI float64 = 3.141592653589793

	radlat1 := PI * lat1 / 180
	radlat2 := PI * lat2 / 180

	theta := lng1 - lng2
	radtheta := PI * theta / 180

	dist := math.Sin(radlat1)*math.Sin(radlat2) + math.Cos(radlat1)*math.Cos(radlat2)*math.Cos(radtheta)

	if dist > 1 {
		dist = 1
	}

	dist = math.Acos(dist)
	dist = dist * 180 / PI
	dist = dist * 60 * 1.1515

	if len(unit) > 0 {
		if unit[0] == "K" {
			dist = dist * 1.609344
		} else if unit[0] == "N" {
			dist = dist * 0.8684
		}
	}

	return dist
}
