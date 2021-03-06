package core

import (
	"fmt"
	"os"
	"os/signal"
	"sync"
	"time"

	"github.com/jonog/redalert/events"
	"github.com/jonog/redalert/utils"
)

func (c *Check) Start() {

	var wg sync.WaitGroup
	wg.Add(1)

	stopScheduler := make(chan bool)
	c.run(stopScheduler)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)
	go func() {
		for _ = range sigChan {
			stopScheduler <- true
			wg.Done()
		}
	}()

	wg.Wait()

}

func (c *Check) run(stopChan chan bool) {

	go func() {

		var err error
		var event *events.Event
		var checkData map[string]*float64

		delay := c.Backoff.Init()

		for {

			checkData, err = c.Checker.Check()
			event = events.NewEvent(checkData)

			if err != nil {

				// Trigger RedAlert as check has failed
				event.MarkRedAlert()
				c.Log.Println(utils.Red, "redalert", err, utils.Reset)

				// increase fail count and delay between checks
				failCount, storeErr := c.Store.IncrFailCount("redalert")
				if storeErr != nil {
					c.Log.Println(utils.Red, "ERROR: storing failure stats", utils.Reset)
				}
				if failCount > 0 {
					delay = c.Backoff.Next(failCount)
				}

			}

			if err == nil {

				lastEvent, storeErr := c.Store.Last()
				if storeErr != nil {
					c.Log.Println(utils.Red, "ERROR: retrieving event from store", utils.Reset)
				}

				// Trigger GreenAlert if check is successful and was previously failing
				isRedalertRecovery := lastEvent != nil && lastEvent.IsRedAlert()
				if isRedalertRecovery {
					event.MarkGreenAlert()
					c.Log.Println(utils.Green, "greenalert", utils.Reset)

					// reset fail count & delay between checks
					delay = c.Backoff.Init()
					storeErr := c.Store.ResetFailCount("redalert")
					if storeErr != nil {
						c.Log.Println(utils.Red, "ERROR: storing failure stats", utils.Reset)
					}

				}

			}

			c.Store.Store(event)
			c.processNotifications(event)

			select {
			case <-time.After(delay):
			case <-stopChan:
				return
			}
		}
	}()

}

func (c *Check) processNotifications(event *events.Event) {

	msgPrefix := c.Name + " :: (" + c.Type + " - " + c.Checker.MessageContext() + ") "

	// Process Threshold Notifications

	for _, trigger := range c.Triggers {

		if !trigger.MeetsCriteria(event.Metrics) {
			continue
		}

		msg := msgPrefix + trigger.Metric + " (" + fmt.Sprintf("%f", *event.Metrics[trigger.Metric]) + ") " + " has met alert criteria, " + trigger.Criteria
		for _, notifier := range c.Notifiers {
			err := notifier.Notify(AlertMessage{msg})
			if err != nil {
				c.Log.Println(utils.Red, "CRITICAL: Failure triggering threshold alert ["+notifier.Name()+"]: ", err.Error())
			}
		}

	}

	// Process Redalert/Greenalert (Failure / Recovery)

	if len(event.Tags) == 0 {
		return
	}

	go func() {

		if !event.IsRedAlert() && !event.IsGreenAlert() {
			return
		}

		var err error
		for _, notifier := range c.Notifiers {

			c.Log.Println(utils.White, "Sending "+event.DisplayTags()+" via "+notifier.Name(), utils.Reset)

			var msg string
			if event.IsRedAlert() {
				msg = msgPrefix + "fail"
			} else if event.IsGreenAlert() {
				msg = msgPrefix + "recovery"
			}

			err = notifier.Notify(AlertMessage{msg})
			if err != nil {
				c.Log.Println(utils.Red, "CRITICAL: Failure triggering alert ["+notifier.Name()+"]: ", err.Error())
			}
		}

	}()
}

type AlertMessage struct {
	Short string
}

func (m AlertMessage) ShortMessage() string {
	return m.Short
}
