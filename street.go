package main

import (
	"fmt"
	"github.com/kyroy/kdtree"
	"raptor/stream"
	"strconv"
	"strings"
	"sync"
	"time"
)

func ReadStreetData(r *RaptorData, filePath string) error {
	// add street graph to raptor dataset

	edgeFilePath := filePath + ".csv"
	vertFilePath := filePath + "_vertices.csv"
	shortcutsFilePath := filePath + "_shortcuts.csv"

	t := time.Now()

	vertexIndex := uint64(len(r.Vertices))
	newVerticesStart := vertexIndex

	vertexLock := sync.Mutex{}

	threadCount := 12

	r.NodesIndex = make(map[int64]uint64)

	err := stream.IterateVertices(vertFilePath, threadCount, func(vertex stream.StringArr) {
		geom := vertex.GetString(stream.VertexGeom)

		geom = strings.TrimPrefix(geom, "POINT(")
		geom = strings.TrimPrefix(geom, "POINT (")
		geom = strings.TrimSuffix(geom, ")")

		parts := strings.Split(geom, " ")

		lon, err := strconv.ParseFloat(parts[0], 64)
		if err != nil {
			panic(err)
		}

		lat, err := strconv.ParseFloat(parts[1], 64)
		if err != nil {
			panic(err)
		}

		vert := Vertex{
			Latitude:  lat,
			Longitude: lon,
		}

		vertexLock.Lock()
		r.Vertices = append(r.Vertices, vert)
		r.NodesIndex[vertex.GetInt(stream.VertexVertexID)] = vertexIndex
		r.StopToRoutes = append(r.StopToRoutes, nil)
		vertexIndex++
		vertexLock.Unlock()
	})
	if err != nil {
		return err
	}

	fmt.Println("Vertices:", len(r.Vertices))
	fmt.Println("last vertex index:", vertexIndex)
	fmt.Println("nodes index:", len(r.NodesIndex))

	r.StreetGraph = make([][]Arc, vertexIndex)

	vertexCount := len(r.Vertices)

	locks := make([]sync.Mutex, vertexCount)

	err = stream.IterateEdges(edgeFilePath, threadCount, func(edge stream.StringArr) {
		from := r.NodesIndex[edge.GetInt(stream.EdgeFromVertexID)]
		to := r.NodesIndex[edge.GetInt(stream.EdgeToVertexID)]

		dist := DistanceMs(&r.Vertices[from], &r.Vertices[to])

		if dist > MaxWalkingMs {
			return
		}

		lock := &locks[from]
		lock.Lock()
		r.StreetGraph[from] = append(r.StreetGraph[from], Arc{
			Target:   to,
			Distance: dist,
		})
		lock.Unlock()
	})
	if err != nil {
		return err
	}

	shortcuts := make([][]Shortcut, vertexCount)

	err = stream.IterateShortcuts(shortcutsFilePath, threadCount, func(shortcut stream.StringArr) {
		from := r.NodesIndex[shortcut.GetInt(stream.ShortcutFromVertexID)]
		to := r.NodesIndex[shortcut.GetInt(stream.ShortcutToVertexID)]
		via := r.NodesIndex[shortcut.GetInt(stream.ShortcutViaVertexID)]

		dist := DistanceMs(&r.Vertices[from], &r.Vertices[to])

		if dist > MaxWalkingMs {
			return
		}

		lock := &locks[from]
		lock.Lock()
		r.StreetGraph[from] = append(r.StreetGraph[from], Arc{
			Target:   to,
			Distance: dist,
		})
		shortcuts[from] = append(shortcuts[from], Shortcut{
			Target: to,
			Via:    via,
		})
		lock.Unlock()
	})
	if err != nil {
		return err
	}

	fmt.Println("Reading files took", time.Since(t))

	// connect stops to street graph using knn

	verticesAsPoints := make([]kdtree.Point, len(r.Vertices))
	for i, v := range r.Vertices {
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

	for i := 0; i < int(newVerticesStart); i++ {
		stop := r.Vertices[i]
		stopPoint := &GeoPoint{
			Latitude:  stop.Latitude,
			Longitude: stop.Longitude,
			VertKey:   uint64(i),
		}

		nearest := tree.KNN(stopPoint, 10)

		for _, point := range nearest {
			streetVert := point.(*GeoPoint)

			if !fastDistWithin(stopPoint, streetVert, MaxStopsConnectionSeconds) {
				break
			}

			dist := DistanceMs(&r.Vertices[i], &r.Vertices[streetVert.VertKey])

			if dist > MaxStopsConnectionSeconds {
				break
			}

			fromKey := stopPoint.VertKey
			toKey := streetVert.VertKey

			fromLock := &locks[fromKey]
			fromLock.Lock()
			r.StreetGraph[fromKey] = append(r.StreetGraph[fromKey], Arc{
				Target:   toKey,
				Distance: dist,
			})
			fromLock.Unlock()

			toLock := &locks[toKey]
			toLock.Lock()
			r.StreetGraph[toKey] = append(r.StreetGraph[toKey], Arc{
				Target:   fromKey,
				Distance: dist,
			})
			toLock.Unlock()
		}
	}

	fmt.Println("Connecting stops to street graph took", time.Since(t))

	return nil
}
