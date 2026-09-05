package main

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestPythonDepsSkipsStdlibAndMapsNames(t *testing.T) {
	code := strings.Join([]string{
		"import os, json",
		"import requests",
		"from flask import Flask, jsonify",
		"import yfinance as yf",
		"from bs4 import BeautifulSoup",
		"import yaml",
	}, "\n")
	got := names(pythonDeps(code))
	want := []string{"requests", "flask", "yfinance", "beautifulsoup4", "pyyaml"}
	if !sameSet(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestJSDepsSkipsRelativeAndBuiltin(t *testing.T) {
	code := strings.Join([]string{
		`import express from "express";`,
		`import { z } from "@hono/zod-validator";`,
		`const fs = require("node:fs");`,
		`import local from "./util.js";`,
	}, "\n")
	got := names(jsDeps(code))
	if !sameSet(got, []string{"express", "@hono/zod-validator"}) {
		t.Errorf("got %v", got)
	}
}

func TestGoDepsTrimsToTheModuleRoot(t *testing.T) {
	code := "import (\n\t\"fmt\"\n\t\"github.com/gin-gonic/gin/binding\"\n\t\"modernc.org/sqlite\"\n)\n"
	got := names(goDeps(code))
	if !sameSet(got, []string{"github.com/gin-gonic/gin", "modernc.org/sqlite"}) {
		t.Errorf("got %v", got)
	}
}

func TestDockerRepoNormalises(t *testing.T) {
	cases := map[string]string{
		"ollama/ollama:latest":     "ollama/ollama",
		"python:3.12-slim":         "library/python",
		"alpine":                   "library/alpine",
		"builder":                  "",
		"scratch":                  "",
		"ghcr.io/someone/thing:v1": "",
		"$BASE_IMAGE":              "",
	}
	for in, want := range cases {
		if got := dockerRepo(in); got != want {
			t.Errorf("dockerRepo(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestShellDepsReadsInstallLines(t *testing.T) {
	code := "pip install flask==3.0.0 requests\nnpm install -g typescript\ndocker run -d --name ollama -p 11434:11434 ollama/ollama\n"
	deps := shellDeps(code)
	got := names(deps)
	if !sameSet(got, []string{"flask", "requests", "typescript", "ollama/ollama"}) {
		t.Errorf("got %v", got)
	}
}

func TestGoProxyEscape(t *testing.T) {
	if got := goProxyEscape("github.com/BurntSushi/toml"); got != "github.com/!burnt!sushi/toml" {
		t.Errorf("got %q", got)
	}
}

// stubRegistry answers as the four registries would, so the lookup path is
// exercised without touching the network.
type stubRegistry struct{ known map[string]bool }

func (s stubRegistry) RoundTrip(r *http.Request) (*http.Response, error) {
	code := http.StatusNotFound
	if s.known[r.URL.Host+r.URL.Path] {
		code = http.StatusOK
	}
	return &http.Response{
		StatusCode: code,
		Body:       io.NopCloser(strings.NewReader("{}")),
		Header:     make(http.Header),
		Request:    r,
	}, nil
}

func TestVerifyDepsMarksTheMissingOne(t *testing.T) {
	client := &http.Client{Transport: stubRegistry{known: map[string]bool{
		"pypi.org/pypi/flask/json": true,
	}}}
	blocks := []CodeBlock{{Lang: "python", Closed: true, Code: "import flask\nimport flask_yahoo_cache\n"}}

	deps := verifyDeps(context.Background(), client, blocks)
	if len(deps) != 2 {
		t.Fatalf("want 2 deps, got %+v", deps)
	}
	byName := map[string]Dependency{}
	for _, d := range deps {
		byName[d.Name] = d
	}
	if !byName["flask"].Found || !byName["flask"].Checked {
		t.Errorf("flask should be found: %+v", byName["flask"])
	}
	if byName["flask-yahoo-cache"].Found {
		t.Errorf("an invented package was reported as real: %+v", byName["flask-yahoo-cache"])
	}
	warns := depWarnings(deps)
	if len(warns) != 1 || !strings.Contains(warns[0], "flask-yahoo-cache") {
		t.Errorf("want one warning naming the missing package, got %v", warns)
	}
}

// A registry that will not answer is not evidence of anything, and saying a
// real package does not exist is worse than saying nothing.
func TestVerifyDepsStaysQuietWhenTheRegistryIsDown(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusTooManyRequests, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
	})}
	deps := verifyDeps(context.Background(), client, []CodeBlock{{Lang: "python", Closed: true, Code: "import requests\n"}})
	if len(deps) != 1 || deps[0].Checked {
		t.Fatalf("a throttled registry should leave the dep unchecked: %+v", deps)
	}
	if warns := depWarnings(deps); len(warns) != 0 {
		t.Errorf("nothing should be warned about, got %v", warns)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func names(deps []Dependency) []string {
	out := make([]string, 0, len(deps))
	seen := map[string]bool{}
	for _, d := range deps {
		if !seen[d.Name] {
			seen[d.Name] = true
			out = append(out, d.Name)
		}
	}
	return out
}

func sameSet(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	have := map[string]bool{}
	for _, g := range got {
		have[g] = true
	}
	for _, w := range want {
		if !have[w] {
			return false
		}
	}
	return true
}
