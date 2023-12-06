# Benchmarks

We compare our routing engine to OpenTripPlanner due to its similarity in that they are both multi-modal routing
engines.
Our current implementation uses a simple RAPTOR algorithm and OTP uses a more complex RAPTOR algorithm, so comparisons
are not
entirely fair. However, as features are added to Bifrost, we will update the benchmarks to reflect the improvements.

## Methodology

We transform OTP into fptf and compare the computing times of the two engines. We use the same data for both engines
that is
a [GTFS dataset](https://www.mvg.de/services/fahrgastservice/fahrplandaten.html) of the Munich public transport network
and an
[OSM dataset](https://download.geofabrik.de/europe/germany/bayern/oberbayern.html) of the Munich area. We use the same
origins,
destinations and departure times for both engines. The algorithm generates the origins and destinations randomly in the
munich city center. We use the same hardware and operating
system for both engines that is a 3.6 GHz Intel Core i7 with 16 GB of RAM on a Windows 10 system. We request each
engine
on 12 goroutines. We average the execution time over 100 runs. Two types of durations are calculated: the global average
execution time that is the time it takes to finish all threads divided by number of calculated routes and the local
average execution time that is the time it takes to finish one thread divided by number of calculated routes. Note, that
the global average execution time also includes transformation to FPTF, but this is negligible. The used memory is
measured by the Windows task manager.

## Results

| Engine  | Global Average Execution Time | Local Average Execution Time | Memory Usage |
|---------|-------------------------------|------------------------------|--------------|
| Bifrost | 36.1ms                        | 422.4ms                      | 800 MB       |
| OTP     | 85.8ms                        | 996.8ms                      | 4026 MB      |