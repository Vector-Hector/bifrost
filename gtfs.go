package main

import (
	"fmt"
	"github.com/artonge/go-gtfs"
	"math"
	"sort"
	"strings"
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
	Shortcuts          [][]Shortcut
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

func GtfsToRaptorData(feed *gtfs.GTFS) *RaptorData {
	fmt.Println("converting gtfs to raptor data")

	fmt.Println("converting stops")

	stops := make([]Vertex, len(feed.Stops))
	stopToRoutes := make([][]StopRoutePair, len(stops))
	stopsIndex := make(map[string]uint64, len(feed.Stops))

	for i, stop := range feed.Stops {
		stops[i] = Vertex{
			Longitude: stop.Longitude,
			Latitude:  stop.Latitude,
			Stop: &StopContext{
				Id:   stop.ID,
				Name: stop.Name,
			},
		}

		stopsIndex[stop.ID] = uint64(i)

		stopToRoutes[i] = make([]StopRoutePair, 0)
	}

	fmt.Println("stops", len(stops))
	fmt.Println("converting services")

	services := make([]*Service, len(feed.Calendars))
	servicesIndex := make(map[string]uint32, len(feed.Calendars))
	for i, calendar := range feed.Calendars {
		services[i] = &Service{
			Weekdays: uint8(calendar.Monday) | uint8(calendar.Tuesday)<<1 | uint8(calendar.Wednesday)<<2 | uint8(calendar.Thursday)<<3 | uint8(calendar.Friday)<<4 | uint8(calendar.Saturday)<<5 | uint8(calendar.Sunday)<<6,
			StartDay: getUnixDay(calendar.Start),
			EndDay:   getUnixDay(calendar.End),
		}

		servicesIndex[calendar.ServiceID] = uint32(i)
	}

	for _, calendarDate := range feed.CalendarDates {
		service := services[servicesIndex[calendarDate.ServiceID]]

		switch calendarDate.ExceptionType {
		case 1:
			service.AddedExceptions = append(service.AddedExceptions, getUnixDay(calendarDate.Date))
		case 2:
			service.RemovedExceptions = append(service.RemovedExceptions, getUnixDay(calendarDate.Date))
		}
	}

	// sort service exceptions
	for _, service := range services {
		sort.Slice(service.AddedExceptions, func(i, j int) bool {
			return service.AddedExceptions[i] < service.AddedExceptions[j]
		})
		sort.Slice(service.RemovedExceptions, func(i, j int) bool {
			return service.RemovedExceptions[i] < service.RemovedExceptions[j]
		})
	}

	fmt.Println("services", len(services))
	fmt.Println("converting trips")

	routeIndex := make(map[string]uint32, len(feed.Routes))
	for i, route := range feed.Routes {
		routeIndex[route.ID] = uint32(i)
	}

	procTrips := make([][]uint32, len(feed.Trips))
	procTripsIndex := make(map[string]uint32, len(feed.Trips))

	for i, trip := range feed.Trips {
		procTrips[i] = make([]uint32, 0)

		procTripsIndex[trip.ID] = uint32(i)
	}

	fmt.Println("trips", len(procTrips))
	fmt.Println("converting stop times")

	for i, stopTime := range feed.StopsTimes {
		tripKey := procTripsIndex[stopTime.TripID]
		trip := procTrips[tripKey]
		trip = append(trip, uint32(i))
		procTrips[tripKey] = trip
	}

	fmt.Println("expanding routes to distinct stop sequences")

	tripRoutes := make([]map[string][]uint32, len(feed.Routes))

	for tripKey, trip := range procTrips {
		sort.Slice(trip, func(i, j int) bool {
			stI := feed.StopsTimes[trip[i]]
			stJ := feed.StopsTimes[trip[j]]

			return stI.StopSeq < stJ.StopSeq
		})

		stopIds := make([]string, len(trip))
		for i, stopTimeKey := range trip {
			stopIds[i] = feed.StopsTimes[stopTimeKey].StopID
		}

		routeId := feed.Trips[tripKey].RouteID
		routeKey := routeIndex[routeId]

		tripRoute := tripRoutes[routeKey]

		if tripRoute == nil {
			tripRoute = make(map[string][]uint32)
		}

		routeSeqId := strings.Join(stopIds, "/\\/")

		routeTrips, ok := tripRoute[routeSeqId]
		if !ok {
			routeTrips = make([]uint32, 0)
		}

		routeTrips = append(routeTrips, uint32(tripKey))
		tripRoute[routeSeqId] = routeTrips
		tripRoutes[routeKey] = tripRoute
	}

	fmt.Println("creating stop route pairs")

	stopRoutePairs := make([][]StopRoutePair, len(stops))
	for i := range stopRoutePairs {
		stopRoutePairs[i] = make([]StopRoutePair, 0)
	}

	maxTripDayLength := uint32(0)

	fmt.Println("converting trips")

	trips := make([]*Trip, len(procTrips))
	for i, trip := range procTrips {
		st := make([]Stopover, len(trip))
		for j, stopTimeKey := range trip {
			arr := getTimeInMs(feed.StopsTimes[stopTimeKey].Arrival)
			dep := getTimeInMs(feed.StopsTimes[stopTimeKey].Departure)

			arrDays := arr / DayInMs
			depDays := dep / DayInMs

			if arrDays > maxTripDayLength {
				maxTripDayLength = arrDays
			}

			if depDays > maxTripDayLength {
				maxTripDayLength = depDays
			}

			st[j] = Stopover{
				Arrival:   uint32(arr),
				Departure: uint32(dep),
			}
		}

		serviceKey := servicesIndex[feed.Trips[i].ServiceID]

		trips[i] = &Trip{
			StopTimes: st,
			Service:   serviceKey,
		}
	}

	fmt.Println("trips", len(trips))
	fmt.Println("creating routes")

	routes := make([]*Route, 0)
	raptorToGtfsRoutes := make([]uint32, 0)
	reorders := make(map[uint64][]uint32)

	for gtfsRouteKey, feedRouteCollection := range tripRoutes {
		for _, route := range feedRouteCollection {
			firstTrip := procTrips[route[0]]

			routeKey := len(routes)

			routeStops := make([]uint64, len(firstTrip))
			for i, stopTimeKey := range firstTrip {
				stop := stopsIndex[feed.StopsTimes[stopTimeKey].StopID]
				routeStops[i] = stop

				pair := StopRoutePair{
					Route:         uint32(routeKey),
					StopKeyInTrip: uint32(i),
				}

				stopRoutePairs[stop] = append(stopRoutePairs[stop], pair)
			}

			sort.Slice(route, func(i, j int) bool {
				tripI := trips[route[i]]
				tripJ := trips[route[j]]

				return tripI.StopTimes[0].Departure < tripJ.StopTimes[0].Departure
			})

			unsortedStops := make([]uint32, 0)

			for stop := 0; stop < len(routeStops); stop++ {
				last := trips[route[0]].StopTimes[stop].Departure

				for _, tripKey := range route {
					trip := trips[tripKey]
					current := trip.StopTimes[stop].Departure
					if current < last {
						unsortedStops = append(unsortedStops, uint32(stop))
						break
					}
					last = current
				}
			}

			for _, stopSeqKey := range unsortedStops {
				routeStopKey := uint64(routeKey)<<32 | uint64(stopSeqKey)

				reorder := make([]uint32, len(route))
				for i := range route {
					reorder[i] = uint32(i)
				}

				sort.Slice(reorder, func(i, j int) bool {
					tripI := trips[route[reorder[i]]]
					tripJ := trips[route[reorder[j]]]

					return tripI.StopTimes[stopSeqKey].Departure < tripJ.StopTimes[stopSeqKey].Departure
				})

				reorders[routeStopKey] = reorder
			}

			routes = append(routes, &Route{
				Stops: routeStops,
				Trips: route,
			})
			raptorToGtfsRoutes = append(raptorToGtfsRoutes, uint32(gtfsRouteKey))
		}
	}

	fmt.Println("routes", len(routes))
	/*fmt.Println("creating transfer graph")

	transferGraph := make([][]Arc, len(stops))
	for i := range stops {
		transferGraph[i] = make([]Arc, 0)
	}

	for i, from := range stops {
		for jShifted, to := range stops[i+1:] {
			j := i + 1 + jShifted

			distInKm := Distance(from.Latitude, from.Longitude, to.Latitude, to.Longitude, "K")
			distInSecs := (distInKm * 1000) / WalkingSpeed

			if distInSecs > MaxWalkingMs {
				continue
			}

			transferGraph[i] = append(transferGraph[i], Arc{
				Target:   uint32(j),
				Distance: uint32(distInSecs),
			})
			transferGraph[j] = append(transferGraph[j], Arc{
				Target:   uint32(i),
				Distance: uint32(distInSecs),
			})
		}
	}

	fmt.Println("transfer graph", len(transferGraph))*/
	fmt.Println("creating route information")

	routeInformation := make([]*RouteInformation, len(feed.Routes))
	for i, route := range feed.Routes {
		routeInformation[i] = &RouteInformation{
			ShortName: route.ShortName,
		}
	}

	fmt.Println("route information", len(routeInformation))
	fmt.Println("creating trip information")

	tripInformation := make([]*TripInformation, len(feed.Trips))
	for i, trip := range feed.Trips {
		tripInformation[i] = &TripInformation{
			Headsign: trip.Headsign,
		}
	}

	fmt.Println("trip information", len(tripInformation))
	fmt.Println("done")

	return &RaptorData{
		MaxTripDayLength: maxTripDayLength,
		Vertices:         stops,
		StopsIndex:       stopsIndex,
		Routes:           routes,
		StopToRoutes:     stopRoutePairs,
		Trips:            trips,
		//StreetGraph:    transferGraph,
		Reorders: reorders,
		Services: services,

		RaptorToGtfsRoutes: raptorToGtfsRoutes,
		RouteInformation:   routeInformation,
		TripInformation:    tripInformation,
	}
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
