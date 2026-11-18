package main

import "testing"

func TestProviderRelevance(t *testing.T) {
	cases := []struct {
		provider, text string
		want           bool
	}{
		{"OVH VINT HILL", "[US][Backbone] - Backbone Incident Notification", false},
		{"OVH VINT HILL", "[US-EAST-VA] Network degradation", true},
		{"OVH VINT HILL", "[GRA] Object storage degraded", false},
		{"CLOUDFLARE", "R2 Downloads Stalling in Japan", false},
		{"CLOUDFLARE", "Cloudflare Tunnel connectivity issues", true},
		{"ANTHROPIC", "Claude unable to join meetings", false},
		{"ANTHROPIC", "Elevated errors on Claude Haiku", true},
	}
	for _, c := range cases {
		for _, p := range providers {
			if p.label == c.provider {
				if got := p.relevant.MatchString(c.text); got != c.want {
					t.Errorf("%s %q = %v, want %v", c.provider, c.text, got, c.want)
				}
			}
		}
	}
}
