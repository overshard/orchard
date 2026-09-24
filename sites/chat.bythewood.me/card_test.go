package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLoadedReadsTheGatewayAndNeverWakesTheModel(t *testing.T) {
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		fmt.Fprint(w, `{"loaded":true,"models":[{"model":"local","name":"Ornith 1.5 9B","state":"ready"}]}`)
	}))
	defer srv.Close()

	name, loaded, up := NewLLM(srv.URL, "local", "k").Loaded(context.Background())
	if !loaded || !up {
		t.Errorf("loaded = %v, up = %v, want both true", loaded, up)
	}
	if name != "Ornith 1.5 9B" {
		t.Errorf("name = %q", name)
	}
	// /health and a completion both load the weights, which is the whole thing
	// this endpoint exists to avoid.
	if path != "/v1/running" {
		t.Errorf("asked the gateway for %q", path)
	}
}

func TestAGatewayThatDoesNotAnswerReadsAsDownAndNotAsFree(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusBadGateway)
	}))
	defer srv.Close()

	_, loaded, up := NewLLM(srv.URL, "local", "k").Loaded(context.Background())
	if loaded || up {
		t.Errorf("loaded = %v, up = %v, want both false", loaded, up)
	}
}

// The picture model takes the card when a picture is drawn, and the chip has to
// name that one rather than the chat model it was built around.
func TestLoadedNamesWhicheverModelIsOnTheCard(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"loaded":true,"models":[{"model":"local","name":"Ornith 1.5 9B","state":"stopping"},`+
			`{"model":"image","name":"FLUX.2 klein 4B","state":"starting"}]}`)
	}))
	defer srv.Close()

	name, loaded, _ := NewLLM(srv.URL, "local", "k").Loaded(context.Background())
	if !loaded || name != "FLUX.2 klein 4B" {
		t.Errorf("name = %q, loaded = %v", name, loaded)
	}
}

func TestUnloadPostsToTheGateway(t *testing.T) {
	var method, path, auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path, auth = r.Method, r.URL.Path, r.Header.Get("Authorization")
		fmt.Fprint(w, `{"loaded":false}`)
	}))
	defer srv.Close()

	if err := NewLLM(srv.URL, "local", "k").Unload(context.Background()); err != nil {
		t.Fatal(err)
	}
	if method != "POST" || path != "/v1/unload" {
		t.Errorf("gateway got %s %s, want POST /v1/unload", method, path)
	}
	if auth != "Bearer k" {
		t.Errorf("the call was unkeyed: %q", auth)
	}
}

// Pulling the weights out from under a running turn kills it with an error
// nobody would connect to the button they pressed.
func TestUnloadIsRefusedWhileATurnIsRunning(t *testing.T) {
	reached := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
	}))
	defer srv.Close()

	q := NewQueue()
	release, ok := q.Enter(context.Background(), nil)
	if !ok {
		t.Fatal("could not take the queue")
	}
	defer release()

	s := &site{llm: NewLLM(srv.URL, "local", "k"), queue: q}
	rec := httptest.NewRecorder()
	s.unload(rec, httptest.NewRequest("POST", "/api/unload", nil))

	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409", rec.Code)
	}
	if reached {
		t.Error("the model was unloaded while a turn was running")
	}
}

func TestUnloadGoesThroughWhenNothingIsRunning(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"loaded":false}`)
	}))
	defer srv.Close()

	s := &site{llm: NewLLM(srv.URL, "local", "k"), queue: NewQueue()}
	rec := httptest.NewRecorder()
	s.unload(rec, httptest.NewRequest("POST", "/api/unload", nil))

	if rec.Code != 200 {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}
