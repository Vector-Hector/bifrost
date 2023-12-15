package bifrost

import (
	"fmt"
	"github.com/Vector-Hector/fptf"
	util "github.com/Vector-Hector/goutil"
	"strconv"
	"time"
)

func (b *Bifrost) ReconstructJourney(destKey uint64, lastRound int, rounds *Rounds) *fptf.Journey {
	// reconstruct path
	trips := make([]*fptf.Trip, 0)
	position := destKey

	for i := lastRound; i > 0; i-- {
		arr, ok := rounds.Rounds[i][position]

		if !ok {
			panic("position does not exist in round")
		}

		if arr.Trip == TripIdNoChange {
			continue
		}

		if arr.Trip == TripIdWalk || arr.Trip == TripIdCycle || arr.Trip == TripIdCar {
			trip, newPos := GetTripFromTransfer(b.Data, rounds.Rounds[i], position, arr.Trip)
			position = newPos
			trips = append(trips, trip)
			continue
		}

		trip, newPos := GetTripFromTrip(b.Data, rounds.Rounds[i-1], arr)
		position = newPos
		trips = append(trips, trip)
	}

	// reverse trips
	for i := len(trips)/2 - 1; i >= 0; i-- {
		opp := len(trips) - 1 - i
		trips[i], trips[opp] = trips[opp], trips[i]
	}

	return &fptf.Journey{
		Trips: trips,
	}
}

func GetTripFromTransfer(r *RoutingData, round map[uint64]StopArrival, destination uint64, tripType uint32) (*fptf.Trip, uint64) {
	mode := fptf.ModeWalking
	if tripType == TripIdCycle {
		mode = fptf.ModeBicycle
	} else if tripType == TripIdCar {
		mode = fptf.ModeCar
	}

	position := destination
	arrival := round[position]
	path := make([]uint64, 1)
	path[0] = position

	for {
		if arrival.Trip != tripType {
			break
		}

		prevPos := arrival.EnterKey
		prevArr := round[prevPos]

		if prevArr.Arrival > arrival.Arrival {
			panic("transfer arrival is before enter")
		}

		position = prevPos
		arrival = prevArr
		path = append(path, position)
	}

	stopovers := make([]*fptf.Stopover, 0, len(path))
	for i := len(path) - 1; i >= 0; i-- {
		stop := path[i]
		sa := round[stop]
		stopover := &fptf.Stopover{
			StopStation: r.GetFptfStop(stop),
		}
		if i != len(path)-1 {
			stopover.Arrival = r.GetTime(sa.Arrival)
		}
		if i != 0 {
			stopover.Departure = r.GetTime(sa.Arrival)
		}
		stopovers = append(stopovers, stopover)
	}

	originStop := stopovers[0].StopStation
	destStop := stopovers[len(stopovers)-1].StopStation
	dep := stopovers[0].Departure
	arr := stopovers[len(stopovers)-1].Arrival

	trip := &fptf.Trip{
		Origin:      originStop,
		Destination: destStop,
		Departure:   dep,
		Arrival:     arr,
		Stopovers:   stopovers,
		Mode:        mode,
	}

	return trip, position
}

func (r *RoutingData) GetFptfStop(stop uint64) *fptf.StopStation {
	id := ""
	name := ""

	if stopCtx := r.Vertices[stop].Stop; stopCtx != nil {
		id = stopCtx.Id
		name = stopCtx.Name + " / " + strconv.Itoa(int(stop))
	}

	return &fptf.StopStation{
		Station: &fptf.Station{
			Id:   id,
			Name: name,
			Location: &fptf.Location{
				Latitude:  r.Vertices[stop].Latitude,
				Longitude: r.Vertices[stop].Longitude,
			},
		},
	}
}

func (r *RoutingData) GetTime(ms uint64) fptf.TimeNullable {
	return fptf.TimeNullable{
		Time: time.Unix(int64(ms/1000), int64(ms%1000)*1000000),
	}
}

