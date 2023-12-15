package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/Vector-Hector/bifrost"
	"github.com/Vector-Hector/fptf"
	"github.com/gin-gonic/gin"
	"math"
	"runtime/debug"
	"strings"
	"time"
)

type JourneyRequest struct {
	Origin      *fptf.Location `json:"origin"`
	Destination *fptf.Location `json:"destination"`
	Departure   time.Time      `json:"departure"`
	Modes       []fptf.Mode    `json:"modes"`
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
	onlyBuild := flag.Bool("only-build", false, "only build the bifrost cache")

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

	if *onlyBuild {
		return
	}

	const roundChanSize = 200

	roundChan := make(chan *bifrost.Rounds, roundChanSize)

	for i := 0; i < roundChanSize; i++ {
		roundChan <- b.NewRounds()
	}

	if *numHandlerThreads < 1 {
		*numHandlerThreads = 1
	}

	if *numHandlerThreads > roundChanSize {
		*numHandlerThreads = roundChanSize
	}

	threadChan := make(chan bool, *numHandlerThreads)

	for i := 0; i < *numHandlerThreads; i++ {
		threadChan <- true
	}

	fmt.Println("Startup took", time.Since(start))

	engine := gin.Default()

	engine.POST("/bifrost", func(c *gin.Context) {
		<-threadChan

		defer func() {
			threadChan <- true
		}()

		handle(c, b)
	})

	err = engine.Run(":8090")
	if err != nil {
		panic(err)
	}
}

func handle(c *gin.Context, b *bifrost.Bifrost) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("Recovered in f", r)

			debug.PrintStack()

			c.String(500, "Internal server error: %v", r)
		}
	}()

	req := &JourneyRequest{}
	err := json.NewDecoder(c.Request.Body).Decode(req)
	if err != nil {
		panic(err)
	}

	// validate request
	if req.Origin == nil || math.Abs(req.Origin.Longitude) < 0.0001 || math.Abs(req.Origin.Latitude) < 0.0001 {
		c.JSON(400, gin.H{
			"error": "invalid origin",
		})
		return
	}

	if req.Destination == nil || math.Abs(req.Destination.Longitude) < 0.0001 || math.Abs(req.Destination.Latitude) < 0.0001 {
		c.JSON(400, gin.H{
			"error": "invalid destination",
		})
		return
	}

	if req.Departure.IsZero() {
		c.JSON(400, gin.H{
			"error": "invalid departure",
		})
		return
	}

	if len(req.Modes) == 0 {
		c.JSON(400, gin.H{
			"error": "invalid modes",
		})
		return
	}

	t := time.Now()

	rounds := b.NewRounds()

	journey, err := b.Route(rounds, []bifrost.SourceLocation{{
		Location:  req.Origin,
		Departure: req.Departure,
	}}, req.Destination, req.Modes, false)
	if err != nil {
		panic(err)
	}

	fmt.Println("Routing took", time.Since(t))

	c.JSON(200, journey)
}
