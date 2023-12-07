package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/Vector-Hector/bifrost"
	"github.com/Vector-Hector/fptf"
	"net/http"
	"time"
)

type JourneyRequest struct {
	Origin      *fptf.Location      `json:"origin"`
	Destination *fptf.Location      `json:"destination"`
	Departure   time.Time           `json:"departure"`
	Mode        bifrost.RoutingMode `json:"mode"`
}

func RouteBifrost(from *fptf.Location, to *fptf.Location, date time.Time, onlyWalk bool) (*fptf.Journey, time.Duration, error) {
	mode := bifrost.ModeTransit
	if onlyWalk {
		mode = bifrost.ModeFoot
	}

	req := &JourneyRequest{
		Origin:      from,
		Destination: to,
		Departure:   date,
		Mode:        mode,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, 0, err
	}

	fmt.Println(string(data))

	reqStart := time.Now()
	resp, err := http.Post("http://localhost:8090/bifrost", "application/json", bytes.NewReader(data))
	if err != nil {
		return nil, 0, err
	}

	var journey fptf.Journey
	err = json.NewDecoder(resp.Body).Decode(&journey)
	if err != nil {
		return nil, 0, err
	}
	reqDuration := time.Since(reqStart)

	return &journey, reqDuration, nil
}