func GetTripFromTrip(r *RoutingData, round map[uint64]StopArrival, arrival StopArrival) (*fptf.Trip, uint64) {
	// todo add support for these trip leg fields:
	// todo - trip.Schedule
	// todo - trip.Mode (split between bus, train and watercraft)
	// todo - trip.Operator

	trip := r.Trips[arrival.Trip]
	routeKey := r.TripToRoute[arrival.Trip]
	route := r.Routes[routeKey]

	enterKey := 0

	for i := int(arrival.EnterKey) - 1; i >= 0; i-- {
		stop := route.Stops[i]
		_, ok := round[stop]
		if !ok {
			continue
		}

		enterKey = i
		break
	}

	if enterKey == 0 {
		_, ok := round[route.Stops[0]]
		if !ok {
			util.PrintJSON(arrival)
			panic(fmt.Sprint("no enter key found for trip ", arrival.Trip, " at route ", routeKey))
		}
	}

	gtfsRouteKey := r.GtfsRouteIndex[routeKey]
	gtfsRoute := r.RouteInformation[gtfsRouteKey]
	gtfsTrip := r.TripInformation[arrival.Trip]

	routeName := gtfsRoute.ShortName

	originStop := r.GetFptfStop(route.Stops[enterKey])
	destStop := r.GetFptfStop(route.Stops[arrival.EnterKey])

	dep := r.GetTime(trip.StopTimes[enterKey].DepartureAtDay(arrival.Departure))
	arr := r.GetTime(trip.StopTimes[arrival.EnterKey].ArrivalAtDay(arrival.Departure))

	stopovers := make([]*fptf.Stopover, 0, int(arrival.EnterKey)-enterKey+1)
	for i := enterKey; i <= int(arrival.EnterKey); i++ {
		stop := route.Stops[i]
		stopover := &fptf.Stopover{
			StopStation: r.GetFptfStop(stop),
			Arrival:     r.GetTime(trip.StopTimes[i].ArrivalAtDay(arrival.Departure)),
			Departure:   r.GetTime(trip.StopTimes[i].DepartureAtDay(arrival.Departure)),
		}
		stopovers = append(stopovers, stopover)
	}

	result := &fptf.Trip{
		Origin:      originStop,
		Destination: destStop,
		Departure:   dep,
		Arrival:     arr,
		Stopovers:   stopovers,
		Line: &fptf.Line{
			Id:   gtfsTrip.TripId,
			Mode: fptf.ModeTrain,
			Name: routeName,
		},
		Mode:      fptf.ModeTrain,
		Direction: gtfsTrip.Headsign,
	}

	return result, route.Stops[enterKey]
}

func (b *Bifrost) addSourceAndDestination(journey *fptf.Journey, sources []SourceLocation, dest *fptf.Location) {
	b.addJourneyDestination(journey, dest)

	originStopStation := journey.GetOrigin()
	if originStopStation == nil {
		return
	}

	origin := originStopStation.GetLocation()
	if origin == nil {
		return
	}

	originPoint := &GeoPoint{
		Latitude:  origin.Latitude,
		Longitude: origin.Longitude,
	}

	minSourceDistance := uint32(0)
	minSourceKey := -1

	for i, source := range sources {
		distance := b.DistanceMs(&GeoPoint{
			Latitude:  source.Location.Latitude,
			Longitude: source.Location.Longitude,
		}, originPoint, VehicleTypeWalking)

		if minSourceKey == -1 || distance < minSourceDistance {
			minSourceDistance = distance
			minSourceKey = i
		}
	}

	if minSourceKey == -1 {
		return
	}

	b.addJourneyOrigin(journey, sources[minSourceKey].Location)
}

func (b *Bifrost) addJourneyOrigin(journey *fptf.Journey, origin *fptf.Location) {
	firstTrip := journey.GetFirstTrip()
	if firstTrip == nil {
		return
	}

	journeyOrigin := journey.GetOrigin()
	journeyOriginLoc := journey.GetOrigin().GetLocation()

	if journeyOriginLoc == nil {
		return
	}

	vehicleType := VehicleTypeWalking
	willAddTrip := true

	if firstTrip.Mode == fptf.ModeWalking {
		vehicleType = VehicleTypeWalking
		willAddTrip = false
	} else if firstTrip.Mode == fptf.ModeBicycle {
		vehicleType = VehicleTypeBicycle
		willAddTrip = false
	} else if firstTrip.Mode == fptf.ModeCar {
		vehicleType = VehicleTypeCar
		willAddTrip = false
	}

	dist := uint64(b.DistanceMs(&GeoPoint{
		Latitude:  origin.Latitude,
		Longitude: origin.Longitude,
	}, &GeoPoint{
		Latitude:  journeyOriginLoc.Latitude,
		Longitude: journeyOriginLoc.Longitude,
	}, vehicleType))

	pad := b.TransferPaddingMs
	if !willAddTrip {
		pad = 0
	}

	dist += pad

	journeyDep := journey.GetDeparture()
	journeyDepDelay := journey.GetDepartureDelay()
	if journeyDepDelay != nil {
		journeyDep = journeyDep.Add(time.Duration(*journeyDepDelay) * time.Second)
	}

	newDep := journeyDep.Add(-time.Duration(dist) * time.Millisecond)
	newArrAtOrigin := journeyDep.Add(-time.Duration(pad) * time.Millisecond)
	newOrigin := &fptf.StopStation{
		Station: &fptf.Station{
			Name: "origin",
			Location: &fptf.Location{
				Latitude:  origin.Latitude,
				Longitude: origin.Longitude,
			},
		},
	}

	if !willAddTrip {
		addTripOrigin(firstTrip, newOrigin, newDep, newArrAtOrigin)
		return
	}

	// create new walk trip
	walkTrip := &fptf.Trip{
		Origin:      newOrigin,
		Destination: journeyOrigin,
		Departure:   fptf.TimeNullable{Time: newDep},
		Arrival:     fptf.TimeNullable{Time: newArrAtOrigin},
		Stopovers: []*fptf.Stopover{{
			StopStation: newOrigin,
			Departure:   fptf.TimeNullable{Time: newDep},
		}, {
			StopStation: journeyOrigin,
			Arrival:     fptf.TimeNullable{Time: newArrAtOrigin},
		}},
		Mode: fptf.ModeWalking,
	}

	journey.Trips = append([]*fptf.Trip{walkTrip}, journey.Trips...)
}

