package main

import (
	"fmt"
	"github.com/Vector-Hector/bifrost"
	"github.com/Vector-Hector/fptf"
	"github.com/gin-gonic/gin"
	"runtime/debug"
	"time"
)

func main() {
	fmt.Println("Loading raptor data")
	b := bifrost.DefaultBifrost
	err := b.LoadData(&bifrost.LoadOptions{
		OsmPaths: []string{"../data/mvv/oberbayern-latest.osm.pbf"},
		//OsmPaths:    []string{"../data/mvv/oberbayern.csv"},
		GtfsPaths:   []string{"../data/mvv/gtfs/"},
		BifrostPath: "../data/mvv/munich.bifrost",
	})
	if err != nil {
		panic(err)
	}

	fmt.Println("Raptor data loaded")

	numHandlerThreads := 12

	roundChan := make(chan *bifrost.Rounds, numHandlerThreads)

	for i := 0; i < numHandlerThreads; i++ {
		roundChan <- b.NewRounds()
	}

	engine := gin.Default()

	engine.GET("/", func(c *gin.Context) {
		rounds := <-roundChan

		defer func() {
			roundChan <- rounds

			if r := recover(); r != nil {
				fmt.Println("Recovered in f", r)

				debug.PrintStack()

				c.String(500, "Internal server error")
			}
		}()

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

		t := time.Now()
		_, err = b.Route(rounds, []bifrost.SourceLocation{{
			Location:  origin,
			Departure: departureTime,
		}}, dest, false, true)

		fmt.Println("Routing took", time.Since(t))

		c.String(200, "Hello world")
	})

	err = engine.Run(":8090")
	if err != nil {
		panic(err)
	}
}
