package bifrost

import (
	"fmt"
	"github.com/LdDl/osm2ch"
	"strconv"
	"strings"
	"time"
)

var carTags = []string{
	"motorway",
	"primary",
	"primary_link",
	"road",
	"secondary",
	"secondary_link",
	"residential",
	"tertiary",
	"tertiary_link",
	"unclassified",
	"trunk",
	"trunk_link",
	"motorway_link",
}

var bikeTags = []string{
	"motorway",
	"primary",
	"primary_link",
	"road",
	"secondary",
	"secondary_link",
	"residential",
	"tertiary",
	"tertiary_link",
	"unclassified",
	"trunk",
	"trunk_link",
	"motorway_link",

	//"cycleway",
	//"path",
	//"footway",
	//"living_street",
	//"pedestrian",
	//"crossing",
	//"steps",
	//"residential",
	//"track",
	//"service",
	//"primary",
	//"primary_link",
	//"secondary",
	//"secondary_link",
	//"tertiary",
	//"tertiary_link",
}

var footTags = []string{
	"motorway",
	"primary",
	"primary_link",
	"road",
	"secondary",
	"secondary_link",
	"residential",
	"tertiary",
	"tertiary_link",
	"unclassified",
	"trunk",
	"trunk_link",
	"motorway_link",

	//"footway",
	//"path",
	//"living_street",
	//"pedestrian",
	//"crossing",
	//"steps",
	//"residential",
	//"track",
	//"service",
	//"secondary",
	//"secondary_link",
	//"tertiary",
	//"tertiary_link",
}

var carTagSet map[string]bool
var bikeTagSet map[string]bool
var footTagSet map[string]bool

func buildTags() []string {
	carTagSet = make(map[string]bool)
	allTags := make(map[string]bool)

	for _, tag := range carTags {
		carTagSet[tag] = true
		allTags[tag] = true
	}

	bikeTagSet = make(map[string]bool)
	for _, tag := range bikeTags {
		bikeTagSet[tag] = true
		allTags[tag] = true
	}

	footTagSet = make(map[string]bool)
	for _, tag := range footTags {
		footTagSet[tag] = true
		allTags[tag] = true
	}

	tags := make([]string, 0, len(allTags))
	for tag := range allTags {
		tags = append(tags, tag)
	}

	return tags
}

var osmCfg = &osm2ch.OsmConfiguration{
	EntityName: "highway", // Currrently we do not support others
	Tags:       buildTags(),
}

func (b *Bifrost) AddOSM(path string) error {
	t := time.Now()

	fmt.Println("Reading OSM data from", path)

	edges, err := osm2ch.ImportFromOSMFile(path, osmCfg)
	if err != nil {
		return err
	}

	fmt.Println("Found", len(edges), "edges")

	if b.Data == nil {
		b.Data = &RoutingData{}
	}

	lastVertex := uint64(len(b.Data.Vertices))

	fmt.Println("Converting edges to bifrost street graph format")

	if b.Data.NodesIndex == nil {
		b.Data.NodesIndex = make(map[int64]uint64)
	}

	if b.Data.StreetGraph == nil {
		b.Data.StreetGraph = make([][]Arc, len(b.Data.Vertices))
	}

	prog := Progress{}
	prog.Reset(uint64(len(edges)))

	for _, edge := range edges {
		prog.Increment()
		prog.Print()

		sourceVertKey, ok := b.Data.NodesIndex[int64(edge.Source)]
		if !ok {
			b.Data.NodesIndex[int64(edge.Source)] = lastVertex
			b.Data.Vertices = append(b.Data.Vertices, Vertex{
				Latitude:  edge.Geom[0].Lat,
				Longitude: edge.Geom[0].Lon,
			})
			b.Data.StreetGraph = append(b.Data.StreetGraph, make([]Arc, 0))
			b.Data.StopToRoutes = append(b.Data.StopToRoutes, nil)
			sourceVertKey = lastVertex
			lastVertex++
		}

		targetVertKey, ok := b.Data.NodesIndex[int64(edge.Target)]
		if !ok {
			b.Data.NodesIndex[int64(edge.Target)] = lastVertex
			b.Data.Vertices = append(b.Data.Vertices, Vertex{
				Latitude:  edge.Geom[len(edge.Geom)-1].Lat,
				Longitude: edge.Geom[len(edge.Geom)-1].Lon,
			})
			b.Data.StreetGraph = append(b.Data.StreetGraph, make([]Arc, 0))
			b.Data.StopToRoutes = append(b.Data.StopToRoutes, nil)
			targetVertKey = lastVertex
			lastVertex++
		}

		sourceDesc := b.getWayDescriptor(&edge.SourceComponent)
		targetDesc := b.getWayDescriptor(&edge.TargetComponent)

		if sourceDesc == nil && targetDesc == nil {
			continue
		}

		merged := sourceDesc.Merge(targetDesc)

		b.Data.StreetGraph[sourceVertKey] = append(b.Data.StreetGraph[sourceVertKey], Arc{
			Target:        targetVertKey,
			WalkDistance:  merged.WalkMs,
			CycleDistance: merged.CycleMs,
			CarDistance:   merged.CarMs,
		})

		if edge.WasOneway && merged.WalkMs > 0 {
			b.Data.StreetGraph[targetVertKey] = append(b.Data.StreetGraph[targetVertKey], Arc{
				Target:        sourceVertKey,
				WalkDistance:  merged.WalkMs,
				CycleDistance: merged.CycleMs,
				CarDistance:   merged.CarMs,
			}) // walkers can walk both ways
		}
	}

	b.Data.RebuildVertexTree()

	fmt.Println("Done reading OSM data.")
	fmt.Println("Reading OSM data took", time.Since(t))

	return nil
}

