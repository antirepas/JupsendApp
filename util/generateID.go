package util

import "github.com/google/uuid"

func GenerateID() int {
	id := int(uuid.New().ID())
	return id
}
