package email_service

import (
	"fmt"

	gomail "gopkg.in/mail.v2"
)

var (
	smtpHost     string
	smtpPort     int
	smtpUser     string
	smtpPassword string
	smtpFrom     string
)

// Init configures the SMTP client for sending emails.
func Init(host string, port int, user, password, from string) {
	smtpHost = host
	smtpPort = port
	smtpUser = user
	smtpPassword = password
	smtpFrom = from
}

func SendEmail(subject, body, html, recipient_email string) error {
	message := gomail.NewMessage()

	message.SetHeader("From", smtpFrom)
	message.SetHeader("To", recipient_email)
	message.SetHeader("Subject", subject)

	message.SetBody("text/plain", body)
	message.AddAlternative("text/html", html)

	dialer := gomail.NewDialer(smtpHost, smtpPort, smtpUser, smtpPassword)

	if err := dialer.DialAndSend(message); err != nil {
		fmt.Println("Error:", err)
		return err
	}

	return nil
}
