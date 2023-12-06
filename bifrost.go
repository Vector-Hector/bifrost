package bifrost

import (
	"fmt"
	"github.com/kyroy/kdtree"
)

const DayInMs uint32 = 24 * 60 * 60 * 1000

const (
	TripIdTransfer = 0xffffffff
	TripIdNoChange = 0xfffffffe
	TripIdOrigin   = 0xfffffffd

	ArrivalTimeNotReached uint64 = 0xffffffffffffffff
)

type Bifrost struct {
	TransferLimit             int
	TransferPaddingMs         uint64  // only search for trips, padded a bit after transitioning
	WalkingSpeed              float64 // in meters per ms
	MaxWalkingMs              uint32  // duration of walks not allowed to be higher than this when transferring
	MaxStopsConnectionSeconds uint32  // max length of added arcs between stops and street graph in deciseconds

	Data *RoutingData
}

var DefaultBifrost = &Bifrost{
	TransferLimit:             4,
	TransferPaddingMs:         3 * 60 * 1000,
	WalkingSpeed:              0.8 * 0.001,
	MaxWalkingMs:              60 * 1000 * 15,
	MaxStopsConnectionSeconds: 60 * 1000 * 5,
}

type RoutingData struct {
	MaxTripDayLength uint32 `json:"maxTripDayLength"` // number of days to go backwards in time (for trips that end after midnight or multiple days later than the start)

	Services []*Service `json:"services"`

	Routes       []*Route          `json:"routes"`
	StopToRoutes [][]StopRoutePair `json:"stopToRoutes"`
	Trips        []*Trip           `json:"trips"`
	StreetGraph  [][]Arc           `json:"streetGraph"`

	Reorders map[uint64][]uint32 `json:"reorders"`

	// for reconstructing journeys after routing
	Vertices         []Vertex            `json:"vertices"`
	StopsIndex       map[string]uint64   `json:"stopsIndex"`     // gtfs stop id -> vertex index
	NodesIndex       map[int64]uint64    `json:"nodesIndex"`     // csv vertex id -> vertex index
	GtfsRouteIndex   []uint32            `json:"gtfsRouteIndex"` // route index -> gtfs route index
	RouteInformation []*RouteInformation `json:"routeInformation"`
	TripInformation  []*TripInformation  `json:"tripInformation"`
	TripToRoute      []uint32            `json:"tripToRoute"` // trip index -> route index

	// for finding vertices by location. points are GeoPoint
	VertexTree *kdtree.KDTree `json:"-"`
}

func (r *RoutingData) PrintStats() {
	fmt.Println("vertices", len(r.Vertices))
	fmt.Println("routes", len(r.Routes))
	fmt.Println("trips", len(r.Trips))
	fmt.Println("transfer graph", len(r.StreetGraph))
	fmt.Println("stop to routes", len(r.StopToRoutes))
	fmt.Println("reorders", len(r.Reorders))
	fmt.Println("services", len(r.Services))
	fmt.Println("max trip day length", r.MaxTripDayLength)
}

func (r *RoutingData) RebuildVertexTree() {
	verticesAsPoints := make([]kdtree.Point, len(r.Vertices))
	for i, v := range r.Vertices {
		verticesAsPoints[i] = &GeoPoint{
			Latitude:  v.Latitude,
			Longitude: v.Longitude,
			VertKey:   uint64(i),
		}
	}

	r.VertexTree = kdtree.New(verticesAsPoints)
}

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

	AddedExceptions   []uint32 // unix days
	RemovedExceptions []uint32 // unix days
}

type RouteInformation struct {
	ShortName string
	RouteId   string
}

type TripInformation struct {
	Headsign string
	TripId   string
}
