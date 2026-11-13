// beets-ui: browse a beets Library and Modify it, through beets-api.
package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"time"
)

func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

func main() {
	listen := flag.String("listen", "127.0.0.1:8339", "listen address; keep it reachable only through the proxy that signs people in")
	api := flag.String("api", "http://127.0.0.1:8338", "beets-api base URL")
	actorHeader := flag.String("actor-header", envOr("BEETS_UI_ACTOR_HEADER", defaultActorHeader),
		"request header the proxy puts the person's name in; requests without it are refused (env BEETS_UI_ACTOR_HEADER)")
	flag.Parse()

	s := newServer(*api, &http.Client{Timeout: 60 * time.Second})
	s.actorHeader = http.CanonicalHeaderKey(*actorHeader)
	log.Printf("beets-ui on http://%s, using beets-api at %s, actor from %s", *listen, *api, s.actorHeader)
	log.Fatal(http.ListenAndServe(*listen, s.routes()))
}
