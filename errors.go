package sx

type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

var (
	ErrorNotFound   = Error{404, "not found"}
	ErrorForbidden  = Error{401, "forbidden"}
	ErrorBadMethod  = Error{405, "bad method"}
	ErrorBadGateway = Error{502, "bad gateway"}
)
