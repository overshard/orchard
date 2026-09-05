package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

// The three shapes instructions actually arrive in, plus an @graph wrapper,
// since a fixed lookup misses every site that uses one.
const recipePage = `<html><head>
<script type="application/ld+json">
{"@context":"https://schema.org","@graph":[
 {"@type":"WebPage","name":"not the recipe"},
 {"@type":["Recipe","Thing"],
  "name":"Breakfast Burritos",
  "recipeYield":["8","8 burritos"],
  "prepTime":"PT20M","cookTime":"PT1H15M","totalTime":"PT1H35M",
  "recipeIngredient":["8 large eggs","1/2 cup shredded cheddar","4 burrito-size flour tortillas","2 tbsp butter"],
  "recipeInstructions":[
    {"@type":"HowToSection","itemListElement":[
      {"@type":"HowToStep","text":"Beat the eggs with a pinch of salt."},
      {"@type":"HowToStep","text":"Melt the butter and scramble over low heat."}]},
    {"@type":"HowToStep","text":"Warm each tortilla until pliable, fill, and roll."}]}]}
</script></head><body><p>words words words</p></body></html>`

func TestRecipeFromJSONLD(t *testing.T) {
	doc, err := html.Parse(strings.NewReader(recipePage))
	if err != nil {
		t.Fatal(err)
	}
	r, ok := recipeFromJSONLD(doc)
	if !ok {
		t.Fatal("no recipe found in a page that has one")
	}
	if r.Name != "Breakfast Burritos" {
		t.Errorf("name = %q", r.Name)
	}
	if r.Yield != "8" {
		t.Errorf("yield = %q", r.Yield)
	}
	if r.TotalTime != "1h 35m" {
		t.Errorf("total time = %q, want the ISO duration in words", r.TotalTime)
	}
	if r.CookTime != "1h 15m" {
		t.Errorf("cook time = %q", r.CookTime)
	}
	if r.PrepTime != "20 minutes" {
		t.Errorf("prep time = %q", r.PrepTime)
	}
	if len(r.Ingredients) != 4 || !strings.HasPrefix(r.Ingredients[0], "8 large eggs") {
		t.Errorf("ingredients = %v", r.Ingredients)
	}
	// The point of the whole file: the quantity survives.
	md := r.Markdown()
	for _, want := range []string{"8 large eggs", "1/2 cup shredded cheddar", "Makes: 8", "1. Beat the eggs"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown is missing %q:\n%s", want, md)
		}
	}
	// A section wrapping steps must flatten rather than disappear.
	if len(r.Steps) != 3 {
		t.Errorf("want 3 steps flattened out of the section, got %d: %v", len(r.Steps), r.Steps)
	}
}

func TestRecipeIgnoresNonRecipe(t *testing.T) {
	doc, _ := html.Parse(strings.NewReader(
		`<html><head><script type="application/ld+json">{"@type":"Article","name":"x"}</script></head></html>`))
	if _, ok := recipeFromJSONLD(doc); ok {
		t.Error("claimed a recipe on an article page")
	}
}

// TestFetchKeepsRecipe goes through the whole of Fetch rather than calling the
// parser directly, because the first version of this worked in isolation and
// did nothing in production: stripAndPick removes script tags in place, so
// reading the structured data after it ran found an empty document.
func TestFetchKeepsRecipe(t *testing.T) {
	page := `<html><head>
<script type="application/ld+json">
{"@type":"Recipe","name":"Breakfast Burritos","recipeYield":"8",
 "recipeIngredient":["8 large eggs","1/2 cup shredded cheddar","4 flour tortillas"],
 "recipeInstructions":[{"@type":"HowToStep","text":"Beat the eggs."},
                       {"@type":"HowToStep","text":"Scramble and roll."}]}
</script></head><body><article>` +
		strings.Repeat("Some prose about burritos that mentions eggs and cheese but no amounts. ", 12) +
		`</article></body></html>`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, page)
	}))
	defer srv.Close()

	p, err := Fetch(srv.Client(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if !hasStructuredRecipe(p) {
		t.Fatalf("the recipe block is not at the top of the markdown:\n%.300s", p.Markdown)
	}
	for _, want := range []string{"8 large eggs", "1/2 cup shredded cheddar", "Makes: 8"} {
		if !strings.Contains(p.Markdown, want) {
			t.Errorf("markdown lost %q", want)
		}
	}
}
