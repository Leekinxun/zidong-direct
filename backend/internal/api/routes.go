package api

import (
	"context"
	"net/http"
	"net/url"
	"strings"
)

type apiRoute struct {
	method  string
	path    []string
	handler http.HandlerFunc
}

type apiRoutes []apiRoute

func (routes *apiRoutes) add(pattern string, handler http.HandlerFunc) {
	method, path, _ := strings.Cut(pattern, " ")
	*routes = append(*routes, apiRoute{method, strings.Split(path, "/"), handler})
}

type connectionIDKey struct{}

func routeConnectionID(r *http.Request) string {
	id, _ := r.Context().Value(connectionIDKey{}).(string)
	return id
}

// Go 1.20's ServeMux can canonicalize paths, but it cannot match methods
// or capture a connection ID. Match escaped path segments independently so
// an escaped slash cannot change the route's shape.
func (routes apiRoutes) withFallback(fallback http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			fallback.ServeHTTP(w, r)
			return
		}
		method := r.Method
		if method == http.MethodHead {
			method = http.MethodGet
		}
		path := strings.Split(r.URL.EscapedPath(), "/")
		for i, segment := range path {
			path[i], _ = url.PathUnescape(segment)
		}
		for _, route := range routes {
			if route.method != method || len(route.path) != len(path) {
				continue
			}
			id, matched := route.match(path)
			if matched {
				if id != "" {
					r = r.WithContext(context.WithValue(r.Context(), connectionIDKey{}, id))
				}
				route.handler(w, r)
				return
			}
		}
		fallback.ServeHTTP(w, r)
	})
}

func (route apiRoute) match(path []string) (string, bool) {
	var id string
	for i, expected := range route.path {
		if expected == "{id}" {
			if path[i] == "" {
				return "", false
			}
			id = path[i]
		} else if expected != path[i] {
			return "", false
		}
	}
	return id, true
}
