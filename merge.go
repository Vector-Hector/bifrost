package bifrost

import (
	"fmt"
	"github.com/kyroy/kdtree"
	"sync"
	"time"
)

// ConnectStopsToVertices connects stops to street graph using knn and the Bifrost parameters.
func (b *Bifrost) ConnectStopsToVertices() {
	t := time.Now()

	verticesAsPoints := make([]kdtree.Point, len(b.Data.Vertices))
	for i, v := range b.Data.Vertices {
		verticesAsPoints[i] = &GeoPoint{
			Latitude:  v.Latitude,
			Longitude: v.Longitude,
			VertKey:   uint64(i),
		}
	}

	tree := kdtree.New(verticesAsPoints)

	fmt.Println("Building kd-tree took", time.Since(t))

	t = time.Now()

	fmt.Println("Connecting stops to street graph")

	locks := make([]sync.Mutex, len(b.Data.Vertices))

	for i, stop := range b.Data.Vertices {
		if stop.Stop == nil {
			continue // only connect stops
		}

		stopPoint := &GeoPoint{
			Latitude:  stop.Latitude,
			Longitude: stop.Longitude,
			VertKey:   uint64(i),
		}

		nearest := tree.KNN(stopPoint, 30)

		for _, point := range nearest {
			streetVert := point.(*GeoPoint)

			if !b.fastDistWithin(stopPoint, streetVert, b.MaxStopsConnectionSeconds) {
				break
			}

			dist := b.DistanceMs(&b.Data.Vertices[i], &b.Data.Vertices[streetVert.VertKey])

			if dist > b.MaxStopsConnectionSeconds {
				break
			}

			fromKey := stopPoint.VertKey
			toKey := streetVert.VertKey

			fromLock := &locks[fromKey]
			fromLock.Lock()
			b.Data.StreetGraph[fromKey] = append(b.Data.StreetGraph[fromKey], Arc{
				Target:   toKey,
				Distance: dist,
			})
			fromLock.Unlock()

			toLock := &locks[toKey]
			toLock.Lock()
			b.Data.StreetGraph[toKey] = append(b.Data.StreetGraph[toKey], Arc{
				Target:   fromKey,
				Distance: dist,
			})
			toLock.Unlock()
		}
	}

	fmt.Println("Connecting stops to street graph took", time.Since(t))
}

func (b *Bifrost) MergeData(other *BifrostData) {
	b.Data = MergeData(b.Data, other)
}

// MergeData merges two BifrostData structs. It only concatenates the vertices and edges. Use ConnectStopsToVertices
// to connect stops to the street graph. IMPORTANT: This algorithm may change and re-use the data from both structs.
// Also note, that using multiple transit feeds may break things like the stops index due to duplicate stop ids.
// Multiple street graphs are not supported as there is no way of connecting them.
// todo: fix stops index for multiple transit feeds
// todo: add support for multiple street graphs
func MergeData(a *BifrostData, b *BifrostData) *BifrostData {
	if a == nil {
		return b
	}

	if b == nil {
		return a
	}

	a.EnsureSliceLengths()
	b.EnsureSliceLengths()

	maxTripDayLength := a.MaxTripDayLength
	if b.MaxTripDayLength > maxTripDayLength {
		maxTripDayLength = b.MaxTripDayLength
	}

	bVertexOffset := uint64(len(a.Vertices))
	bTripOffset := uint32(len(a.Trips))
	bRouteOffset := uint32(len(a.Routes))
	bServiceOffset := uint32(len(a.Services))
	bGtfsRouteOffset := uint32(len(a.RouteInformation))

	return &BifrostData{
		MaxTripDayLength: maxTripDayLength,
		Services:         append(a.Services, b.Services...),
		Routes:           mergeRoutes(a.Routes, b.Routes, bVertexOffset, bTripOffset),
		StopToRoutes:     mergeStopToRoutes(a.StopToRoutes, b.StopToRoutes, bRouteOffset),
		Trips:            mergeTrips(a.Trips, b.Trips, bServiceOffset),
		StreetGraph:      mergeStreetGraph(a.StreetGraph, b.StreetGraph, bVertexOffset),
		Reorders:         mergeReorders(a.Reorders, b.Reorders, bRouteOffset),
		Vertices:         append(a.Vertices, b.Vertices...),
		StopsIndex:       mergeStopsIndex(a.StopsIndex, b.StopsIndex, bVertexOffset),
		NodesIndex:       mergeNodesIndex(a.NodesIndex, b.NodesIndex, bVertexOffset),
		GtfsRouteIndex:   mergeGtfsRouteIndex(a.GtfsRouteIndex, b.GtfsRouteIndex, bGtfsRouteOffset),
		RouteInformation: append(a.RouteInformation, b.RouteInformation...),
		TripInformation:  append(a.TripInformation, b.TripInformation...),
		TripToRoute:      mergeTripToRoute(a.TripToRoute, b.TripToRoute, bRouteOffset),
	}
}

