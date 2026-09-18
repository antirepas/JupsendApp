package model

import (
	"emailtracker.com/db"
)

const DeliverabilityProbeVariant = "deliverability_probe"

const deliverabilityProbeTemplateName = "Deliverability probe (system)"

// EnsureDeliverabilityProbeTemplate returns a reusable plain template for Mail-Tester probes.
func EnsureDeliverabilityProbeTemplate(userID int64) (int64, error) {
	var id int64
	err := db.QueryRow(`
		SELECT id FROM template WHERE user_id = ? AND name = ? ORDER BY id ASC LIMIT 1
	`, userID, deliverabilityProbeTemplateName).Scan(&id)
	if err == nil && id > 0 {
		return id, nil
	}

	tpl := Template{
		Name:    deliverabilityProbeTemplateName,
		Subject: "Deliverability check",
		Body: `<p>Hello,</p>
<p>This is a one-off deliverability check from our sending mailbox.</p>
<p>If you received this message in your inbox, authentication and content look healthy.</p>
<p>Best regards</p>`,
	}
	return tpl.SaveTemplate(userID, nil)
}
