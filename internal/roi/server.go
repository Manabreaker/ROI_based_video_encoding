package roi

import (
	"net/http"
)

// serve exposes the output directory through a local HTTP file server.
func serve(dir string, addr string) error {
	fs := http.FileServer(http.Dir(dir))
	http.Handle("/", fs)
	return http.ListenAndServe(addr, nil)
}
