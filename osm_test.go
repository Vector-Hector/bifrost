package bifrost

import "testing"

func TestOSMImport(t *testing.T) {
	err := b.AddOSM("data/mvv/oberbayern-latest.osm.pbf")
	if err != nil {
		t.Fatal(err)
	}
}