func (r *BifrostData) EnsureSliceLengths() {
	vertexCount := len(r.Vertices)
	if len(r.StopToRoutes) == 0 {
		r.StopToRoutes = make([][]StopRoutePair, vertexCount)
	}
	if len(r.StopToRoutes) != vertexCount {
		panic(fmt.Sprintf("stop to routes length mismatch: %d != %d", len(r.StopToRoutes), vertexCount))
	}

	if len(r.StreetGraph) == 0 {
		r.StreetGraph = make([][]Arc, vertexCount)
	}
	if len(r.StreetGraph) != vertexCount {
		panic(fmt.Sprintf("street graph length mismatch: %d != %d", len(r.StreetGraph), vertexCount))
	}

	routeCount := len(r.Routes)
	if len(r.GtfsRouteIndex) == 0 {
		r.GtfsRouteIndex = make([]uint32, routeCount)
	}
	if len(r.GtfsRouteIndex) != routeCount {
		panic(fmt.Sprintf("gtfs route index length mismatch: %d != %d", len(r.GtfsRouteIndex), routeCount))
	}

	tripCount := len(r.Trips)
	if len(r.TripToRoute) == 0 {
		r.TripToRoute = make([]uint32, tripCount)
	}
	if len(r.TripToRoute) != tripCount {
		panic(fmt.Sprintf("trip to route length mismatch: %d != %d", len(r.TripToRoute), tripCount))
	}
}

func mergeRoutes(a []*Route, b []*Route, bVertexOffset uint64, bTripOffset uint32) []*Route {
	if len(a) == 0 {
		return b
	}

	if len(b) == 0 {
		return a
	}

	// shift all trips and routes in b
	for _, route := range b {
		for j, stop := range route.Stops {
			route.Stops[j] = stop + bVertexOffset
		}
		for j, trip := range route.Trips {
			route.Trips[j] = trip + bTripOffset
		}
	}

	routes := make([]*Route, len(a)+len(b))
	copy(routes, a)
	copy(routes[len(a):], b)

	return routes
}

func mergeStopToRoutes(a [][]StopRoutePair, b [][]StopRoutePair, bRouteOffset uint32) [][]StopRoutePair {
	if len(a) == 0 {
		return b
	}

	if len(b) == 0 {
		return a
	}

	// shift all trips and routes in b
	for _, stopToRoutes := range b {
		for _, stopRoutePair := range stopToRoutes {
			stopRoutePair.Route += bRouteOffset
			// StopKeyInRoute not shifted
		}
	}

	stopToRoutes := make([][]StopRoutePair, len(a)+len(b))
	copy(stopToRoutes, a)
	copy(stopToRoutes[len(a):], b)

	return stopToRoutes
}

func mergeTrips(a []*Trip, b []*Trip, bServiceOffset uint32) []*Trip {
	if len(a) == 0 {
		return b
	}

	if len(b) == 0 {
		return a
	}

	// shift all services in b
	for _, trip := range b {
		trip.Service += bServiceOffset
		// stopovers not shifted
	}

	trips := make([]*Trip, len(a)+len(b))
	copy(trips, a)
	copy(trips[len(a):], b)

	return trips
}

func mergeStreetGraph(a [][]Arc, b [][]Arc, bVertexOffset uint64) [][]Arc {
	if len(a) == 0 {
		return b
	}

	if len(b) == 0 {
		return a
	}

	// shift all vertices in b
	for _, arcs := range b {
		for _, arc := range arcs {
			arc.Target += bVertexOffset
		}
	}

	streetGraph := make([][]Arc, len(a)+len(b))
	copy(streetGraph, a)
	copy(streetGraph[len(a):], b)

	return streetGraph
}

func mergeReorders(a, b map[uint64][]uint32, bRouteOffset uint32) map[uint64][]uint32 {
	if len(a) == 0 {
		return b
	}

	if len(b) == 0 {
		return a
	}

	for k, v := range b {
		// k= uint64(routeKey)<<32 | uint64(stopSeqKey)

		routeKey := uint32(k >> 32)
		stopSeqKey := uint32(k) & 0xffffffff

		routeKey += bRouteOffset

		k = uint64(routeKey)<<32 | uint64(stopSeqKey)

		a[k] = v // write to a, so a contains the merged reorders
	}

	return a
}

func mergeStopsIndex(a, b map[string]uint64, bVertexOffset uint64) map[string]uint64 {
	if len(a) == 0 {
		return b
	}

	if len(b) == 0 {
		return a
	}

	for k, v := range b {
		a[k] = v + bVertexOffset
	}

	return a
}

func mergeNodesIndex(a, b map[int64]uint64, bVertexOffset uint64) map[int64]uint64 {
	if len(a) == 0 {
		return b
	}

	if len(b) == 0 {
		return a
	}

	for k, v := range b {
		a[k] = v + bVertexOffset
	}

	return a
}

func mergeGtfsRouteIndex(a, b []uint32, bGtfsRouteOffset uint32) []uint32 {
	if len(a) == 0 {
		return b
	}

	if len(b) == 0 {
		return a
	}

	for i, v := range b {
		a[i] = v + bGtfsRouteOffset
	}

	return a
}

func mergeTripToRoute(a, b []uint32, bRouteOffset uint32) []uint32 {
	if len(a) == 0 {
		return b
	}

	if len(b) == 0 {
		return a
	}

	for i, v := range b {
		a[i] = v + bRouteOffset
	}

	return a
}
