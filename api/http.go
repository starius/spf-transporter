package api

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/starius/api2"
)

func route(s Service, handler, httpMethod string) api2.Route {
	return api2.Route{
		Method:  httpMethod,
		Path:    fmt.Sprintf("/v1/transporter/%s", strings.ToLower(handler)),
		Handler: api2.Method(&s, handler),
		Transport: &api2.JsonTransport{
			Errors: map[string]error{
				"Error": Error{},
			},
		},
	}
}

func GetRoutes(s Service) []api2.Route {
	return []api2.Route{
		route(s, "CheckUtxoApproval", http.MethodGet),
		route(s, "SubmitScpTx", http.MethodPost),
		route(s, "SubmitSolanaTx", http.MethodPost),
		route(s, "History", http.MethodGet),
	}
}
