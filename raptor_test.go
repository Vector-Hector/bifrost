package bifrost

import (
	"testing"
	"time"
)

var b *Bifrost
var r *Rounds

func init() {
	b = DefaultBifrost
	err := b.LoadData(&LoadOptions{
		StreetPaths: []string{"../data/mvv/oberbayern.csv"},
		GtfsPaths:   []string{"../data/mvv/gtfs/"},
		BifrostPath: "../data/mvv/munich.bifrost",
	})
	if err != nil {
		panic(err)
	}
	r = b.NewRounds()
}

func TestRaptor(t *testing.T) {
	originID := "476628" // münchen hbf
	destID := "170058"   // marienplatz
	//destID := "193261" // berlin hbf

	departureTime, err := time.Parse(time.RFC3339, "2023-12-12T08:30:00Z")
	if err != nil {
		panic(err)
	}

	for i := 0; i < 100; i++ {
		b.Route(r, []Source{{
			StopKey:   b.Data.StopsIndex[originID],
			Departure: departureTime,
		}}, b.Data.StopsIndex[destID], false)
	}
}
