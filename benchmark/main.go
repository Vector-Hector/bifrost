package main

import (
	"fmt"
	"github.com/Vector-Hector/fptf"
	"math/rand"
	"sync/atomic"
	"time"
)

type Bounds struct {
	MinLat float64
	MinLon float64
	MaxLat float64
	MaxLon float64
}

type Router func(*fptf.Location, *fptf.Location, time.Time, bool) (*fptf.Journey, time.Duration, error)

func (b *Bounds) GenerateRandomLocation() *fptf.Location {
	lat := b.MinLat + (b.MaxLat-b.MinLat)*rand.Float64()
	lon := b.MinLon + (b.MaxLon-b.MinLon)*rand.Float64()

	return &fptf.Location{
		Longitude: lon,
		Latitude:  lat,
	}
}

func main() {
	bounds := &Bounds{
		MinLat: 48.1910386,
		MinLon: 11.5022064,
		MaxLat: 48.0939415,
		MaxLon: 11.6560394,
	}

	samples := 100

	toBenchmark := map[string]Router{
		"bifrost": RouteBifrost,
		"otp":     RouteOpenTripPlanner,
	}

	rand.Seed(time.Now().UnixNano())

	departureTime, err := time.Parse(time.RFC3339, "2023-12-12T08:30:00Z")
	if err != nil {
		panic(err)
	}

	tz := time.FixedZone("Europe/Berlin", 3600)
	departureTime = departureTime.In(tz)

	threads := 12

	benchmarkMultiThreaded(bounds, departureTime, samples, threads, toBenchmark)
}

func benchmarkMultiThreaded(bounds *Bounds, departureTime time.Time, samples int, threads int, toBenchmark map[string]Router) {
	for name, router := range toBenchmark {
		fmt.Println("Benchmarking", name)
		individualAvg, globalAvg := benchmarkFunction(bounds, departureTime, samples, threads, router)
		fmt.Println("Individual average time:", individualAvg)
		fmt.Println("Global average time:", globalAvg)
	}
}

func benchmarkFunction(bounds *Bounds, departureTime time.Time, samples int, threads int, toBenchmark Router) (time.Duration, time.Duration) {
	start := time.Now()

	counter := int32(0)
	totalNs := int64(0)

	done := make(chan bool, threads)

	for i := 0; i < threads; i++ {
		go func() {
			for {
				origin := bounds.GenerateRandomLocation()
				dest := bounds.GenerateRandomLocation()

				_, dur, err := toBenchmark(origin, dest, departureTime, false)
				if err != nil {
					panic(err)
				}

				ms := dur.Nanoseconds()
				atomic.AddInt64(&totalNs, ms)

				result := atomic.AddInt32(&counter, 1)
				if result >= int32(samples) {
					break
				}
			}

			done <- true
		}()
	}

	for i := 0; i < threads; i++ {
		<-done
	}

	globalAverage := time.Since(start) / time.Duration(counter)

	return time.Duration(totalNs / int64(counter)), globalAverage
}
