// Command openapi regenerates the checked-in OpenAPI document and its copy
// for the marketing site. Run it from BE after changing a public route or
// DTO: go run ./cmd/openapi
package main

import (
	"fmt"
	"os"

	"github.com/kuayle/kuayle-backend/internal/openapi"
)

func main() {
	spec, err := openapi.Build()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	for _, path := range []string{"internal/openapi/openapi.json", "../WEB/src/lib/data/openapi.json"} {
		if err := os.WriteFile(path, spec, 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println("wrote", path)
	}
}
