package main

import (
	"encoding/json"
	"net/http"
	"strconv"

	"chat.bythewood.me/tools"
)

// The page fetches a widget's readings here rather than being handed them with
// the turn. That is what lets a chart change range without asking the model
// anything, and what makes a conversation opened a week later draw today's
// price instead of the one that was on screen when the question was asked.

// Widget is the subject of a chart, recorded on the message that produced it.
type Widget = tools.Widget

func (s *site) widgetTicker(w http.ResponseWriter, r *http.Request) {
	sym := r.URL.Query().Get("symbol")
	if sym == "" {
		http.Error(w, "symbol is required", 400)
		return
	}
	series, err := tools.Ticker(r.Context(), s.engine.Deps(), sym, r.URL.Query().Get("range"))
	if err != nil {
		widgetErr(w, err)
		return
	}
	writeJSON(w, series)
}

func (s *site) widgetWeather(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	lat, err1 := strconv.ParseFloat(q.Get("lat"), 64)
	lon, err2 := strconv.ParseFloat(q.Get("lon"), 64)
	if err1 != nil || err2 != nil {
		http.Error(w, "lat and lon are required", 400)
		return
	}
	days, _ := strconv.Atoi(q.Get("days"))
	rep, err := tools.Forecast(r.Context(), s.engine.Deps(), lat, lon,
		q.Get("place"), q.Get("zip"), q.Get("country"), days)
	if err != nil {
		widgetErr(w, err)
		return
	}
	writeJSON(w, rep)
}

// widgetErr answers with the reason rather than a bare status, because the
// panel prints it: a rate limited host and a symbol that does not exist read
// identically as a blank chart otherwise.
func widgetErr(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadGateway)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}
