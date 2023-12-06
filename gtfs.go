package bifrost

import (
	"fmt"
	"github.com/Vector-Hector/bifrost/stream"
	"github.com/artonge/go-gtfs"
	"github.com/kyroy/kdtree"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

type gtfsStopTime struct {
	Departure uint32
	Arrival   uint32
	StopSeq   uint32
	StopKey   uint64
}

func timeStringToMs(timeStr string) uint32 {
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

func getUnixDay(date string) uint32 {
	t, err := time.Parse("20060102", date)
	if err != nil {
		panic(err)
	}

	return uint32(uint64(t.UnixMilli()) / uint64(DayInMs))
}

func (b *Bifrost) DistanceMs(from kdtree.Point, to kdtree.Point) uint32 {
	if from.Dimensions() != 2 || to.Dimensions() != 2 {
		panic("invalid dimension")
	}

	distInKm := Distance(from.Dimension(0), from.Dimension(1), to.Dimension(0), to.Dimension(1), "K")
	distInMs := (distInKm * 1000) / b.WalkingSpeed
	res := uint32(math.Ceil(distInMs))
	if res == 0 {
		return 1
	}
	return res
}

// Distance https://www.geodatasource.com/developers/go
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

func (b *Bifrost) AddGtfs(directory string) error {
	// todo merge directly instead of using a temporary struct. see AddStreetData on how it's supposed to work

	directory = strings.TrimSuffix(directory, "/")

	stopCount, err := stream.CountRows(directory + "/stops.txt")
	if err != nil {
		return err
	}

	prog := &Progress{}

	stops := make([]Vertex, stopCount)
	stopsAsPoints := make([]kdtree.Point, stopCount)
	stopToRoutes := make([][]StopRoutePair, stopCount)
	stopsIndex := make(map[string]uint64, stopCount)

	prog.Reset(uint64(stopCount))
	err = stream.IterateStops(directory+"/stops.txt", func(index int, stop *gtfs.Stop) bool {
		prog.Increment()
		prog.Print()

		stops[index] = Vertex{
			Stop: &StopContext{
				Id:   stop.ID,
				Name: stop.Name,
			},
			Longitude: stop.Longitude,
			Latitude:  stop.Latitude,
		}
		stopsAsPoints[index] = &GeoPoint{
			Latitude:  stop.Longitude,
			Longitude: stop.Latitude,
			VertKey:   uint64(index),
		}

		stopsIndex[stop.ID] = uint64(index)
		stopToRoutes[index] = make([]StopRoutePair, 0)
		return true
	})
	if err != nil {
		return err
	}
	fmt.Println()

	fmt.Println("stops", stopCount)

	fmt.Println("converting services")

	serviceCount, err := stream.CountRows(directory + "/calendar.txt")
	if err != nil {
		return err
	}

	services := make([]*Service, serviceCount)
	servicesIndex := make(map[string]uint32, serviceCount)

	prog.Reset(uint64(serviceCount))
	err = stream.IterateServices(directory+"/calendar.txt", func(index int, calendar *gtfs.Calendar) bool {
		prog.Increment()
		prog.Print()
		services[index] = &Service{
			Weekdays:          uint8(calendar.Monday) | uint8(calendar.Tuesday)<<1 | uint8(calendar.Wednesday)<<2 | uint8(calendar.Thursday)<<3 | uint8(calendar.Friday)<<4 | uint8(calendar.Saturday)<<5 | uint8(calendar.Sunday)<<6,
			StartDay:          getUnixDay(calendar.Start),
			EndDay:            getUnixDay(calendar.End),
			AddedExceptions:   make([]uint32, 0),
			RemovedExceptions: make([]uint32, 0),
		}

		servicesIndex[calendar.ServiceID] = uint32(index)

		return true
	})
	if err != nil {
		return err
	}
	fmt.Println()

	fmt.Println("iterating calendar dates")

	err = stream.IterateCalendarDates(directory+"/calendar_dates.txt", func(index int, calendarDate *gtfs.CalendarDate) bool {
		service := services[servicesIndex[calendarDate.ServiceID]]

		switch calendarDate.ExceptionType {
		case 1:
			service.AddedExceptions = append(service.AddedExceptions, getUnixDay(calendarDate.Date))
		case 2:
			service.RemovedExceptions = append(service.RemovedExceptions, getUnixDay(calendarDate.Date))
		}

		return true
	})
	if err != nil {
		return err
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

	routeCount, err := stream.CountRows(directory + "/routes.txt")
	if err != nil {
		return err
	}

	routeIndex := make(map[string]uint32, routeCount)
	routeInformation := make([]*RouteInformation, routeCount)

	prog.Reset(uint64(routeCount))
	err = stream.IterateRoutes(directory+"/routes.txt", func(index int, route *gtfs.Route) bool {
		prog.Increment()
		prog.Print()

		routeIndex[route.ID] = uint32(index)
		routeInformation[index] = &RouteInformation{
			ShortName: route.ShortName,
			RouteId:   route.ID,
		}
		return true
	})
	if err != nil {
		return err
	}
	fmt.Println()

	tripCount, err := stream.CountRows(directory + "/trips.txt")
	if err != nil {
		return err
	}

	procTrips := make([][]uint32, tripCount)
	tripToRouteKey := make([]uint32, tripCount)
	tripToServiceKey := make([]uint32, tripCount)
	procTripsIndex := make(map[string]uint32, tripCount)
	tripInformation := make([]*TripInformation, tripCount)

	prog.Reset(uint64(tripCount))
	err = stream.IterateTrips(directory+"/trips.txt", func(index int, trip *gtfs.Trip) bool {
		prog.Increment()
		prog.Print()

		procTrips[index] = make([]uint32, 0)
		tripToRouteKey[index] = routeIndex[trip.RouteID]
		tripToServiceKey[index] = servicesIndex[trip.ServiceID]
		procTripsIndex[trip.ID] = uint32(index)
		tripInformation[index] = &TripInformation{
			Headsign: trip.Headsign,
			TripId:   trip.ID,
		}
		return true
	})
	if err != nil {
		return err
	}
	fmt.Println()

	fmt.Println("trips", tripCount)
	fmt.Println("converting stop times")

	stopTimeCount, err := stream.CountRows(directory + "/stop_times.txt")
	if err != nil {
		return err
	}

	stopTimes := make([]*gtfsStopTime, stopTimeCount)

	prog.Reset(uint64(stopTimeCount))
	err = stream.IterateStopTimes(directory+"/stop_times.txt", func(index int, stopTime *gtfs.StopTime) bool {
		prog.Increment()
		prog.Print()

		tripKey := procTripsIndex[stopTime.TripID]
		procTrips[tripKey] = append(procTrips[tripKey], uint32(index))
		stopTimes[index] = &gtfsStopTime{
			Departure: timeStringToMs(stopTime.Departure),
			Arrival:   timeStringToMs(stopTime.Arrival),
			StopSeq:   stopTime.StopSeq,
			StopKey:   stopsIndex[stopTime.StopID],
		}
		return true
	})
	if err != nil {
		return err
	}
	fmt.Println()

	fmt.Println("expanding routes to distinct stop sequences")

	tripRoutes := make([]map[string][]uint32, routeCount)

	for tripKey, trip := range procTrips {
		if len(trip) == 0 {
			continue
		}

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
		if len(trip) == 0 {
			continue
		}

		st := make([]Stopover, len(trip))
		for j, stopTimeKey := range trip {
			stopTime := stopTimes[stopTimeKey]
			arr := stopTime.Arrival
			dep := stopTime.Departure

			arrDays := arr / DayInMs
			depDays := dep / DayInMs

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

			routeStops := make([]uint64, len(firstTrip))
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

	tripToRoute := make([]uint32, tripCount)

	for i, route := range routes {
		for _, tripKey := range route.Trips {
			tripToRoute[tripKey] = uint32(i)
		}
	}

	b.MergeData(&RoutingData{
		MaxTripDayLength: maxTripDayLength,
		Vertices:         stops,
		StopsIndex:       stopsIndex,
		Routes:           routes,
		StopToRoutes:     stopRoutePairs,
		Trips:            trips,
		Reorders:         reorders,
		Services:         services,

		GtfsRouteIndex:   raptorToGtfsRoutes,
		RouteInformation: routeInformation,
		TripInformation:  tripInformation,
		TripToRoute:      tripToRoute,

		StreetGraph: make([][]Arc, len(stops)),
		NodesIndex:  make(map[int64]uint64),
	})

	return nil
}

func (b *Bifrost) fastDistWithin(from kdtree.Point, to kdtree.Point, maxMsDist uint32) bool {
	if from.Dimensions() != 2 || to.Dimensions() != 2 {
		panic("invalid dimension")
	}

	latDiffInMs := (math.Abs(from.Dimension(0)-to.Dimension(0)) * 111 * 1000) / b.WalkingSpeed

	if latDiffInMs > float64(maxMsDist) {
		return false
	}

	lonDiffInSecs := (math.Abs(from.Dimension(1)-to.Dimension(1)) * 111 * math.Cos(from.Dimension(0)) * 1000) / b.WalkingSpeed

	if lonDiffInSecs > float64(maxMsDist) {
		return false
	}

	return true
}
