package routes

// PagePlaybook is passed to templates/partials/page_playbook.html
type PagePlaybook struct {
	Title     string
	Intro     string
	Bullets   []string
	GuideHref string
}

func playbookContacts() PagePlaybook {
	return PagePlaybook{
		Title:     "Contacts that send cleanly",
		Intro:     "Sheets in campaign folders beat giant unmapped dumps.",
		GuideHref: "/guides/contacts",
		Bullets: []string{
			"Put the audience in the campaign folder sheet so Manage stays in sync.",
			"Map columns to the same variable names your templates use.",
			"Add a location column if you schedule by each contact’s local time.",
			"Suppress bounces and unsubscribes so you don’t re-mail them.",
		},
	}
}

func playbookTemplates() PagePlaybook {
	return PagePlaybook{
		Title:     "Templates that earn replies",
		Intro:     "One idea, one ask, real personalization.",
		GuideHref: "/guides/templates",
		Bullets: []string{
			"Keep body short; make the ask easy to answer.",
			"Only use variables that exist on the contacts sheet.",
			"Run Jev as a checklist, then edit for truth.",
			"Preview with a real contact before launching.",
		},
	}
}

func playbookCampaigns() PagePlaybook {
	return PagePlaybook{
		Title:     "Campaigns that improve over time",
		Intro:     "Folders hold templates and contacts; Manage launches the campaign.",
		GuideHref: "/guides/campaigns",
		Bullets: []string{
			"Use one-time for a single touch; Sequence for follow-ups on Manage.",
			"Schedule absolute time or each contact’s local time via a location column.",
			"Set stop-on-reply / temperature / reply watchers before launch.",
			"Watch queue progress, Interested, and analytics after send.",
		},
	}
}

func playbookWorkflows() PagePlaybook {
	return PagePlaybook{
		Title:     "Sequences on Manage",
		Intro:     "Follow-ups are configured on the campaign — not a separate attach-workflow flow.",
		GuideHref: "/guides/workflows",
		Bullets: []string{
			"Set type to Sequence (or Sequence + A/B), then Edit sequence.",
			"Assign a template to every follow-up step.",
			"Reply watchers can auto-answer verified intents after a short delay.",
			"Replies can stop remaining steps — close from Interested yourself.",
		},
	}
}

func playbookMailboxes() PagePlaybook {
	return PagePlaybook{
		Title:     "Mailboxes & deliverability",
		Intro:     "Your sender reputation is the foundation of outreach.",
		GuideHref: "/guides/mailboxes",
		Bullets: []string{
			"Domains and mailboxes are add-ons — optional until you send.",
			"Match seat count to plan volume — one cold mailbox won’t carry Hyper alone.",
			"Stay on domain status until seats are ready, then test SMTP.",
			"Google sign-in is for account login — sending is configured here.",
		},
	}
}

func playbookSends() PagePlaybook {
	return PagePlaybook{
		Title:     "Reading the Sends list",
		Intro:     "Confirm delivery before you trust engagement charts.",
		GuideHref: "/guides/sends",
		Bullets: []string{
			"Spot-check that new campaign sends reach “sent.”",
			"Local-time schedules may leave jobs waiting until each contact’s slot.",
			"Open send detail for human vs bot opens and clicks.",
			"Failed sends usually mean mailbox/SMTP or plan-limit issues.",
		},
	}
}

func playbookInterested() PagePlaybook {
	return PagePlaybook{
		Title:     "Working interested leads",
		Intro:     "Automation finds interest — you close it.",
		GuideHref: "/guides/interested",
		Bullets: []string{
			"Prioritize positive replies first.",
			"Follow up the same day when possible.",
			"Dismiss noise so the queue stays actionable.",
			"Don’t rely on the next automated bump after a human reply.",
		},
	}
}

func playbookAnalytics() PagePlaybook {
	return PagePlaybook{
		Title:     "Using analytics to improve",
		Intro:     "Optimize for replies, not vanity opens.",
		GuideHref: "/guides/analytics",
		Bullets: []string{
			"Dashboard shows account pulse and plan usage.",
			"Campaign analytics compare variants and reply mix.",
			"Billing shows monthly email and contact usage vs caps.",
			"Change one variable at a time when iterating.",
		},
	}
}

func playbookSettings() PagePlaybook {
	return PagePlaybook{
		Title:     "Account & sending setup",
		Intro:     "Billing, goals, and pointers into mailboxes.",
		GuideHref: "/guides/getting-started",
		Bullets: []string{
			"Confirm plan and subscription under Billing.",
			"Mailboxes control what address actually sends.",
			"Getting started walks folder → sequence → launch.",
			"Reopen any guide anytime from Help in the sidebar.",
		},
	}
}
