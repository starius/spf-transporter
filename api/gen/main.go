package main

import (
	"github.com/starius/api2"
	"gitlab.com/scpcorp/spf-transporter/api"
)

func main() {
	api2.GenerateClient(api.GetRoutes)
	api2.GenerateOpenApiSpec(&api2.TypesGenConfig{
		OutDir: "./openapi",
		Routes: []interface{}{api.GetRoutes},
	})
}
