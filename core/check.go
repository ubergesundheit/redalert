package core

import (
	"errors"
	"log"
	"os"
	"strings"

	"github.com/nu7hatch/gouuid"

	"github.com/jonog/redalert/backoffs"
	"github.com/jonog/redalert/checks"
	"github.com/jonog/redalert/notifiers"
	"github.com/jonog/redalert/storage"
)

var MaxEventsStored = 100

type Check struct {
	ID      string
	Name    string
	Type    string // e.g. future options: web-ping, ssh-ping, query
	Backoff backoffs.Backoff

	Notifiers []notifiers.Notifier

	Log *log.Logger

	failCounts map[string]int

	Store storage.EventStorage

	Checker checks.Checker

	Triggers []checks.Trigger

	ConfigRank int
}

func NewCheck(config checks.Config) (*Check, error) {
	logger := log.New(os.Stdout, config.Name+" ", log.Ldate|log.Ltime)

	u4, err := uuid.NewV4()
	if err != nil {
		return nil, err
	}

	checker, err := checks.New(config, logger)
	if err != nil {
		return nil, err
	}

	return &Check{
		ID:         u4.String(),
		Name:       config.Name,
		Type:       config.Type,
		Backoff:    backoffs.New(config.Backoff),
		Notifiers:  make([]notifiers.Notifier, 0),
		Log:        logger,
		Store:      storage.NewMemoryList(MaxEventsStored),
		Checker:    checker,
		failCounts: make(map[string]int),
		Triggers:   config.Triggers,
	}, nil
}

func (c *Check) AddNotifiers(service *Service, names []string) error {
	for _, name := range names {
		notifier, err := getNotifier(service, name)
		if err != nil {
			return err
		}
		c.Notifiers = append(c.Notifiers, notifier)
	}
	return nil
}

func getNotifier(service *Service, name string) (notifiers.Notifier, error) {
	notifier, ok := service.notifiers[name]
	if !ok {
		return nil, errors.New("redalert: notifier requested has not be registered. name: " + name)
	}
	return notifier, nil
}

func (c *Check) incrFailCount(trigger string) int {
	c.failCounts[trigger]++
	return c.failCounts[trigger]
}

func (c *Check) resetFailCount(trigger string) {
	c.failCounts[trigger] = 0
}

func (c *Check) RecentMetrics(metric string) string {
	events, err := c.Store.GetRecent()
	if err != nil {
		c.Log.Println("ERROR: retrieving recent events")
		return ""
	}
	var output []string
	for _, event := range events {
		output = append([]string{event.DisplayMetric(metric)}, output...)
	}
	return strings.Join(output, ",")
}
