package main

import (
	"fmt"
	"strings"
)

// The map on the code page is drawn twice, wide with the sites fanned out to
// the right and tall for a phone with them in two columns under the edge, so
// neither has to shrink its text to fit the other's shape.

type TopoNode struct {
	Name, Label string
	X, Y, W     int
	Wire        string
	Packet      bool
	PacketAlt   bool
	Health      Health
}

type Topology struct {
	Wide, Tall     []TopoNode
	TallHeight     int
	TallWallHeight int
	TallTrunkEnds  [2]int
}

const (
	wideHubX, wideHubY = 468, 200
	wideNodeX, wideW   = 540, 196
	tallColW           = 150
	tallTop            = 300
	tallRow            = 32
)

func buildTopology(health func(Site) Health) Topology {
	var t Topology
	rows := (len(sites) + 1) / 2
	for i, s := range sites {
		h := health(s)

		y := 30 + i*(340/(len(sites)-1))
		t.Wide = append(t.Wide, TopoNode{
			Name: s.Name, Label: s.Symbol + "  " + s.Host(), X: wideNodeX, Y: y, W: wideW, Health: h,
			Wire: fmt.Sprintf("M%d %d C %d %d, %d %d, %d %d",
				wideHubX, wideHubY, wideHubX+36, wideHubY, wideNodeX-36, y, wideNodeX, y),
			Packet:    i%3 == 1,
			PacketAlt: i%2 == 0,
		})

		col, row := i/rows, i%rows
		trunk := 14 + col*172
		ty := tallTop + row*tallRow
		t.Tall = append(t.Tall, TopoNode{
			Name: s.Name, Label: s.Symbol + "  " + strings.TrimSuffix(s.Name, ".com"), X: trunk + 12, Y: ty, W: tallColW, Health: h,
			Wire: fmt.Sprintf("M%d %d H%d", trunk, ty, trunk+12),
		})
	}
	t.TallTrunkEnds = [2]int{tallTop + (rows-1)*tallRow, tallTop + (len(sites)-rows-1)*tallRow}
	t.TallHeight = tallTop + rows*tallRow
	t.TallWallHeight = t.TallHeight - 6 - 122
	return t
}
