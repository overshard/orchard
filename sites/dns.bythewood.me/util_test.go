package main

import (
	"net/netip"
	"strconv"
)

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func netipLoopback() netip.Addr { return netip.MustParseAddr("127.0.0.1") }
