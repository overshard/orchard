package tools

import (
	"strings"
	"testing"
)

// Search is asked to skip its own history row every time, and to stop the
// gateway logging only when the chat turn behind the question is incognito.
func TestDeepSearchOnlyClaimsIncognitoWhenTheTurnIs(t *testing.T) {
	ordinary := deepSearchURL("who won", false)
	if !strings.Contains(ordinary, "nohistory=1") {
		t.Errorf("no history flag: %s", ordinary)
	}
	if strings.Contains(ordinary, "incognito=1") {
		t.Errorf("an ordinary turn asked for incognito: %s", ordinary)
	}
	if incog := deepSearchURL("who won", true); !strings.Contains(incog, "incognito=1") {
		t.Errorf("an incognito turn did not ask for it: %s", incog)
	}
}
