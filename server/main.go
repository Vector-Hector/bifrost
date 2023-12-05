package main

import (
	"fmt"
	"github.com/Vector-Hector/bifrost"
	"github.com/gin-gonic/gin"
	"runtime/debug"
	"time"
)

func main() {
	fmt.Println("Loading raptor data")
	b := bifrost.DefaultBifrost
	err := b.LoadData(&bifrost.LoadOptions{
		StreetPaths: []string{"../data/mvv/oberbayern.csv"},
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

		originID := "de:09162:6"
		destID := "de:09162:2"

		originKey := b.Data.StopsIndex[originID]
		destKey := b.Data.StopsIndex[destID]

		departureTime, err := time.Parse(time.RFC3339, "2023-12-12T08:30:00Z")
		if err != nil {
			panic(err)
		}

		t := time.Now()
		b.Route(rounds, []bifrost.Source{{
			StopKey:   originKey,
			Departure: departureTime,
		}}, destKey, true)

		fmt.Println("Routing took", time.Since(t))

		c.String(200, "Hello world")
	})

	err = engine.Run(":8090")
	if err != nil {
		panic(err)
	}
}
