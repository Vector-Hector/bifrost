package main

import (
	"fmt"
	"github.com/artonge/go-gtfs"
	"github.com/kyroy/kdtree"
	"math"
	gtfs_stream "raptor/gtfs"
	"sort"
	"strconv"
	"strings"
	"time"
)

type StopTime struct {
	Departure uint32
	Arrival   uint32
	StopSeq   uint32
	StopKey   uint32
}

type Progress struct {
	Start     time.Time
	LastPrint time.Time
	Current   uint64
	Total     uint64
}

func (p *Progress) Reset(total uint64) {
	p.Start = time.Now()
	p.Current = 0
	p.Total = total
	p.LastPrint = time.Now()
}

func (p *Progress) Increment() {
	p.Current++
}

// Print prints the current progress bar to the console with current, total, percentage and ETA
func (p *Progress) Print() {
	if time.Since(p.LastPrint) < time.Second {
		return
	}
	p.LastPrint = time.Now()
	fmt.Printf("\r%v/%v (%.2f%%) ETA: %v", p.Current, p.Total, float64(p.Current)/float64(p.Total)*100, p.ETA())
}

// ETA returns the estimated time of arrival
func (p *Progress) ETA() string {
	elapsed := time.Since(p.Start)
	eta := time.Duration(float64(elapsed) / float64(p.Current) * float64(p.Total-p.Current))
	return eta.String()
}

