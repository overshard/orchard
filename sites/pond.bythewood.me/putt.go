package main

// Pad putt deals its nine holes in the page from a pool of par twos, threes
// and fours, so the server only bounds the score. A hole pays ten at par and
// three more a stroke under, so an ace on a par four is the most one can make.
const (
	puttHoles = 9
	puttAce   = 19
	puttMost  = puttHoles * puttAce
)
