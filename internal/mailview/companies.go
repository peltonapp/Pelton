package mailview

import (
	"strings"

	"golang.org/x/net/publicsuffix"
)

// trackerCompanies names the company behind a tracking domain, where the
// domain itself says it. It is hand-written and short on purpose: a domain
// missing here is shown as itself, which is still the truth.
var trackerCompanies = map[string]string{
	"list-manage.com":      "Mailchimp",
	"mailchimp.com":        "Mailchimp",
	"mcusercontent.com":    "Mailchimp",
	"mandrillapp.com":      "Mailchimp",
	"sendgrid.net":         "Twilio SendGrid",
	"hubspotemail.net":     "HubSpot",
	"hubspotlinks.com":     "HubSpot",
	"hs-analytics.net":     "HubSpot",
	"hs-sites.com":         "HubSpot",
	"exct.net":             "Salesforce",
	"rs6.net":              "Constant Contact",
	"klaviyomail.com":      "Klaviyo",
	"klclick.com":          "Klaviyo",
	"klclick1.com":         "Klaviyo",
	"braze.com":            "Braze",
	"braze.eu":             "Braze",
	"sparkpostmail.com":    "SparkPost",
	"mailgun.org":          "Mailgun",
	"awstrack.me":          "Amazon",
	"doubleclick.net":      "Google",
	"google-analytics.com": "Google",
	"googletagmanager.com": "Google",
	"mailtrack.io":         "Mailtrack",
	"yesware.com":          "Yesware",
	"streak.com":           "Streak",
	"bananatag.com":        "Bananatag",
	"convertkit-mail.com":  "Kit",
	"convertkit-mail2.com": "Kit",
	"customeriomail.com":   "Customer.io",
	"mailerlite.com":       "MailerLite",
	"mlsend.com":           "MailerLite",
	"omnisend.com":         "Omnisend",
	"sailthru.com":         "Sailthru",
	"emltrk.com":           "Litmus",
	"mjt.lu":               "Mailjet",
	"createsend1.com":      "Campaign Monitor",
	"cmail19.com":          "Campaign Monitor",
	"cmail20.com":          "Campaign Monitor",
	"sendibt2.com":         "Brevo",
	"sendibt3.com":         "Brevo",
	"pstmrk.it":            "Postmark",
	"iterable.com":         "Iterable",
	"app.link":             "Branch",
}

// Company returns the company a tracking host belongs to, or empty when the
// domain does not say.
func Company(host string) string {
	h := strings.ToLower(strings.TrimSpace(host))
	if c, ok := trackerCompanies[h]; ok {
		return c
	}
	reg, err := publicsuffix.EffectiveTLDPlusOne(h)
	if err != nil {
		return ""
	}
	return trackerCompanies[reg]
}
