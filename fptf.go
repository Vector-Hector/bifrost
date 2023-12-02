package main

import (
	fptf "github.com/Vector-Hector/friendly-public-transport-format"
	"strconv"
	"time"
)

func (r *RaptorData) ReconstructJourney(destKey uint32, lastRound int, rounds [][]StopArrival) *fptf.Journey {
	// reconstruct path
	reverseTrips := make([]*fptf.Trip, 0)
	position := destKey

	for i := lastRound; i > 0; i-- {
		arr := rounds[i][position]

		if arr.Trip == TripIdNoChange {
			continue
		}

		if arr.Trip == TripIdTransfer {
			trip, newPos := GetTripFromTransfer(r, position, arr)
			position = newPos
			reverseTrips = append(reverseTrips, trip)
			continue
		}

		trip, newPos := GetTripFromTrip(r, rounds[i-1], arr)
		position = newPos
		reverseTrips = append(reverseTrips, trip)
	}

	trips := make([]*fptf.Trip, len(reverseTrips))
	for i, trip := range reverseTrips {
		trips[len(trips)-1-i] = trip
	}

	return &fptf.Journey{
		Trips: trips,
	}
}

func GetTripFromTransfer(r *RaptorData, destination uint32, arrival StopArrival) (*fptf.Trip, uint32) {
	origin := arrival.EnterStopOrKey

	originStop := r.GetFptfStop(origin)
	destStop := r.GetFptfStop(destination)
	dep := r.GetTime(arrival.DepartureOrRoute)
	arr := r.GetTime(arrival.Arrival)

	trip := &fptf.Trip{
		Origin:      originStop,
		Destination: destStop,
		Departure:   dep,
		Arrival:     arr,
		Stopovers: []*fptf.Stopover{
			{
				StopStation: originStop,
				Departure:   dep,
			},
			{
				StopStation: destStop,
				Arrival:     arr,
			},
		},
		Mode: fptf.ModeWalking,
	}

	return trip, origin
}

func (r *RaptorData) GetFptfStop(stop uint32) *fptf.StopStation {
	return &fptf.StopStation{
		Station: &fptf.Station{
			Id:   r.Stops[stop].Id,
			Name: r.Stops[stop].Name + " / " + strconv.Itoa(int(stop)),
			Location: &fptf.Location{
				Latitude:  r.Stops[stop].Latitude,
				Longitude: r.Stops[stop].Longitude,
			},
		},
	}
}

func (r *RaptorData) GetTime(seconds uint32) fptf.TimeNullable {
	t := PivotDate.Add(time.Duration(seconds) * time.Second)
	return fptf.TimeNullable{Time: t}
}

func GetTripFromTrip(r *RaptorData, round []StopArrival, arrival StopArrival) (*fptf.Trip, uint32) {
	trip := r.Trips[arrival.Trip]
	route := r.Routes[arrival.DepartureOrRoute]

	enterKey := 0

	for i := int(arrival.EnterStopOrKey) - 1; i >= 0; i-- {
		stop := route.Stops[i]
		if !round[stop].Exists {
			continue
		}

		enterKey = i
		break
	}

	if enterKey == 0 && !round[route.Stops[0]].Exists {
		panic("no enter key found")
	}

	gtfsRouteKey := r.RaptorToGtfsRoutes[arrival.DepartureOrRoute]
	gtfsRoute := r.RouteInformation[gtfsRouteKey]
	gtfsTrip := r.TripInformation[arrival.Trip]

	routeName := gtfsRoute.ShortName

	originStop := r.GetFptfStop(route.Stops[enterKey])
	destStop := r.GetFptfStop(route.Stops[arrival.EnterStopOrKey])

	dep := r.GetTime(arrival.DepartureDay*DayInSeconds + trip.StopTimes[enterKey].Departure)
	arr := r.GetTime(arrival.DepartureDay*DayInSeconds + trip.StopTimes[arrival.EnterStopOrKey].Arrival)

	stopovers := make([]*fptf.Stopover, 0, int(arrival.EnterStopOrKey)-enterKey+1)
	for i := enterKey; i <= int(arrival.EnterStopOrKey); i++ {
		stop := route.Stops[i]
		stopover := &fptf.Stopover{
			StopStation: r.GetFptfStop(stop),
			Arrival:     r.GetTime(trip.StopTimes[i].Arrival),
			Departure:   r.GetTime(trip.StopTimes[i].Departure),
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
			Mode: fptf.ModeTrain,
			Name: routeName,
		},
		Mode:      fptf.ModeTrain,
		Direction: gtfsTrip.Headsign,
	}

	return result, route.Stops[enterKey]
}
