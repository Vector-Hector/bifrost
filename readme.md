# Bifrost

A blazing fast, multi-modal routing engine in go.

## How it works

The routing algorithm is based on dijkstra and the RAPTOR algorithm. It switches each round between public transport
and street routing to find the best multi-modal path. 

## References

Thanks to all the people who wrote the following articles, algorithms and libraries:

- [Raptor Agorithm Paper](https://www.microsoft.com/en-us/research/wp-content/uploads/2012/01/raptor_alenex.pdf): The paper that describes the RAPTOR algorithm
- [Simple version of RAPTOR in python](https://kuanbutts.com/2020/09/12/raptor-simple-example/): Helped me understand the algorithm and implement it
- [Dijkstra](https://en.wikipedia.org/wiki/Dijkstra%27s_algorithm): For street routing
- [GTFS](https://developers.google.com/transit/gtfs/reference): For public transport data
- [OSM](https://www.openstreetmap.org/): For street data
- [osm2ch](https://github.com/LdDl/osm2ch): For converting OSM to csv files