package stream

import "strconv"

const (
	VertexVertexID   = iota
	VertexOrderPos   = iota
	VertexImportance = iota
	VertexGeom       = iota
)

var VertexFieldsOrder = []string{"vertex_id", "order_pos", "importance", "geom"}

const (
	EdgeFromVertexID         = iota
	EdgeToVertexID           = iota
	EdgeWeight               = iota
	EdgeGeom                 = iota
	EdgeWasOneWay            = iota
	EdgeEdgeID               = iota
	EdgeOsmWayFrom           = iota
	EdgeOsmWayTo             = iota
	EdgeOsmWayFromSourceNode = iota
	EdgeOsmWayFromTargetNode = iota
	EdgeOsmWayToSourceNode   = iota
	EdgeOsmWayToTargetNode   = iota
)

var EdgeFieldsOrder = []string{"from_vertex_id", "to_vertex_id", "weight", "geom", "was_one_way", "edge_id", "osm_way_from", "osm_way_to", "osm_way_from_source_node", "osm_way_from_target_node", "osm_way_to_source_node", "osm_way_to_target_node"}

const (
	ShortcutFromVertexID = iota
	ShortcutToVertexID   = iota
	ShortcutWeight       = iota
	ShortcutViaVertexID  = iota
)

var ShortcutFieldsOrder = []string{"from_vertex_id", "to_vertex_id", "weight", "via_vertex_id"}

type StringArr []string

func (v StringArr) GetInt(index int) int64 {
	i, err := strconv.ParseInt(v[index], 10, 64)
	if err != nil {
		panic(err)
	}

	return i
}

func (v StringArr) GetFloat(index int) float64 {
	f, err := strconv.ParseFloat(v[index], 64)
	if err != nil {
		panic(err)
	}

	return f
}

func (v StringArr) GetString(index int) string {
	return v[index]
}

func IterateVertices(fileName string, threadCount int, handler func(StringArr)) error {
	return iterateCsvFileStringBuffer(fileName, ';', VertexFieldsOrder, threadCount, handler)
}

func IterateEdges(fileName string, threadCount int, handler func(StringArr)) error {
	return iterateCsvFileStringBuffer(fileName, ';', EdgeFieldsOrder, threadCount, handler)
}

func IterateShortcuts(fileName string, threadCount int, handler func(StringArr)) error {
	return iterateCsvFileStringBuffer(fileName, ';', ShortcutFieldsOrder, threadCount, handler)
}
