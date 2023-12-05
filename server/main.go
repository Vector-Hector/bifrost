package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/Vector-Hector/bifrost"
	"github.com/Vector-Hector/fptf"
	"github.com/gin-gonic/gin"
	"runtime/debug"
	"strings"
	"time"
)

type JourneyRequest struct {
	Origin      *fptf.Location `json:"origin"`
	Destination *fptf.Location `json:"destination"`
	Departure   time.Time      `json:"departure"`
	OnlyWalk    bool           `json:"onlyWalk"`
}

type StringSlice []string

func (s *StringSlice) String() string {
	if s == nil {
		return ""
	}
	return strings.Join(*s, ",")
}

func (s *StringSlice) Set(value string) error {
	if s == nil {
		return nil
	}
	*s = append(*s, value)
	return nil
}

func main() {
	var osmPath StringSlice
	var gtfsPath StringSlice

	flag.Var(&osmPath, "osm", "path to an osm pbf file")
	flag.Var(&gtfsPath, "gtfs", "path to a gtfs zip file")
	bifrostPath := flag.String("bifrost", "data.bifrost", "path to bifrost cache")
	numHandlerThreads := flag.Int("threads", 12, "number of handler threads")

	flag.Parse()

	start := time.Now()

	fmt.Println("Loading raptor data")
	b := bifrost.DefaultBifrost
	err := b.LoadData(&bifrost.LoadOptions{
		OsmPaths:    osmPath,
		GtfsPaths:   gtfsPath,
		BifrostPath: *bifrostPath,
	})
	if err != nil {
		panic(err)
	}

	fmt.Println("Raptor data loaded")

	roundChan := make(chan *bifrost.Rounds, *numHandlerThreads)

	for i := 0; i < *numHandlerThreads; i++ {
		roundChan <- b.NewRounds()
	}

	fmt.Println("Startup took", time.Since(start))

	engine := gin.Default()

	engine.POST("/bifrost", func(c *gin.Context) {
		handle(c, b, roundChan)
	})

	err = engine.Run(":8090")
	if err != nil {
		panic(err)
	}
}

func handle(c *gin.Context, b *bifrost.Bifrost, roundChan chan *bifrost.Rounds) {
	rounds := <-roundChan

	defer func() {
		roundChan <- rounds

		if r := recover(); r != nil {
			fmt.Println("Recovered in f", r)

			debug.PrintStack()

			c.String(500, "Internal server error")
		}
	}()

	req := &JourneyRequest{}
	err := json.NewDecoder(c.Request.Body).Decode(req)
	if err != nil {
		panic(err)
	}

	t := time.Now()

	journey, err := b.Route(rounds, []bifrost.SourceLocation{{
		Location:  req.Origin,
		Departure: req.Departure,
	}}, req.Destination, req.OnlyWalk, false)

	fmt.Println("Routing took", time.Since(t))

	c.JSON(200, journey)
}
