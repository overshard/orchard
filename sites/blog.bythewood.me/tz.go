package main

import (
	"time"
	_ "time/tzdata"
)

// The image is FROM scratch with no zone files, so without the embedded copy
// local time is UTC and a post dated tomorrow goes up at 8pm tonight.
var eastern = func() *time.Location {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		panic(err)
	}
	return loc
}()
