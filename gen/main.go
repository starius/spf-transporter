package main

import (
	"github.com/starius/api2"
	"gitlab.com/scpcorp/spf-transporter"
)

func main() {
	api2.GenerateClient(transporter.GetRoutes)
	api2.GenerateOpenApiSpec(&api2.TypesGenConfig{
		OutDir: "./openapi",
		Routes: []interface{}{transporter.GetRoutes},
	})
}
