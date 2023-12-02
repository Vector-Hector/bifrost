package main

import (
	"testing"
)

var R *RaptorData
var RaptorRounds *Rounds

func init() {
	R = LoadRaptorDataset()
	RaptorRounds = NewRounds(len(R.Stops))
}

func TestRaptor(t *testing.T) {
	originID := "476628" // münchen hbf
	destID := "170058"   // marienplatz
	//destID := "193261" // berlin hbf

	for i := 0; i < 100; i++ {
		runRaptor(R, RaptorRounds, R.StopsIndex[originID], R.StopsIndex[destID], false)
	}
}
