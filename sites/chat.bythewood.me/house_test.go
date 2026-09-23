package main

import (
	"reflect"
	"testing"
)

func TestHouseQuestionReadsTheChipsAndTheFirstMessage(t *testing.T) {
	first := "$269,900\n88 Example St Granite Falls, NC 28630"
	cases := []struct {
		q        string
		history  []Message
		addr     string
		price    int
		sections []string
	}{
		{first, nil, "88 Example St Granite Falls, NC 28630", 269900, []string{"summary"}},
		// The question that got a clean bill it should not have.
		{"Is there anything I would not want to live near around 88 Example St, Granite Falls, NC, 28630, like data centres, factories, landfills or quarries?",
			[]Message{{Role: RoleUser, Content: first}},
			"88 Example St, Granite Falls, NC, 28630", 0, []string{"industry"}},
		{"What is the neighbourhood around 88 Example St, Granite Falls, NC, 28630 like, and what is the crime?", nil,
			"88 Example St, Granite Falls, NC, 28630", 0, []string{"area", "neighbours"}},
		// A follow up with no address is about the last house named.
		{"What about nursing jobs in the area for a CNA", []Message{
			{Role: RoleUser, Content: "402 Sample Rd, Lenoir, NC 28645 at $310,000"},
			{Role: RoleAssistant, Content: "At $310,000 the house..."}},
			"402 Sample Rd, Lenoir, NC 28645", 310000, []string{"commutes"}},
	}
	for _, c := range cases {
		addr, price, sections := houseQuestion(c.q, c.history)
		if addr != c.addr || price != c.price || !reflect.DeepEqual(sections, c.sections) {
			t.Errorf("%q\n got %q %d %v\nwant %q %d %v", c.q, addr, price, sections, c.addr, c.price, c.sections)
		}
	}
}

func TestHouseQuestionLeavesEverythingElseAlone(t *testing.T) {
	house := []Message{{Role: RoleUser, Content: "402 Sample Rd, Lenoir, NC 28645 at $310,000"}}
	for _, c := range []struct {
		q       string
		history []Message
	}{
		{"what's the weather like there this weekend", house},
		{"what schools are good in Caldwell County", nil},
		// No zip, so it could spend a cold lookup on a street that will not place.
		{"what about 12 Oak St", nil},
		{"is 1600 Pennsylvania Ave a real place", nil},
	} {
		if addr, _, _ := houseQuestion(c.q, c.history); addr != "" {
			t.Errorf("%q was read as a house question about %q", c.q, addr)
		}
	}
}