type wayDescriptor struct {
	WalkMs  uint32
	CycleMs uint32
	CarMs   uint32
}

func (w *wayDescriptor) Merge(v *wayDescriptor) *wayDescriptor {
	walk := uint32(0)
	if w.WalkMs > 0 && v.WalkMs > 0 {
		walk = w.WalkMs + v.WalkMs
	}

	cycle := uint32(0)
	if w.CycleMs > 0 && v.CycleMs > 0 {
		cycle = w.CycleMs + v.CycleMs
	}

	car := uint32(0)
	if w.CarMs > 0 && v.CarMs > 0 {
		car = w.CarMs + v.CarMs
	}

	return &wayDescriptor{
		WalkMs:  walk,
		CycleMs: cycle,
		CarMs:   car,
	}
}

func (b *Bifrost) getWayDescriptor(edge *osm2ch.ExpandedEdgeComponent) *wayDescriptor {
	highwayTagValue := ""

	for _, tag := range edge.Tags {
		if tag.Key != "highway" {
			continue
		}

		highwayTagValue = tag.Value
	}

	if highwayTagValue == "" {
		return nil
	}

	return &wayDescriptor{
		WalkMs:  b.getWalk(edge, highwayTagValue),
		CycleMs: b.getCycle(edge, highwayTagValue),
		CarMs:   b.getCar(edge, highwayTagValue),
	}
}

// isWalk returns true if the edge can be walked.
func (b *Bifrost) isWalk(edge *osm2ch.ExpandedEdgeComponent, highwayTagValue string) bool {
	if _, ok := footTagSet[highwayTagValue]; ok {
		return true
	}

	for _, tag := range edge.Tags {
		if tag.Key != "foot" && tag.Key != "sidewalk" {
			continue
		}

		if tag.Value == "no" {
			return false
		}

		return true
	}

	return false
}

// getWalk returns the walking distance in ms for an edge. If the edge cannot be walked, 0 is returned.
func (b *Bifrost) getWalk(edge *osm2ch.ExpandedEdgeComponent, highwayTagValue string) uint32 {
	if !b.isWalk(edge, highwayTagValue) {
		return 0
	}

	dist := uint32(edge.CostMeters / b.WalkingSpeed)
	if dist == 0 {
		dist = 1
	}

	if dist > b.MaxWalkingMs {
		return 0
	}

	return dist
}

// isCycle returns true if the edge can be cycled.
func (b *Bifrost) isCycle(edge *osm2ch.ExpandedEdgeComponent, highwayTagValue string) bool {
	if _, ok := bikeTagSet[highwayTagValue]; ok {
		return true
	}

	for _, tag := range edge.Tags {
		if tag.Key != "bicycle" && tag.Key != "sidewalk" {
			continue
		}

		if tag.Value == "no" {
			return false
		}

		return true
	}

	return false
}

// getCycle returns the cycling distance in ms for an edge. If the edge cannot be cycled, 0 is returned.
func (b *Bifrost) getCycle(edge *osm2ch.ExpandedEdgeComponent, highwayTagValue string) uint32 {
	if !b.isCycle(edge, highwayTagValue) {
		return 0
	}

	dist := uint32(edge.CostMeters / b.CycleSpeed)
	if dist == 0 {
		dist = 1
	}

	if dist > b.MaxCyclingMs {
		return 0
	}

	return dist
}

// isCar returns true if the edge can be driven.
func (b *Bifrost) isCar(edge *osm2ch.ExpandedEdgeComponent, highwayTagValue string) bool {
	if _, ok := carTagSet[highwayTagValue]; ok {
		return true
	}

	return false
}

// getCarSpeed returns the speed limit in m/ms for an edge.
func (b *Bifrost) getCarSpeed(edge *osm2ch.ExpandedEdgeComponent) float64 {
	for _, tag := range edge.Tags {
		if tag.Key != "maxspeed" {
			continue
		}

		if tag.Value == "" || tag.Value == "none" {
			return b.CarMaxSpeed
		}

		speed, err := parseSpeed(tag.Value, b.CarMaxSpeed)
		if err != nil {
			continue
		}

		return speed
	}

	return b.CarMaxSpeed
}

// parseSpeed parses a speed string and returns the speed in m/ms.
func parseSpeed(speedStr string, maxSpeed float64) (float64, error) {
	if speedStr == "" {
		return 0, fmt.Errorf("empty speed string")
	}

	speedStr = strings.TrimSpace(speedStr)

	impl, err := getImplicitMaxSpeedValue(speedStr, maxSpeed)
	if err == nil {
		return impl, nil
	}

	if strings.HasSuffix(speedStr, " mph") {
		num, err := strconv.Atoi(speedStr[:len(speedStr)-4])
		if err != nil {
			fmt.Println("error parsing speed", speedStr, err)
			return 0, err
		}

		return mph(float64(num))
	}

	num, err := strconv.Atoi(speedStr) // default is km/h
	if err != nil {
		fmt.Println("error parsing speed", speedStr, err)
		return 0, err
	}

	return kmph(float64(num))
}

// getCar returns the driving distance in ms for an edge. If the edge cannot be driven, 0 is returned.
func (b *Bifrost) getCar(edge *osm2ch.ExpandedEdgeComponent, highwayTagValue string) uint32 {
	if !b.isCar(edge, highwayTagValue) {
		return 0
	}

	speed := b.getCarSpeed(edge)

	dist := uint32(edge.CostMeters / speed)
	if dist == 0 {
		dist = 1
	}

	return dist
}
