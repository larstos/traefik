package plugin

import (
	"github.com/urfave/negroni"
)

// Middleware defines functions that should be implemented by a Middleware plugin
type Middleware interface {
	negroni.Handler
	Stop()
	Name()string
}
