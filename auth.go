package main

// Who is using beets-web. beets-web signs nobody in: it sits behind a reverse
// proxy (Traefik with Authelia's forward auth) that does, and which sends the
// person's identity in headers. Every request must carry the Actor header or
// it is refused, so nothing is written without saying who wrote it.
//
// The headers are only trustworthy when beets-web is reachable through the
// proxy alone: anything that can connect directly can send them itself. In
// development a browser extension can add them.

import (
	"context"
	"io"
	"net/http"
	"strings"
	"unicode"
)

// defaultActorHeader is the header Authelia puts the username in.
const defaultActorHeader = "Remote-User"

// identityHeaders are what Authelia's forward auth sends about the person.
var identityHeaders = []string{"Remote-User", "Remote-Name", "Remote-Email", "Remote-Groups"}

// maxActor matches beets-api, which refuses a longer Actor.
const maxActor = 200

// identity is the person making a request.
type identity struct {
	Actor       string // what beets-api records, from ActorHeader
	ActorHeader string
	User        string
	Name        string
	Email       string
	Groups      []string
}

// DisplayName is the person's name for the navbar: their full name if they
// have one, else the name they sign in with, else the Actor.
func (i identity) DisplayName() string {
	for _, s := range []string{i.Name, i.User, i.Actor} {
		if s != "" {
			return s
		}
	}
	return ""
}

// Header is the value the proxy sent for a header, by name.
func (i identity) Header(name string) string {
	switch strings.ToLower(name) {
	case "remote-user":
		return i.User
	case "remote-name":
		return i.Name
	case "remote-email":
		return i.Email
	case "remote-groups":
		return strings.Join(i.Groups, ",")
	}
	return ""
}

type identityKey struct{}

func withIdentity(ctx context.Context, who identity) context.Context {
	return context.WithValue(ctx, identityKey{}, who)
}

// currentUser is who is making the request the context belongs to.
func currentUser(ctx context.Context) identity {
	who, _ := ctx.Value(identityKey{}).(identity)
	return who
}

// identityWriter carries who is signed in to pages rendered from it, as render
// and fail only have the ResponseWriter.
type identityWriter struct {
	http.ResponseWriter
	who identity
}

func (w identityWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func validActor(s string) bool {
	if len(s) > maxActor {
		return false
	}
	for _, r := range s {
		if !unicode.IsPrint(r) {
			return false
		}
	}
	return true
}

func splitGroups(s string) []string {
	var groups []string
	for _, g := range strings.Split(s, ",") {
		if g = strings.TrimSpace(g); g != "" {
			groups = append(groups, g)
		}
	}
	return groups
}

// requireActor refuses any request that doesn't say who made it. /ping is
// the one exception, so a health check can see that HTTP is up.
func (s *server) requireActor(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ping" && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			io.WriteString(w, "ok\n")
			return
		}
		header := s.actorHeader
		actor := strings.TrimSpace(r.Header.Get(header))
		if actor == "" {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusUnauthorized)
			io.WriteString(w, "401 Unauthorized: the "+header+" header is missing.\n")
			return
		}
		if !validActor(actor) {
			http.Error(w, "400 Bad Request: the "+header+" header is too long or has control characters", http.StatusBadRequest)
			return
		}
		who := identity{
			Actor: actor, ActorHeader: header,
			User:   strings.TrimSpace(r.Header.Get("Remote-User")),
			Name:   strings.TrimSpace(r.Header.Get("Remote-Name")),
			Email:  strings.TrimSpace(r.Header.Get("Remote-Email")),
			Groups: splitGroups(r.Header.Get("Remote-Groups")),
		}
		next.ServeHTTP(identityWriter{w, who}, r.WithContext(withIdentity(r.Context(), who)))
	})
}

// handleProfile shows what the proxy told beets-web about the person, and what
// beets-api records for their changes.
func (s *server) handleProfile(w http.ResponseWriter, r *http.Request) {
	render(w, http.StatusOK, profileView(currentUser(r.Context())))
}

// profileHeaders are the identity headers to list, plus the Actor header when
// it is some other one.
func profileHeaders(who identity) []string {
	for _, h := range identityHeaders {
		if strings.EqualFold(h, who.ActorHeader) {
			return identityHeaders
		}
	}
	return append(append([]string{}, identityHeaders...), who.ActorHeader)
}
