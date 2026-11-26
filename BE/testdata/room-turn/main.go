// The browser fixture captures the owner's post and passes it through the same
// inbox, wake and prompt functions the pipeline worker uses for a new turn.
package main

import (
	"encoding/json"
	"log"
	"os"

	"github.com/kuayle/kuayle-backend/internal/domain"
)

func main() {
	var post domain.TaskMessage
	if err := json.NewDecoder(os.Stdin).Decode(&post); err != nil {
		log.Fatal(err)
	}
	posts := []domain.TaskMessage{post}
	prompts := make(map[string]string)
	for _, role := range os.Args[1:] {
		if domain.RoomWakes(domain.PipelineMember{Role: role}, domain.RoomInbox(role, posts)) {
			prompts[role] = domain.RoomTurnPrompt(role, posts)
		}
	}
	if err := json.NewEncoder(os.Stdout).Encode(prompts); err != nil {
		log.Fatal(err)
	}
}