func addTripOrigin(trip *fptf.Trip, newOrigin *fptf.StopStation, newDep time.Time, newArrAtOrigin time.Time) {
	trip.Origin = newOrigin
	trip.Departure = fptf.TimeNullable{Time: newDep}
	trip.Stopovers = append([]*fptf.Stopover{{
		StopStation: newOrigin,
		Departure:   fptf.TimeNullable{Time: newDep},
	}}, trip.Stopovers...)
	trip.Stopovers[1].Arrival = fptf.TimeNullable{Time: newArrAtOrigin}
}

func (b *Bifrost) addJourneyDestination(journey *fptf.Journey, dest *fptf.Location) {
	lastTrip := journey.GetLastTrip()
	if lastTrip == nil {
		return
	}

	journeyDest := journey.GetDestination()
	journeyDestLoc := journey.GetDestination().GetLocation()

	if journeyDestLoc == nil {
		return
	}

	vehicleType := VehicleTypeWalking
	if lastTrip.Mode == fptf.ModeBicycle {
		vehicleType = VehicleTypeBicycle
	} else if lastTrip.Mode == fptf.ModeCar {
		vehicleType = VehicleTypeCar
	}

	dist := uint64(b.DistanceMs(&GeoPoint{
		Latitude:  dest.Latitude,
		Longitude: dest.Longitude,
	}, &GeoPoint{
		Latitude:  journeyDestLoc.Latitude,
		Longitude: journeyDestLoc.Longitude,
	}, vehicleType))

	journeyArr := journey.GetArrival()
	journeyArrDelay := journey.GetArrivalDelay()
	if journeyArrDelay != nil {
		journeyArr = journeyArr.Add(time.Duration(*journeyArrDelay) * time.Second)
	}

	newArr := journeyArr.Add(time.Duration(dist) * time.Millisecond)
	newDest := &fptf.StopStation{
		Station: &fptf.Station{
			Name: "destination",
			Location: &fptf.Location{
				Latitude:  dest.Latitude,
				Longitude: dest.Longitude,
			},
		},
	}

	if lastTrip.Mode == fptf.ModeWalking || lastTrip.Mode == fptf.ModeBicycle || lastTrip.Mode == fptf.ModeCar {
		addTripDestination(lastTrip, newDest, newArr, journeyArr)
		return
	}

	// create new walk trip
	walkTrip := &fptf.Trip{
		Origin:      journeyDest,
		Destination: newDest,
		Departure:   fptf.TimeNullable{Time: journeyArr},
		Arrival:     fptf.TimeNullable{Time: newArr},
		Stopovers: []*fptf.Stopover{{
			StopStation: journeyDest,
			Departure:   fptf.TimeNullable{Time: journeyArr},
		}, {
			StopStation: newDest,
			Arrival:     fptf.TimeNullable{Time: newArr},
		}},
		Mode: fptf.ModeWalking,
	}

	journey.Trips = append(journey.Trips, walkTrip)
}

func addTripDestination(trip *fptf.Trip, newDest *fptf.StopStation, newArr time.Time, journeyArr time.Time) {
	trip.Destination = newDest
	trip.Arrival = fptf.TimeNullable{Time: newArr}
	trip.Stopovers = append(trip.Stopovers, &fptf.Stopover{
		StopStation: newDest,
		Arrival:     fptf.TimeNullable{Time: newArr},
	})
	trip.Stopovers[len(trip.Stopovers)-2].Departure = fptf.TimeNullable{Time: journeyArr}
}
