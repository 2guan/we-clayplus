package sync

import (
	contactability "github.com/we-clay/we-clay/gateway/internal/ability/contact"
	messageapi "github.com/we-clay/we-clay/gateway/internal/api/message"
	"github.com/we-clay/we-clay/gateway/internal/plugin"
	pluginsdk "github.com/we-clay/we-clay/sdk/plugin"
)

func handleContact(contacts []*messageapi.ModifyContact) {
	publishChangeEvents(contactability.HandleModifyContacts(contacts))
}

func publishChangeEvents(events []*pluginsdk.ChangeEvent) {
	for _, event := range events {
		if event == nil {
			continue
		}
		plugin.Publish(&pluginsdk.Event{
			Topic:   event.Domain + "::" + event.Action,
			Payload: &pluginsdk.Event_Change{Change: event},
		})
		for _, change := range event.Changes {
			if change.GetPath() == "" {
				continue
			}
			plugin.Publish(&pluginsdk.Event{
				Topic:   event.Domain + "::" + event.Action + "::" + change.GetPath(),
				Payload: &pluginsdk.Event_Change{Change: event},
			})
		}
	}
}
