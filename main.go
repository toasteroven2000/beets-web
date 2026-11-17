// beets-web: browse a beets Library and Modify it, through beets-api.
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
	listen := flag.String("listen", envOr("BEETS_WEB_LISTEN", "127.0.0.1:8339"), "listen address; keep it reachable only through the proxy that signs people in (env BEETS_WEB_LISTEN)")
	api := flag.String("api", envOr("BEETS_WEB_API", "http://127.0.0.1:8338"), "beets-api base URL (env BEETS_WEB_API)")
	actorHeader := flag.String("actor-header", envOr("BEETS_WEB_ACTOR_HEADER", defaultActorHeader),
		"request header the proxy puts the person's name in; requests without it are refused (env BEETS_WEB_ACTOR_HEADER)")
	thumbDir := flag.String("thumb-cache", os.Getenv("BEETS_WEB_THUMB_CACHE"),
		"directory to keep cover thumbnails in; without it the Library shows full covers (env BEETS_WEB_THUMB_CACHE)")
	flag.Parse()

	s := newServer(*api, &http.Client{Timeout: 60 * time.Second})
	s.actorHeader = http.CanonicalHeaderKey(*actorHeader)
	if *thumbDir != "" {
		c, err := newThumbCache(*thumbDir)
		if err != nil {
			log.Fatalf("thumbnail cache: %v", err)
		}
		s.thumbs = c
	}
	log.Printf("beets-web on http://%s, using beets-api at %s, actor from %s", *listen, *api, s.actorHeader)
	log.Fatal(http.ListenAndServe(*listen, s.routes()))
}
