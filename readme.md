# Bifrost

A lightweight, blazing fast, multi-modal routing engine in go. It can route on public transport and streets. Its
features are still
limited compared to other routing engines, but it is already quite fast and easy to use.

## Benchmark

We compare our routing engine to OpenTripPlanner due to its similarity in that they are both multi-modal routing
engines. It is not entirely fair, because our current implementation is more simple. See [here](benchmark/benchmark.md)
for more details on the benchmark methodology and definitions. Here are the results on multi-modal routing in Munich:

| Engine  | Global Average Execution Time | Local Average Execution Time | Memory Usage |
|---------|-------------------------------|------------------------------|--------------|
| Bifrost | 36.1ms                        | 422.4ms                      | 800 MB       |
| OTP     | 85.8ms                        | 996.8ms                      | 4026 MB      |

## Usage

You can use it either as a library or as a command line tool. The cli will start a server that you can query with
http requests.
You will need to prepare a GTFS and an OSM file. We use the [golang binding](https://github.com/Vector-Hector/fptf) of
the [friendly public transport format](https://github.com/public-transport/friendly-public-transport-format/blob/1.2.1/spec/readme.md)
for journey input and output.

Note, that internally, one of the libraries uses CGO for faster parsing. This can be turned off by setting the
environment variable `CGO_ENABLED=0` before building.

### CLI Usage

Please prepare at least one GTFS and one OSM file. After that, run:

```bash
go run server/main.go -gtfs data/mvv/gtfs/ -osm data/mvv/osm/oberbayern-latest.osm.pbf -bifrost data/mvv/munich.bifrost
```

This will start a server on port 8090. You can query it with http requests. See [here](server/api.json) for the api
specification.

### Library Usage

```bash
go get github.com/Vector-Hector/bifrost
```

Example script:

```go
package main

import (
	"fmt"
	"github.com/Vector-Hector/bifrost"
	"github.com/Vector-Hector/fptf"
	"time"
)

func main() {
	b := bifrost.DefaultBifrost // Create a new router with default parameters
	err := b.LoadData(&bifrost.LoadOptions{
		OsmPaths:    []string{"oberbayern-latest.osm.pbf"},
		GtfsPaths:   []string{"gtfs/"},
		BifrostPath: "munich.bifrost",
	}) // Load cached data or create and cache routing data from source
	if err != nil {
		panic(err)
	}

	r := b.NewRounds() // Reusable rounds object for routing

	// define origin, destination and time
	origin := &fptf.Location{
		Name:      "München Hbf",
		Longitude: 11.5596949,
		Latitude:  48.140262,
	}

	dest := &fptf.Location{
		Name:      "Marienplatz",
		Longitude: 11.5757167,
		Latitude:  48.1378071,
	}

	departureTime, err := time.Parse(time.RFC3339, "2023-12-12T08:30:00Z")
	if err != nil {
		panic(err)
	}

	journey, err := b.Route(r, []bifrost.SourceLocation{{
		Location:  origin,
		Departure: departureTime,
	}}, dest, false, false)
	if err != nil {
		panic(err)
	}

	fmt.Println("Journey from", journey.GetOrigin().GetName(), "to", journey.GetDestination().GetName(), "departs at", journey.GetDeparture(), "and arrives at", journey.GetArrival())
}
```

## How it works internally

The routing algorithm is based on dijkstra and the RAPTOR algorithm. It switches each round between public transport
and street routing to find the best multi-modal path.

## References

Thanks to all the people who wrote the following articles, algorithms and libraries:

- [OpenTripPlanner](https://github.com/opentripplanner/OpenTripPlanner): Great routing engine that inspired us to write
  this. It has much more features, but also needs much more resources.
- [Raptor Agorithm Paper](https://www.microsoft.com/en-us/research/wp-content/uploads/2012/01/raptor_alenex.pdf): The
  paper that describes the RAPTOR algorithm
- [Simple version of RAPTOR in python](https://kuanbutts.com/2020/09/12/raptor-simple-example/): Helped us understand
  the algorithm and implement it
- [Dijkstra](https://en.wikipedia.org/wiki/Dijkstra%27s_algorithm): For street routing
- [GTFS](https://developers.google.com/transit/gtfs/reference): For public transport data
- [OSM](https://www.openstreetmap.org/): For street data
- [osm2ch](https://github.com/LdDl/osm2ch): For converting OSM to a street graph
- [kdtree](https://github.com/kyroy/kdtree): For efficient nearest neighbour search
- [fptf](https://github.com/public-transport/friendly-public-transport-format/blob/1.2.1/spec/readme.md): For input and output of the routing API
