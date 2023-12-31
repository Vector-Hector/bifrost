package bifrost

import (
	"github.com/Vector-Hector/fptf"
	"testing"
	"time"
)

var b *Bifrost
var r *Rounds

func init() {
	b = DefaultBifrost
	err := b.LoadData(&LoadOptions{
		OsmPaths:    []string{"data/mvv/oberbayern-latest.osm.pbf"},
		GtfsPaths:   []string{"data/mvv/gtfs/"},
		BifrostPath: "data/mvv/munich.bifrost",
	})
	if err != nil {
		panic(err)
	}
	r = b.NewRounds()
}

func TestRaptor(t *testing.T) {
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

	_, err = b.Route(r, []SourceLocation{{
		Location:  origin,
		Departure: departureTime,
	}}, dest, ModeTransit, true)
	if err != nil {
		panic(err)
	}
}