func ReadGtfsData(directory string) (*RaptorData, error) {
	stopCount, err := gtfs_stream.CountRows(directory + "/stops.txt")
	if err != nil {
		return nil, err
	}

	prog := &Progress{}

	stops := make([]*Stop, stopCount)
	stopsAsPoints := make([]kdtree.Point, stopCount)
	stopToRoutes := make([][]StopRoutePair, stopCount)
	stopsIndex := make(map[string]uint32, stopCount)

	prog.Reset(uint64(stopCount))
	err = gtfs_stream.IterateStops(directory+"/stops.txt", func(index int, stop *gtfs.Stop) bool {
		prog.Increment()
		prog.Print()

		stops[index] = &Stop{
			Id:        stop.ID,
			Name:      stop.Name,
			Longitude: stop.Longitude,
			Latitude:  stop.Latitude,
		}
		stopsAsPoints[index] = &GeoPoint{
			Latitude:  stop.Longitude,
			Longitude: stop.Latitude,
			StopKey:   uint32(index),
		}

		stopsIndex[stop.ID] = uint32(index)
		stopToRoutes[index] = make([]StopRoutePair, 0)
		return true
	})
	if err != nil {
		return nil, err
	}
	fmt.Println()

	fmt.Println("stops", stopCount)

	fmt.Println("building kdtree")
	stopsTree := kdtree.New(stopsAsPoints)

	fmt.Println("creating transfer graph")

	transferGraph := make([][]Arc, len(stops))
	for i := range stops {
		transferGraph[i] = make([]Arc, 0)
	}

	prog.Reset(uint64(len(stops)))

	for _, fromPoint := range stopsAsPoints {
		prog.Increment()
		prog.Print()

		neighbours := stopsTree.KNN(fromPoint, 500)

		from := fromPoint.(*GeoPoint)

		for _, toPoint := range neighbours {
			to := toPoint.(*GeoPoint)

			if from.StopKey == to.StopKey {
				continue
			}

			if !fastDistWithin(from, to, MaxWalkingSeconds) {
				break
			}

			distInKm := Distance(from.Latitude, from.Longitude, to.Latitude, to.Longitude, "K")
			distInSecs := (distInKm * 1000) / WalkingSpeed

			if distInSecs > MaxWalkingSeconds {
				break
			}

			fromKey := from.StopKey
			toKey := to.StopKey

			transferGraph[fromKey] = append(transferGraph[fromKey], Arc{
				Target:   toKey,
				Distance: uint32(distInSecs),
			})
			transferGraph[toKey] = append(transferGraph[toKey], Arc{
				Target:   fromKey,
				Distance: uint32(distInSecs),
			})
		}
	}

	fmt.Println("transfer graph", len(transferGraph))
	fmt.Println("converting services")

	serviceCount, err := gtfs_stream.CountRows(directory + "/calendar.txt")
	if err != nil {
		return nil, err
	}

	services := make([]*Service, serviceCount)
	servicesIndex := make(map[string]uint32, serviceCount)

	prog.Reset(uint64(serviceCount))
	err = gtfs_stream.IterateServices(directory+"/calendar.txt", func(index int, calendar *gtfs.Calendar) bool {
		prog.Increment()
		prog.Print()
		services[index] = &Service{
			Weekdays:          uint8(calendar.Monday) | uint8(calendar.Tuesday)<<1 | uint8(calendar.Wednesday)<<2 | uint8(calendar.Thursday)<<3 | uint8(calendar.Friday)<<4 | uint8(calendar.Saturday)<<5 | uint8(calendar.Sunday)<<6,
			StartDay:          getDaySincePivotDate(calendar.Start),
			EndDay:            getDaySincePivotDate(calendar.End),
			AddedExceptions:   make([]uint32, 0),
			RemovedExceptions: make([]uint32, 0),
		}

		servicesIndex[calendar.ServiceID] = uint32(index)

		return true
	})
	if err != nil {
		return nil, err
	}
	fmt.Println()

	fmt.Println("iterating calendar dates")

	err = gtfs_stream.IterateCalendarDates(directory+"/calendar_dates.txt", func(index int, calendarDate *gtfs.CalendarDate) bool {
		service := services[servicesIndex[calendarDate.ServiceID]]

		switch calendarDate.ExceptionType {
		case 1:
			service.AddedExceptions = append(service.AddedExceptions, getDaySincePivotDate(calendarDate.Date))
		case 2:
			service.RemovedExceptions = append(service.RemovedExceptions, getDaySincePivotDate(calendarDate.Date))
		}

		return true
	})
	if err != nil {
		return nil, err
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

	routeCount, err := gtfs_stream.CountRows(directory + "/routes.txt")
	if err != nil {
		return nil, err
	}

	routeIndex := make(map[string]uint32, routeCount)
	routeInformation := make([]*RouteInformation, routeCount)

	prog.Reset(uint64(routeCount))
	err = gtfs_stream.IterateRoutes(directory+"/routes.txt", func(index int, route *gtfs.Route) bool {
		prog.Increment()
		prog.Print()

		routeIndex[route.ID] = uint32(index)
		routeInformation[index] = &RouteInformation{
			ShortName: route.ShortName,
		}
		return true
	})
	if err != nil {
		return nil, err
	}
	fmt.Println()

	tripCount, err := gtfs_stream.CountRows(directory + "/trips.txt")
	if err != nil {
		return nil, err
	}

	procTrips := make([][]uint32, tripCount)
	tripToRouteKey := make([]uint32, tripCount)
	tripToServiceKey := make([]uint32, tripCount)
	procTripsIndex := make(map[string]uint32, tripCount)
	tripInformation := make([]*TripInformation, tripCount)

	prog.Reset(uint64(tripCount))
	err = gtfs_stream.IterateTrips(directory+"/trips.txt", func(index int, trip *gtfs.Trip) bool {
		prog.Increment()
		prog.Print()

		procTrips[index] = make([]uint32, 0)
		tripToRouteKey[index] = routeIndex[trip.RouteID]
		tripToServiceKey[index] = servicesIndex[trip.ServiceID]
		procTripsIndex[trip.ID] = uint32(index)
		tripInformation[index] = &TripInformation{
			Headsign: trip.Headsign,
		}
		return true
	})
	if err != nil {
		return nil, err
	}
	fmt.Println()

	fmt.Println("trips", tripCount)
	fmt.Println("converting stop times")

	stopTimeCount, err := gtfs_stream.CountRows(directory + "/stop_times.txt")
	if err != nil {
		return nil, err
	}

	stopTimes := make([]*StopTime, stopTimeCount)

	prog.Reset(uint64(stopTimeCount))
	err = gtfs_stream.IterateStopTimes(directory+"/stop_times.txt", func(index int, stopTime *gtfs.StopTime) bool {
		prog.Increment()
		prog.Print()

		tripKey := procTripsIndex[stopTime.TripID]
		procTrips[tripKey] = append(procTrips[tripKey], uint32(index))
		stopTimes[index] = &StopTime{
			Departure: getTimeInSeconds(stopTime.Departure),
			Arrival:   getTimeInSeconds(stopTime.Arrival),
			StopSeq:   stopTime.StopSeq,
			StopKey:   stopsIndex[stopTime.StopID],
		}
		return true
	})
	if err != nil {
		return nil, err
	}
	fmt.Println()

	fmt.Println("expanding routes to distinct stop sequences")

	tripRoutes := make([]map[string][]uint32, routeCount)

	for tripKey, trip := range procTrips {
		sort.Slice(trip, func(i, j int) bool {
			stI := stopTimes[trip[i]]
			stJ := stopTimes[trip[j]]

			return stI.StopSeq < stJ.StopSeq
		})

		stopIds := make([]string, len(trip))
		for i, stopTimeKey := range trip {
			stopIds[i] = strconv.Itoa(int(stopTimes[stopTimeKey].StopKey))
		}

		routeKey := tripToRouteKey[tripKey]

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

	stopRoutePairs := make([][]StopRoutePair, stopCount)
	for i := range stopRoutePairs {
		stopRoutePairs[i] = make([]StopRoutePair, 0)
	}

	maxTripDayLength := uint32(0)

	fmt.Println("converting trips")

	trips := make([]*Trip, tripCount)
	for i, trip := range procTrips {
		st := make([]Stopover, len(trip))
		for j, stopTimeKey := range trip {
			stopTime := stopTimes[stopTimeKey]
			arr := stopTime.Arrival
			dep := stopTime.Departure

			arrDays := arr / DayInSeconds
			depDays := dep / DayInSeconds

			if arrDays > maxTripDayLength {
				maxTripDayLength = arrDays
			}

			if depDays > maxTripDayLength {
				maxTripDayLength = depDays
			}

			st[j] = Stopover{
				Arrival:   arr,
				Departure: dep,
			}
		}

		serviceKey := tripToServiceKey[i]

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

			routeStops := make([]uint32, len(firstTrip))
			for i, stopTimeKey := range firstTrip {
				stop := stopTimes[stopTimeKey].StopKey
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

	return &RaptorData{
		MaxTripDayLength: maxTripDayLength,
		Stops:            stops,
		StopsIndex:       stopsIndex,
		Routes:           routes,
		StopToRoutes:     stopRoutePairs,
		Trips:            trips,
		TransferGraph:    transferGraph,
		Reorders:         reorders,
		Services:         services,

		RaptorToGtfsRoutes: raptorToGtfsRoutes,
		RouteInformation:   routeInformation,
		TripInformation:    tripInformation,
	}, nil
}

func fastDistWithin(from *GeoPoint, to *GeoPoint, maxSecondsDist int) bool {
	latDiffInSecs := (math.Abs(from.Latitude-to.Latitude) * 111 * 1000) / WalkingSpeed

	if latDiffInSecs > float64(maxSecondsDist) {
		return false
	}

	lonDiffInSecs := (math.Abs(from.Longitude-to.Longitude) * 111 * math.Cos(from.Latitude) * 1000) / WalkingSpeed

	if lonDiffInSecs > float64(maxSecondsDist) {
		return false
	}

	return true
}
