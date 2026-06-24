package dto

type UserSetting struct {
	NotifyType                       string  `json:"notify_type,omitempty"`                          // QuotaWarningType
	QuotaWarningThreshold            float64 `json:"quota_warning_threshold,omitempty"`              // QuotaWarningThreshold
	WebhookUrl                       string  `json:"webhook_url,omitempty"`                          // WebhookUrl webhook
	WebhookSecret                    string  `json:"webhook_secret,omitempty"`                       // WebhookSecret webhook
	NotificationEmail                string  `json:"notification_email,omitempty"`                   // NotificationEmail
	BarkUrl                          string  `json:"bark_url,omitempty"`                             // BarkUrl BarkURL
	GotifyUrl                        string  `json:"gotify_url,omitempty"`                           // GotifyUrl Gotify
	GotifyToken                      string  `json:"gotify_token,omitempty"`                         // GotifyToken Gotify
	GotifyPriority                   int     `json:"gotify_priority"`                                // GotifyPriority Gotify
	UpstreamModelUpdateNotifyEnabled bool    `json:"upstream_model_update_notify_enabled,omitempty"`
	AcceptUnsetRatioModel            bool    `json:"accept_unset_model_ratio_model,omitempty"`       // AcceptUnsetRatioModel
	RecordIpLog                      bool    `json:"record_ip_log,omitempty"`                        // IP
	SidebarModules                   string  `json:"sidebar_modules,omitempty"`                      // SidebarModules
	BillingPreference                string  `json:"billing_preference,omitempty"`                   // BillingPreference /
	Language                         string  `json:"language,omitempty"`                             // Language (zh, en)
}

var (
	NotifyTypeEmail   = "email"   // Email
	NotifyTypeWebhook = "webhook" // Webhook
	NotifyTypeBark    = "bark"    // Bark
	NotifyTypeGotify  = "gotify"  // Gotify
)
