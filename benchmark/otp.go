package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/Vector-Hector/fptf"
	"io"
	"net/http"
	"strings"
	"time"
)

type GraphQLRequest struct {
	Query string `json:"query"`
}

func RouteOpenTripPlanner(from *fptf.Location, to *fptf.Location, date time.Time, onlyWalk bool) (*fptf.Journey, time.Duration, error) {
	var modes []string

	if onlyWalk {
		modes = []string{"{mode:WALK}"}
	} else {
		modes = []string{"{mode:WALK}", "{mode:TRANSIT}"}
	}

	query := fmt.Sprintf(`{
    plan(
        from: { lat: %f, lon: %f }
        to: {lat: %f, lon: %f }
      
        date: "%s"
        time: "%s"
      
        transportModes: [%s]
	) {
        itineraries {
            startTime
            endTime
            legs {
                mode
                startTime
                endTime
                agency {
                    id
                    name
                    gtfsId
                }
                from {
                    name
                    lat
                    lon
                    departureTime
                    arrivalTime
                }
                to {
                    name
                    lat
                    lon
                    departureTime
                    arrivalTime
                }
                route {
                    gtfsId
                    longName
                    shortName
                }
                legGeometry {
                    points
                }
            }
        }
    }
}
`, from.Latitude, from.Longitude, to.Latitude, to.Longitude, date.Format("2006-01-02"), date.Format("15:04"), strings.Join(modes, ","))

	req, err := json.Marshal(&GraphQLRequest{
		Query: query,
	})
	if err != nil {
		return nil, 0, err
	}

	reqStart := time.Now()
	resp, err := http.Post("http://localhost:8080/otp/routers/default/index/graphql", "application/json", bytes.NewReader(req))
	if err != nil {
		return nil, 0, err
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, err
	}

	reqDuration := time.Since(reqStart)

	var otpResp OtpResponse
	err = json.Unmarshal(data, &otpResp)
	if err != nil {
		return nil, 0, err
	}

	journeys := otpResp.Transform()

	// choose the one with the earliest arrival time

	bestArrival := time.Time{}
	var bestJourney *fptf.Journey

	for _, journey := range journeys {
		if bestJourney == nil || journey.GetArrival().Before(bestArrival) {
			bestArrival = journey.GetArrival()
			bestJourney = journey
		}
	}

	return bestJourney, reqDuration, nil
}

type OtpResponse struct {
	Data struct {
		Plan struct {
			Itineraries []*OtpItinerary `json:"itineraries"`
		} `json:"plan"`
	} `json:"data"`
}

func (o *OtpResponse) Transform() []*fptf.Journey {
	iti := o.Data.Plan.Itineraries

	journeys := make([]*fptf.Journey, len(iti))

	for i, itinerary := range iti {
		journeys[i] = itinerary.Transform()
	}

	return journeys
}

type OtpItinerary struct {
	StartTime int64     `json:"startTime"`
	EndTime   int64     `json:"endTime"`
	Legs      []*OtpLeg `json:"legs"`
}

func (o *OtpItinerary) Transform() *fptf.Journey {
	journey := &fptf.Journey{
		Trips: make([]*fptf.Trip, len(o.Legs)),
	}

	for i, leg := range o.Legs {
		journey.Trips[i] = leg.Transform()
	}

	return journey
}

type OtpLeg struct {
	Mode      string      `json:"mode"`
	StartTime int64       `json:"startTime"`
	EndTime   int64       `json:"endTime"`
	Agency    *OtpAgency  `json:"agency"`
	From      *OtpPlace   `json:"from"`
	To        *OtpPlace   `json:"to"`
	Route     *OtpRoute   `json:"route"`
	Stopovers []*OtpPlace `json:"intermediateStops"`
}

func (o *OtpLeg) Transform() *fptf.Trip {
	if o == nil {
		return nil
	}

	return &fptf.Trip{
		Origin:      o.From.TransformToStopStation(),
		Destination: o.To.TransformToStopStation(),
		Departure:   o.From.TransformToDeparture(),
		Arrival:     o.To.TransformToArrival(),
		Mode:        TransformMode(o.Mode),
		SubMode:     strings.ToLower(o.Mode),
		Operator:    o.Agency.Transform(),
		Line:        o.Route.Transform(),
		Stopovers:   o.TransformStopovers(),
	}
}

func (o *OtpLeg) TransformStopovers() []*fptf.Stopover {
	stopovers := make([]*fptf.Stopover, len(o.Stopovers)+2)

	stopovers[0] = o.From.TransformToStopover()

	for i, stopover := range o.Stopovers {
		stopovers[i+1] = stopover.TransformToStopover()
	}

	stopovers[len(stopovers)-1] = o.To.TransformToStopover()

	return stopovers
}

func TransformMode(mode string) fptf.Mode {
	switch mode {
	case "WALK":
		return fptf.ModeWalking
	case "BUS":
		return fptf.ModeBus
	case "RAIL":
		return fptf.ModeTrain
	case "TRAM":
		return fptf.ModeTrain
	case "SUBWAY":
		return fptf.ModeTrain
	case "TRANSIT":
		return fptf.ModeTrain
	case "BICYCLE":
		return fptf.ModeBicycle
	case "CAR":
		return fptf.ModeCar
	default:
		return ""
	}
}

type OtpAgency struct {
	Id     string `json:"id"`
	Name   string `json:"name"`
	GtfsId string `json:"gtfsId"`
}

func (o *OtpAgency) Transform() *fptf.Operator {
	if o == nil {
		return nil
	}
	return &fptf.Operator{
		Id:   o.Id,
		Name: o.Name,
	}
}

type OtpPlace struct {
	Stop          *OtpStop `json:"stop"`
	Name          string   `json:"name"`
	Lat           float64  `json:"lat"`
	Lon           float64  `json:"lon"`
	DepartureTime int64    `json:"departureTime"`
	ArrivalTime   int64    `json:"arrivalTime"`
}

type OtpStop struct {
	Id string `json:"gtfsId"`
}

func (o *OtpPlace) TransformToStopStation() *fptf.StopStation {
	if o == nil {
		return nil
	}

	id := ""
	if o.Stop != nil {
		id = o.Stop.Id
	}

	return &fptf.StopStation{
		Station: &fptf.Station{
			Id:       id,
			Name:     o.Name,
			Location: &fptf.Location{Longitude: o.Lon, Latitude: o.Lat},
		},
	}
}

func (o *OtpPlace) TransformToDeparture() fptf.TimeNullable {
	if o == nil {
		return fptf.TimeNullable{}
	}

	return fptf.TimeNullable{Time: time.Unix(o.DepartureTime/1000, (o.DepartureTime%1000)*1000000)}
}

func (o *OtpPlace) TransformToArrival() fptf.TimeNullable {
	if o == nil {
		return fptf.TimeNullable{}
	}

	return fptf.TimeNullable{Time: time.Unix(o.ArrivalTime/1000, (o.ArrivalTime%1000)*1000000)}
}

func (o *OtpPlace) TransformToStopover() *fptf.Stopover {
	if o == nil {
		return nil
	}

	return &fptf.Stopover{
		StopStation: o.TransformToStopStation(),
		Arrival:     o.TransformToArrival(),
		Departure:   o.TransformToDeparture(),
	}
}

type OtpRoute struct {
	GtfsId    string `json:"gtfsId"`
	LongName  string `json:"longName"`
	ShortName string `json:"shortName"`
}

func (o *OtpRoute) Transform() *fptf.Line {
	if o == nil {
		return nil
	}

	return &fptf.Line{
		Id:   o.GtfsId,
		Name: o.LongName,
	}
}
