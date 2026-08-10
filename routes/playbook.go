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
		Title:     "Contacts for perfect outreach",
		Intro:     "Clean lists and mapped variables beat big messy imports.",
		GuideHref: "/guides/contacts",
		Bullets: []string{
			"Import a focused ICP list — not your entire database.",
			"Map columns to the same variable names your templates use.",
			"Prefer one best email per row; use suppressions for bounces/unsubscribes.",
			"Group people into lists so each campaign stays organized.",
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
			"Only use variables you actually import on contacts.",
			"Write a different follow-up than the intro for warm paths.",
			"Preview with a real contact before launching a campaign.",
		},
	}
}

func playbookCampaigns() PagePlaybook {
	return PagePlaybook{
		Title:     "Campaigns that improve over time",
		Intro:     "Pick the right type, set temperature rules, then launch with a checklist.",
		GuideHref: "/guides/campaigns",
		Bullets: []string{
			"Use bulk for one touch; workflow when you need follow-ups.",
			"Publish workflows and map a template to every send step.",
			"Set lead temperature (warm/hot thresholds) on the campaign page.",
			"Watch analytics and Interested after send — treat each campaign as an experiment.",
		},
	}
}

func playbookWorkflows() PagePlaybook {
	return PagePlaybook{
		Title:     "Workflows that react to interest",
		Intro:     "Start from the recommended outreach sequence, then publish and attach.",
		GuideHref: "/guides/outreach",
		Bullets: []string{
			"Clone Recommended outreach: A/B cold → temperature → value prop or breakup.",
			"Hot/warm at the end = manual Loom + Calendly — automation stops on purpose.",
			"Publish before attaching; map a template to every send step.",
			"Replies stop remaining steps — follow up from Interested.",
		},
	}
}

func playbookMailboxes() PagePlaybook {
	return PagePlaybook{
		Title:     "Mailboxes & deliverability",
		Intro:     "Your sender reputation is the foundation of outreach.",
		GuideHref: "/guides/mailboxes",
		Bullets: []string{
			"Free uses a shared seat; Pro uses your domain and included mailboxes.",
			"Use real From names on seats; warm new domains slowly.",
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
			"Open send detail for human vs bot opens and clicks.",
			"In-app previews won’t fire tracking pixels or click trackers.",
			"Failed sends usually mean mailbox/SMTP issues — fix Mailboxes first.",
		},
	}
}

func playbookInterested() PagePlaybook {
	return PagePlaybook{
		Title:     "Working interested leads",
		Intro:     "Automation finds interest — you close it.",
		GuideHref: "/guides/interested",
		Bullets: []string{
			"Prioritize replies, then clicks, then meaningful opens.",
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
			"Dashboard shows account pulse after your first sends.",
			"Campaign analytics compare variants and funnels.",
			"Workflow analytics show where contacts stall in the graph.",
			"Change one variable at a time (subject, opener, CTA) when iterating.",
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
			"Optional getting-started playbook walks the full outreach path.",
			"Reopen any guide anytime from Help in the sidebar.",
		},
	}
}
