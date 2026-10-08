package server

import (
	"log"
	"time"

	"github.com/friendo-world/friendo/runtime/go/data"
	"github.com/friendo-world/friendo/runtime/go/email"
)

// reminderEvery is how often the server looks for reminders to send.
const reminderEvery = 5 * time.Minute

// runReminders emails event reminders as they come due (an RSVP with an
// email, the day before its date — see data.SendDueReminders). It runs for
// the life of the server. Without an email provider there's nothing it could
// send, and the API won't have stored any addresses, so it just waits.
func runReminders(db *data.DB) {
	send := func(r data.Reminder) error {
		return email.SendEventReminder(r.Email, r.Title, r.When, r.Link)
	}
	for {
		if email.Configured() {
			n, err := db.SendDueReminders(time.Now(), send)
			if err != nil {
				log.Printf("event reminders: %v", err)
			} else if n > 0 {
				log.Printf("event reminders: sent %d", n)
			}
		}
		time.Sleep(reminderEvery)
	}
}
