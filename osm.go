package bifrost

import (
	"fmt"
	"github.com/LdDl/osm2ch"
	"strings"
	"time"
)

var tagStr = "motorway,primary,primary_link,road,secondary,secondary_link,residential,tertiary,tertiary_link,unclassified,trunk,trunk_link,motorway_link"
var osmCfg = &osm2ch.OsmConfiguration{
	EntityName: "highway", // Currrently we do not support others
	Tags:       strings.Split(tagStr, ","),
}

func (b *Bifrost) AddOSM(path string) error {
	t := time.Now()

	fmt.Println("Reading OSM data from", path)

	edges, err := osm2ch.ImportFromOSMFile(path, osmCfg)
	if err != nil {
		return err
	}

	fmt.Println("Found", len(edges), "edges")

	if b.Data == nil {
		b.Data = &RoutingData{}
	}

	lastVertex := uint64(len(b.Data.Vertices))

	fmt.Println("Converting edges to bifrost street graph format")

	if b.Data.NodesIndex == nil {
		b.Data.NodesIndex = make(map[int64]uint64)
	}

	if b.Data.StreetGraph == nil {
		b.Data.StreetGraph = make([][]Arc, len(b.Data.Vertices))
	}

	prog := Progress{}
	prog.Reset(uint64(len(edges)))

	for _, edge := range edges {
		prog.Increment()
		prog.Print()

		sourceVertKey, ok := b.Data.NodesIndex[int64(edge.Source)]
		if !ok {
			b.Data.NodesIndex[int64(edge.Source)] = lastVertex
			b.Data.Vertices = append(b.Data.Vertices, Vertex{
				Latitude:  edge.Geom[0].Lat,
				Longitude: edge.Geom[0].Lon,
			})
			b.Data.StreetGraph = append(b.Data.StreetGraph, make([]Arc, 0))
			b.Data.StopToRoutes = append(b.Data.StopToRoutes, nil)
			sourceVertKey = lastVertex
			lastVertex++
		}

		targetVertKey, ok := b.Data.NodesIndex[int64(edge.Target)]
		if !ok {
			b.Data.NodesIndex[int64(edge.Target)] = lastVertex
			b.Data.Vertices = append(b.Data.Vertices, Vertex{
				Latitude:  edge.Geom[len(edge.Geom)-1].Lat,
				Longitude: edge.Geom[len(edge.Geom)-1].Lon,
			})
			b.Data.StreetGraph = append(b.Data.StreetGraph, make([]Arc, 0))
			b.Data.StopToRoutes = append(b.Data.StopToRoutes, nil)
			targetVertKey = lastVertex
			lastVertex++
		}

		dist := uint32(edge.CostMeters / b.WalkingSpeed)
		if dist == 0 {
			dist = 1
		}

		if dist > b.MaxWalkingMs {
			continue
		}

		b.Data.StreetGraph[sourceVertKey] = append(b.Data.StreetGraph[sourceVertKey], Arc{
			Target:   targetVertKey,
			Distance: dist,
		})
	}

	b.Data.RebuildVertexTree()

	fmt.Println("Done reading OSM data.")
	fmt.Println("Reading OSM data took", time.Since(t))

	return nil
}
