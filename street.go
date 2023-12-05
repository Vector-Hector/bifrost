package bifrost

import (
	"fmt"
	"github.com/Vector-Hector/bifrost/stream"
	"strconv"
	"strings"
	"sync"
	"time"
)

// AddStreet reads the street data from StreetPath and adds it to the BifrostData or creates a new one containing
// only the street data. Obtain the csv street files from osm2ch.
func (b *Bifrost) AddStreet(filePath string) error {
	filePath = strings.TrimSuffix(filePath, ".csv")

	edgeFilePath := filePath + ".csv"
	vertFilePath := filePath + "_vertices.csv"

	t := time.Now()

	r := b.Data
	if r == nil {
		r = &BifrostData{
			Vertices: make([]Vertex, 0),
		}
	}

	vertexIndex := uint64(len(r.Vertices))

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

		dist := b.DistanceMs(&r.Vertices[from], &r.Vertices[to])

		if dist > b.MaxWalkingMs {
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

	fmt.Println("Reading files took", time.Since(t))

	return nil
}
