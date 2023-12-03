package main

import (
	"fmt"
	fptf "github.com/Vector-Hector/friendly-public-transport-format"
	util "github.com/Vector-Hector/goutil"
	"strconv"
	"time"
)

func (r *RaptorData) ReconstructJourney(destKey uint64, lastRound int, rounds *Rounds) *fptf.Journey {
	// reconstruct path
	trips := make([]*fptf.Trip, 0)
	position := destKey

	for i := lastRound; i > 0; i-- {
		fmt.Println("reconstructing round", i, "at position", position)
		arr := rounds.Rounds[i][position]

		if arr.Trip == TripIdNoChange {
			continue
		}

		if arr.Trip == TripIdTransfer {
			fmt.Println("round", i, "is a transfer")
			trip, newPos := GetTripFromTransfer(r, rounds, rounds.Rounds[i], position)
			position = newPos
			trips = append(trips, trip)
			continue
		}

		fmt.Println("round", i, "is a trip")
		trip, newPos := GetTripFromTrip(r, rounds, rounds.Rounds[i-1], arr)
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

func GetTripFromTransfer(r *RaptorData, rounds *Rounds, round []StopArrival, destination uint64) (*fptf.Trip, uint64) {
	position := destination
	arrival := round[position]
	path := make([]uint64, 1)
	path[0] = position

	for {
		if arrival.Trip != TripIdTransfer {
			break
		}

		if !rounds.Exists(&arrival) {
			panic("transfer trip does not exist")
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
		stopover := &fptf.Stopover{
			StopStation: r.GetFptfStop(stop),
		}
		if i != len(path)-1 {
			stopover.Arrival = r.GetTime(arrival.Arrival)
		}
		if i != 0 {
			stopover.Departure = r.GetTime(arrival.Arrival)
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
		Mode:        fptf.ModeWalking,
	}

	return trip, position
}

func (r *RaptorData) GetFptfStop(stop uint64) *fptf.StopStation {
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

func (r *RaptorData) GetTime(ms uint64) fptf.TimeNullable {
	return fptf.TimeNullable{
		Time: time.Unix(int64(ms/1000), int64(ms%1000)*1000000),
	}
}

func GetTripFromTrip(r *RaptorData, rounds *Rounds, round []StopArrival, arrival StopArrival) (*fptf.Trip, uint64) {
	trip := r.Trips[arrival.Trip]
	routeKey := r.TripToRoute[arrival.Trip]
	route := r.Routes[routeKey]

	enterKey := 0

	for i := int(arrival.EnterKey) - 1; i >= 0; i-- {
		stop := route.Stops[i]
		if !rounds.Exists(&round[stop]) {
			continue
		}

		enterKey = i
		break
	}

	if enterKey == 0 && !rounds.Exists(&round[route.Stops[0]]) {
		util.PrintJSON(arrival)
		panic(fmt.Sprint("no enter key found for trip ", arrival.Trip, " at route ", routeKey))
	}

	gtfsRouteKey := r.RaptorToGtfsRoutes[routeKey]
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
			Mode: fptf.ModeTrain,
			Name: routeName,
		},
		Mode:      fptf.ModeTrain,
		Direction: gtfsTrip.Headsign,
	}

	return result, route.Stops[enterKey]
}
