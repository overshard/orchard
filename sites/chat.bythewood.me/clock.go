package main

import (
	"context"
	"encoding/json"
	"regexp"

	"chat.bythewood.me/tools"
)

// A countdown is read off the clock before the model decides anything, since
// handed the time it said 10am was 33 minutes away at 8:27.
var untilAsk = regexp.MustCompile(`(?i)\b(?:how\s+(?:long|much\s+(?:longer|time)|many\s+(?:more\s+)?(?:hours|hrs|minutes|mins|days|weeks))|time\s+left|countdown)\b.*?\b(?:until|till|til|'til|before|to)\s+(.+?)\s*[?.!]*\s*$`)

func (e *Engine) clockOpening(ctx context.Context, deps *tools.Deps, question string) (tools.Result, Message, bool) {
	m := untilAsk.FindStringSubmatch(question)
	if m == nil {
		return tools.Result{}, Message{}, false
	}
	if _, ok := tools.ParseUntil(m[1], e.local()); !ok {
		return tools.Result{}, Message{}, false
	}
	args, err := json.Marshal(map[string]string{"timezone": e.tz, "until": m[1]})
	if err != nil {
		return tools.Result{}, Message{}, false
	}
	res := e.reg.Call(ctx, deps, tools.Now.Name, args)
	if res.Err != "" {
		return tools.Result{}, Message{}, false
	}
	msg := Message{Role: RoleUser, Content: "Before you answer, the clock was read for you and the time left " +
		"worked out: " + resultText(res) + "\n\nGive the remaining time exactly as it is here rather than doing " +
		"the arithmetic again."}
	return res, msg, true
}
