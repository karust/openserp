package core

import "io"

// HTMLParser is implemented by engines that can parse a SERP HTML document
// without a live browser. Used to expose POST /parse/{engine} endpoints.
type HTMLParser interface {
	Name() string
	ParseHTML(io.Reader) ([]SearchResult, error)
}
