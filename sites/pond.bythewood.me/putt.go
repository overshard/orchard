package main

// Pad putt's course is the same nine holes every day and lives in the page,
// so the server only bounds the score. A hole pays ten at par and three more
// a stroke under, so an ace on a par three is the most a hole can make.
const (
	puttHoles = 9
	puttAce   = 16
	puttMost  = puttHoles * puttAce
)
