// beets-ui: browse a beets Library and Modify it, through beets-api.
package main

import (
	"flag"
	"log"
	"net/http"
	"time"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:8339", "listen address; there is no auth, so keep it local")
	api := flag.String("api", "http://127.0.0.1:8338", "beets-api base URL")
	flag.Parse()

	s := newServer(*api, &http.Client{Timeout: 60 * time.Second})
	log.Printf("beets-ui on http://%s, using beets-api at %s", *listen, *api)
	log.Fatal(http.ListenAndServe(*listen, s.routes()))
}
