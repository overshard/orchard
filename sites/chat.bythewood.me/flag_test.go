package main

import "testing"

func TestAskingToComeBackToAChatIsAFlag(t *testing.T) {
	for msg, want := range map[string]bool{
		"this is not true, remember in memory to check this chat later to fix":                   true,
		"this didn't do anything -- remember this chat and that i nshould fix it when iget home": true,
		"flag this conversation": true,
		"Hmmmm tomorrow is the 30th... Remember you got this wrong so we can fix later":                      true,
		"i don't think this is correct nad up to date info if you want to remember this so we can fix later": true,
		"remember this is wrong it's an hour and 33 mins lol":                                                true,
		"remember that I like watercolour":                                                                   false,
		"remember that the long walk is on netflix":                                                          false,
		"add the long walk to the list of movies we want to see":                                             false,
		"what did we decide in the chat about tailscale":                                                     false,
	} {
		if got := flagAsk.MatchString(msg); got != want {
			t.Errorf("flagAsk(%q) = %v, want %v", msg, got, want)
		}
	}
}

func TestAFlagIsKeptOnTheConversationAndCleared(t *testing.T) {
	s := testStore(t)
	id, err := s.NewConversation("tailscale")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Append(id, Stored{Role: RoleUser, Content: "hi"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetFlag(id, "check this chat later"); err != nil {
		t.Fatal(err)
	}
	if c, _ := s.Get(id); c.Flag != "check this chat later" {
		t.Errorf("Get flag = %q", c.Flag)
	}
	list, _ := s.List(10)
	if len(list) != 1 || list[0].Flag == "" {
		t.Errorf("the list lost the flag: %+v", list)
	}
	_ = s.SetFlag(id, "")
	if c, _ := s.Get(id); c.Flag != "" {
		t.Errorf("a cleared flag is still %q", c.Flag)
	}
}
