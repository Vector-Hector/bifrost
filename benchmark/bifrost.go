package main

import (
	"bytes"
	"encoding/json"
	"github.com/Vector-Hector/fptf"
	"net/http"
	"time"
)

type JourneyRequest struct {
	Origin      *fptf.Location `json:"origin"`
	Destination *fptf.Location `json:"destination"`
	Departure   time.Time      `json:"departure"`
	OnlyWalk    bool           `json:"onlyWalk"`
}

func RouteBifrost(from *fptf.Location, to *fptf.Location, date time.Time, onlyWalk bool) (*fptf.Journey, time.Duration, error) {
	req := &JourneyRequest{
		Origin:      from,
		Destination: to,
		Departure:   date,
		OnlyWalk:    onlyWalk,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, 0, err
	}

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
