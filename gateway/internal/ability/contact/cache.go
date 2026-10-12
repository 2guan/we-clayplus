package contactability

import "github.com/we-clay/we-clay/sdk/contact"

var cache map[string]*contact.Contact

func get(username string) *contact.Contact {
	return cache[username]
}
