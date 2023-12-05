package bifrost

import (
	"encoding/json"
	"fmt"
	"github.com/klauspost/compress/zstd"
	"os"
	"time"
)

type LoadOptions struct {
	StreetPaths []string // paths to street CSV files
	GtfsPaths   []string // path to GTFS zip files
	BifrostPath string   // path to bifrost cache
}

// LoadData loads the data from a given bifrost cache if it exists. Otherwise it will generate the data from given GTFS
// and street CSV files. After generating the data it will write the data to a bifrost cache.
func (b *Bifrost) LoadData(load *LoadOptions) error {
	cacheExists := true

	_, err := os.Stat(load.BifrostPath)
	if os.IsNotExist(err) {
		cacheExists = false
	} else if err != nil {
		return fmt.Errorf("error checking for bifrost cache: %w", err)
	}

	if cacheExists {
		b.AddBifrostData(load.BifrostPath)
		b.Data.RebuildVertexTree()
		return nil
	}

	t := time.Now()

	fmt.Println("reading gtfs data")

	for _, gtfsPath := range load.GtfsPaths {
		fmt.Println("reading gtfs data from", gtfsPath)
		localT := time.Now()
		err = b.AddGtfs(gtfsPath)
		if err != nil {
			return fmt.Errorf("error reading gtfs data: %w", err)
		}

		fmt.Println("reading gtfs data from", gtfsPath, "took", time.Since(localT))
	}

	fmt.Println("reading gtfs data took", time.Since(t))

	for _, streetPath := range load.StreetPaths {
		fmt.Println("reading street data from", streetPath)

		localT := time.Now()
		err = b.AddStreet(streetPath)
		if err != nil {
			return fmt.Errorf("error reading street data: %w", err)
		}

		fmt.Println("reading street data from", streetPath, "took", time.Since(localT))
	}

	fmt.Println("reading all files", time.Since(t))
	t = time.Now()

	b.ConnectStopsToVertices()

	fmt.Println("connecting stops to vertices took", time.Since(t))

	fmt.Println("writing to bifrost cache")
	t = time.Now()

	b.WriteBifrostData(load.BifrostPath)

	fmt.Println("writing raptor data took", time.Since(t))

	return nil
}

// AddBifrostData Adds cached bifrost data file to the Bifrost data. Used by LoadOptions, generated by WriteBifrostData
func (b *Bifrost) AddBifrostData(fileName string) {
	f, err := os.Open(fileName)
	if err != nil {
		panic(err)
	}
	defer f.Close()

	read, err := zstd.NewReader(f)
	if err != nil {
		panic(err)
	}
	defer read.Close()

	r := &RoutingData{}

	decoder := json.NewDecoder(read)
	err = decoder.Decode(r)
	if err != nil {
		panic(err)
	}

	b.Data = MergeData(b.Data, r)
}

func (b *Bifrost) WriteBifrostData(fileName string) {
	f, err := os.Create(fileName)
	if err != nil {
		panic(err)
	}
	defer f.Close()

	write, err := zstd.NewWriter(f)
	if err != nil {
		panic(err)
	}
	defer write.Flush()
	defer write.Close()

	encoder := json.NewEncoder(write)
	err = encoder.Encode(b.Data)
	if err != nil {
		panic(err)
	}
}