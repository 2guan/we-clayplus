package sync

import (
	contactability "github.com/we-clay/we-clay/gateway/internal/ability/contact"
	messageapi "github.com/we-clay/we-clay/gateway/internal/api/message"
	pluginsdk "github.com/we-clay/we-clay/sdk/plugin"
)

func (p *processor) handleContact(contacts []*messageapi.ModifyContact) error {
	events := contactability.HandleModifyContacts(contacts)
	if len(contacts) > 0 && p.hooks.Contacts != nil {
		if err := p.hooks.Contacts(); err != nil {
			return &callbackError{err}
		}
	}
	return p.publishChangeEvents(events)
}

func (p *processor) publishChangeEvents(events []*pluginsdk.ChangeEvent) error {
	for _, event := range events {
		if event == nil {
			continue
		}
		if err := p.publish(&pluginsdk.Event{
			Topic:   event.Domain + "::" + event.Action,
			Payload: &pluginsdk.Event_Change{Change: event},
		}); err != nil {
			return err
		}
		for _, change := range event.Changes {
			if change.GetPath() == "" {
				continue
			}
			if err := p.publish(&pluginsdk.Event{
				Topic:   event.Domain + "::" + event.Action + "::" + change.GetPath(),
				Payload: &pluginsdk.Event_Change{Change: event},
			}); err != nil {
				return err
			}
		}
	}
	return nil
}
