package main

import (
	"crypto/sha256"
	"encoding/base64"
	"html/template"
)

// earlyScript has to run before the first paint, so it stays inline and the
// CSP allows it by hash. It must not vary per page or the hash stops matching.
const earlyScript = `(function () {
  // Rules that start an element at opacity 0 and rely on a script to reveal it
  // must be gated on data-js, or a throw in the module leaves them invisible.
  var root = document.documentElement;
  root.dataset.js = "true";
  var build = root.dataset.build;
  var week = 7 * 24 * 60 * 60 * 1000;
  try {
    var seen = JSON.parse(localStorage.getItem("ib.build") || "null");
    if (seen && seen.b === build && Date.now() - seen.t < week) {
      root.dataset.returning = "true";
    }
    // Stamped on every load, so the window is a week since the last visit.
    localStorage.setItem("ib.build", JSON.stringify({ b: build, t: Date.now() }));
  } catch (e) {}
})();`

func earlyScriptHash() string {
	sum := sha256.Sum256([]byte(earlyScript))
	return "'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
}

var templateFuncs = template.FuncMap{
	"earlyScript": func() template.JS { return earlyScript },
}
